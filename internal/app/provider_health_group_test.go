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

func providerGroupHealthByID(groups []ProviderHealthView, groupID string) *ProviderHealthView {
	for i := range groups {
		if groups[i].Group == groupID {
			return &groups[i]
		}
	}
	return nil
}
