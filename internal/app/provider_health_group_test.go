package app

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
)

func TestQueryProviderHealthSeparatesGroupsAndPreservesProviderSummary(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Providers = []config.Provider{{
		ID: "vendor", Name: "Vendor", BaseURL: "https://vendor.example/v1",
		Groups: []config.ProviderGroup{
			{ID: "standard", Name: "Standard", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-fake-standard", Models: []string{"shared"}},
			{ID: "premium", Name: "Premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-fake-premium", Models: []string{"shared"}},
		},
	}}
	cfg.Aliases = []config.Alias{{
		Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
		Targets: []config.Target{
			{Provider: "vendor", Group: "premium", Model: "shared", Enabled: true},
			{Provider: "vendor", Group: "standard", Model: "shared", Enabled: true},
		},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	svc.traces = proxy.NewTraceStore(10)
	now := time.Now().UTC()
	traces := []proxy.RequestTrace{
		{
			ID: 1, StartedAt: now, Alias: "chat", Success: true, FinalProvider: "vendor", FinalGroup: "premium", AttemptCount: 2,
			Attempts: []proxy.TraceAttempt{
				{Attempt: 1, Provider: "vendor", Group: "standard", Model: "shared", StatusCode: http.StatusTooManyRequests, Retryable: true, Result: "retryable_failure"},
				{Attempt: 2, Provider: "vendor", Group: "premium", Model: "shared", StatusCode: http.StatusOK, Success: true, Result: "success"},
			},
		},
		{
			ID: 2, StartedAt: now.Add(time.Second), Alias: "chat", FinalProvider: "vendor", FinalGroup: "standard", AttemptCount: 1,
			Attempts: []proxy.TraceAttempt{{Attempt: 1, Provider: "vendor", Group: "standard", Model: "shared", StatusCode: http.StatusBadGateway, Retryable: true, Result: "retryable_failure"}},
		},
		{
			ID: 3, StartedAt: now.Add(2 * time.Second), Alias: "legacy", Success: true, FinalProvider: "vendor", AttemptCount: 1,
		},
	}
	for _, trace := range traces {
		if err := svc.traces.Add(context.Background(), trace); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", trace.ID, err)
		}
	}

	result, err := svc.QueryProviderHealth(context.Background(), ProviderHealthInput{})
	if err != nil {
		t.Fatalf("QueryProviderHealth() error = %v", err)
	}
	provider := providerHealthByID(result.Providers, "vendor")
	if provider == nil {
		t.Fatal("vendor health missing")
	}
	if provider.AttemptCount != 4 || provider.Success != 2 || provider.RetryableFailures != 2 || provider.RateLimited != 1 || provider.Upstream5xx != 1 {
		t.Fatalf("provider summary = %#v", provider)
	}
	standard := providerGroupHealthByID(provider.Groups, "standard")
	premium := providerGroupHealthByID(provider.Groups, "premium")
	legacy := providerGroupHealthByID(provider.Groups, config.DefaultGroupID)
	if standard == nil || premium == nil || legacy == nil {
		t.Fatalf("group details = %#v", provider.Groups)
	}
	if standard.AttemptCount != 2 || standard.Success != 0 || standard.RetryableFailures != 2 || standard.RateLimited != 1 || standard.Upstream5xx != 1 || standard.Role != "backup" {
		t.Fatalf("standard = %#v", standard)
	}
	if premium.AttemptCount != 1 || premium.Success != 1 || premium.FinalSuccess != 1 || premium.RetryableFailures != 0 || premium.Role != "primary" {
		t.Fatalf("premium = %#v", premium)
	}
	if legacy.Group != config.DefaultGroupID || legacy.Configured || legacy.AttemptCount != 1 || legacy.Success != 1 || legacy.FinalSuccess != 1 {
		t.Fatalf("historical default = %#v", legacy)
	}
	if provider.AttemptCount != standard.AttemptCount+premium.AttemptCount+legacy.AttemptCount || provider.RetryableFailures != standard.RetryableFailures+premium.RetryableFailures+legacy.RetryableFailures {
		t.Fatalf("provider summary does not equal group totals: provider=%#v groups=%#v", provider, provider.Groups)
	}
}

func TestQueryProviderHealthAggregatesModelHealthByProviderGroupModel(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Providers = []config.Provider{modelHealthTestProvider("vendor", []config.ProviderGroup{
		{ID: "standard", Name: "Standard", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-fake-standard", Models: []string{"gpt-5.5"}},
		{ID: "premium", Name: "Premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-fake-premium", Models: []string{"gpt-5.5", "gpt-4.1"}},
	})}
	cfg.Aliases = []config.Alias{{
		Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
		Targets: []config.Target{
			{Provider: "vendor", Group: "premium", Model: "gpt-5.5", Enabled: true},
			{Provider: "vendor", Group: "standard", Model: "gpt-5.5", Enabled: true},
		},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	svc.traces = proxy.NewTraceStore(10)
	now := time.Now().UTC()
	cachePremium := int64(850)
	cacheStandard := int64(400)
	traces := []proxy.RequestTrace{
		{
			ID: 1, StartedAt: now, DurationMs: 3_600_000, Alias: "chat", Success: true,
			FinalProvider: "vendor", FinalGroup: "premium", FinalModel: "gpt-5.5", InputTokens: 100, OutputTokens: 50,
			Usage: proxy.TraceUsage{CacheReadTokens: &cachePremium},
		},
		{
			ID: 2, StartedAt: now.Add(time.Second), DurationMs: 1_800_000, Alias: "chat", Success: true,
			FinalProvider: "vendor", FinalGroup: "standard", FinalModel: "gpt-5.5", InputTokens: 80, OutputTokens: 20,
			Usage: proxy.TraceUsage{CacheReadTokens: &cacheStandard},
		},
		{
			ID: 3, StartedAt: now.Add(2 * time.Second), DurationMs: 1_000, Alias: "chat", Success: false,
			Attempts: []proxy.TraceAttempt{{Attempt: 1, Provider: "vendor", Group: "premium", Model: "gpt-5.5", StatusCode: http.StatusBadGateway, Retryable: true, Result: "retryable_failure"}},
		},
	}
	for _, trace := range traces {
		if err := svc.traces.Add(context.Background(), trace); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", trace.ID, err)
		}
	}

	result, err := svc.QueryProviderHealth(context.Background(), ProviderHealthInput{})
	if err != nil {
		t.Fatalf("QueryProviderHealth() error = %v", err)
	}
	premium := providerModelHealthByRoute(result.Models, "vendor", "premium", "gpt-5.5")
	standard := providerModelHealthByRoute(result.Models, "vendor", "standard", "gpt-5.5")
	if premium == nil || standard == nil {
		t.Fatalf("model health rows = %#v", result.Models)
	}
	if premium.RequestCount != 2 || premium.Success != 1 || premium.InputTokens != 100 || premium.OutputTokens != 50 || premium.CacheReadTokens != 850 || premium.TotalTokens != 1000 || premium.TotalDurationMs != 3_601_000 {
		t.Fatalf("premium model health = %#v", premium)
	}
	if standard.RequestCount != 1 || standard.Success != 1 || standard.InputTokens != 80 || standard.OutputTokens != 20 || standard.CacheReadTokens != 400 || standard.TotalTokens != 500 || standard.TotalDurationMs != 1_800_000 {
		t.Fatalf("standard model health = %#v", standard)
	}
	assertFloatNear(t, premium.TokenShare, 2.0/3.0)
	assertFloatNear(t, standard.TokenShare, 1.0/3.0)
	assertFloatNear(t, premium.CacheHitRate, 850.0/950.0)
	assertFloatNear(t, premium.SuccessRate, 0.5)
	assertFloatNear(t, standard.SuccessRate, 1)
}

func TestProviderModelHealthViewsSortsByProviderTotalTokensThenModelShare(t *testing.T) {
	t.Parallel()

	traces := []proxy.RequestTrace{
		modelHealthTrace(1, "alpha", "default", "small", 100),
		modelHealthTrace(2, "zeta", "default", "small", 200),
		modelHealthTrace(3, "alpha", "default", "large", 700),
		modelHealthTrace(4, "zeta", "default", "large", 1000),
	}

	items := providerModelHealthViews(traces, nil)
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Provider+"/"+item.Group+"/"+item.Model)
	}
	want := []string{
		"zeta/default/large",
		"zeta/default/small",
		"alpha/default/large",
		"alpha/default/small",
	}
	if len(got) != len(want) {
		t.Fatalf("model health order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("model health order = %v, want %v", got, want)
		}
	}
	assertFloatNear(t, items[0].TokenShare, 0.5)
	assertFloatNear(t, items[1].TokenShare, 0.1)
}

func modelHealthTrace(id uint64, provider, group, model string, totalTokens int64) proxy.RequestTrace {
	return proxy.RequestTrace{
		ID:            id,
		StartedAt:     time.Now().UTC().Add(time.Duration(id) * time.Second),
		Success:       true,
		FinalProvider: provider,
		FinalGroup:    group,
		FinalModel:    model,
		InputTokens:   totalTokens,
	}
}

func modelHealthTestProvider(id string, groups []config.ProviderGroup) config.Provider {
	return config.Provider{ID: id, Name: "Vendor", BaseURL: "https://vendor.example/v1", Groups: groups}
}

func providerModelHealthByRoute(items []ProviderModelHealthView, providerID, groupID, model string) *ProviderModelHealthView {
	for i := range items {
		if items[i].Provider == providerID && items[i].Group == groupID && items[i].Model == model {
			return &items[i]
		}
	}
	return nil
}

func assertFloatNear(t *testing.T, got, want float64) {
	t.Helper()
	const tolerance = 0.000001
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("float = %v, want %v", got, want)
	}
}

func providerGroupHealthByID(groups []ProviderHealthView, groupID string) *ProviderHealthView {
	for i := range groups {
		if groups[i].Group == groupID {
			return &groups[i]
		}
	}
	return nil
}
