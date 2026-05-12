package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateProviderBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "valid exact", input: "https://example.com/v1"},
		{name: "valid trailing slash", input: "https://example.com/v1/"},
		{name: "valid trimmed", input: "  https://example.com/v1/  "},
		{name: "missing", input: "", wantErr: "missing base_url"},
		{name: "missing v1", input: "https://example.com/api", wantErr: "base_url must end with /v1"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProviderBaseURL(ProtocolOpenAIResponses, tt.input)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestValidateProviderBaseURLAnthropic(t *testing.T) {
	t.Parallel()

	if err := ValidateProviderBaseURL(ProtocolAnthropicMessages, "https://api.anthropic.com/v1/"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProviderBaseURL(ProtocolAnthropicMessages, "https://api.anthropic.com/api"); err == nil {
		t.Fatal("expected /v1 validation error")
	} else if err.Error() != "base_url must end with /v1" {
		t.Fatalf("expected anthropic /v1 validation error, got %q", err.Error())
	}
}

func TestValidateProviderBaseURLOpenAICompatible(t *testing.T) {
	t.Parallel()

	if err := ValidateProviderBaseURL(ProtocolOpenAICompatible, "https://compat.example.com/v1/"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := ProtocolLocalRequestPath(ProtocolOpenAICompatible); got != "/v1/chat/completions" {
		t.Fatalf("ProtocolLocalRequestPath() = %q, want /v1/chat/completions", got)
	}
	if got := ProtocolUpstreamRequestPath(ProtocolOpenAICompatible); got != "/chat/completions" {
		t.Fatalf("ProtocolUpstreamRequestPath() = %q, want /chat/completions", got)
	}
}

func TestNormalizeProviderBaseURL(t *testing.T) {
	t.Parallel()

	if got := NormalizeProviderBaseURL("  https://example.com/v1/  "); got != "https://example.com/v1" {
		t.Fatalf("NormalizeProviderBaseURL() = %q", got)
	}
}

func TestAvailableTargetsSkipsDisabledAndMissingProviders(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true},
		},
	}
	alias := Alias{
		Alias:   "gpt-5.4",
		Enabled: true,
		Targets: []Target{
			{Provider: "p1", Model: "up-1", Enabled: true},
			{Provider: "p2", Model: "up-2", Enabled: true},
			{Provider: "missing", Model: "up-3", Enabled: true},
			{Provider: "p1", Model: "up-4", Enabled: false},
		},
	}

	targets := cfg.AvailableTargets(alias)
	if len(targets) != 1 {
		t.Fatalf("available targets = %#v, want exactly one", targets)
	}
	if targets[0].Provider != "p1" || targets[0].Model != "up-1" {
		t.Fatalf("available target = %#v, want p1/up-1", targets[0])
	}
}

func TestReorderTargetsPreservesTargetState(t *testing.T) {
	t.Parallel()

	cfg := &Config{Aliases: []Alias{{
		Alias:   "chat",
		Enabled: true,
		Targets: []Target{
			{Provider: "p1", Model: "up-1", Enabled: true},
			{Provider: "p2", Model: "up-2", Enabled: false},
			{Provider: "p3", Model: "up-3", Enabled: true},
		},
	}}}

	if err := cfg.ReorderTargets("chat", []TargetRef{
		{Provider: "p3", Model: "up-3"},
		{Provider: "p1", Model: "up-1"},
		{Provider: "p2", Model: "up-2"},
	}); err != nil {
		t.Fatalf("ReorderTargets() error = %v", err)
	}

	alias := cfg.FindAlias("chat")
	if alias == nil {
		t.Fatal("alias chat not found")
	}
	want := []Target{
		{Provider: "p3", Model: "up-3", Enabled: true},
		{Provider: "p1", Model: "up-1", Enabled: true},
		{Provider: "p2", Model: "up-2", Enabled: false},
	}
	if !slices.Equal(alias.Targets, want) {
		t.Fatalf("targets = %#v, want %#v", alias.Targets, want)
	}
}

func TestReorderTargetsRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		refs    []TargetRef
		wantErr string
	}{
		{
			name:    "missing target",
			refs:    []TargetRef{{Provider: "p1", Model: "up-1"}},
			wantErr: "target count mismatch",
		},
		{
			name:    "duplicate target",
			refs:    []TargetRef{{Provider: "p1", Model: "up-1"}, {Provider: "p1", Model: "up-1"}},
			wantErr: "duplicate target p1/up-1",
		},
		{
			name:    "unknown target",
			refs:    []TargetRef{{Provider: "p1", Model: "up-1"}, {Provider: "missing", Model: "up-x"}},
			wantErr: "not found",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{Aliases: []Alias{{
				Alias: "chat",
				Targets: []Target{
					{Provider: "p1", Model: "up-1", Enabled: true},
					{Provider: "p2", Model: "up-2", Enabled: true},
				},
			}}}
			err := cfg.ReorderTargets("chat", tt.refs)
			if err == nil {
				t.Fatal("ReorderTargets() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ReorderTargets() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAvailableAliasNamesOnlyReturnsRoutableAliases(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true},
		},
		Aliases: []Alias{
			{Alias: "ok", Enabled: true, Targets: []Target{{Provider: "p1", Model: "up-1", Enabled: true}}},
			{Alias: "provider-disabled", Enabled: true, Targets: []Target{{Provider: "p2", Model: "up-2", Enabled: true}}},
			{Alias: "alias-disabled", Enabled: false, Targets: []Target{{Provider: "p1", Model: "up-3", Enabled: true}}},
		},
	}

	names := cfg.AvailableAliasNames()
	if len(names) != 1 || names[0] != "ok" {
		t.Fatalf("available alias names = %#v, want [ok]", names)
	}
}

func TestValidateRejectsDefaultKeyOnNonLoopbackHost(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: Default().Server,
	}
	cfg.Server.Host = "0.0.0.0"

	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", errs)
	}
	if !strings.Contains(errs[0].Error(), "must not use the default value") {
		t.Fatalf("Validate() error = %q", errs[0].Error())
	}
}

func TestValidateReportsAliasWithoutAvailableTargets(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Default().Server,
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", Disabled: true}},
		Aliases: []Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	}

	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", errs)
	}
	if got := errs[0].Error(); got != `alias "gpt-5.4" has no available targets` {
		t.Fatalf("Validate() error = %q", got)
	}
}

func TestRequestRewriteRulesSaveLoadAndApply(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.RequestRewriteRules = []RequestRewriteRule{
		{
			Name:    "fast-tier",
			Alias:   "gpt-5.5-fast",
			Enabled: true,
			Set: map[string]any{
				"serviceTier": "priority",
				"store":       false,
			},
		},
		{
			Name:     "model-override",
			Model:    "gpt-5.5",
			Enabled:  true,
			Override: true,
			Set:      map[string]any{"reasoningEffort": "high"},
			Delete:   []string{"parallel_tool_calls"},
		},
		{
			Name:    "disabled",
			Alias:   "gpt-5.5-fast",
			Enabled: false,
			Set:     map[string]any{"disabled_field": true},
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.RequestRewriteRules) != 3 {
		t.Fatalf("rewrite rule count = %d, want 3", len(loaded.RequestRewriteRules))
	}
	if loaded.RequestRewriteRules[0].Name != "fast-tier" || !loaded.RequestRewriteRules[0].Enabled {
		t.Fatalf("first rule = %#v", loaded.RequestRewriteRules[0])
	}

	payload := map[string]any{
		"model":               "gpt-5.5",
		"serviceTier":         "standard",
		"parallel_tool_calls": true,
	}
	loaded.ApplyRequestRewriteRules("gpt-5.5-fast", "gpt-5.5", payload)
	if got := payload["serviceTier"]; got != "standard" {
		t.Fatalf("serviceTier = %#v, want request value", got)
	}
	if got := payload["store"]; got != false {
		t.Fatalf("store = %#v, want false", got)
	}
	if got := payload["reasoningEffort"]; got != "high" {
		t.Fatalf("reasoningEffort = %#v, want high", got)
	}
	if _, ok := payload["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls still present: %#v", payload)
	}
	if _, ok := payload["disabled_field"]; ok {
		t.Fatalf("disabled rule applied: %#v", payload)
	}
}

func TestValidateRequestRewriteRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    RequestRewriteRule
		wantErr string
	}{
		{
			name:    "empty name",
			rule:    RequestRewriteRule{Alias: "chat", Enabled: true, Set: map[string]any{"store": false}},
			wantErr: "empty name",
		},
		{
			name:    "missing scope",
			rule:    RequestRewriteRule{Name: "missing-scope", Enabled: true, Set: map[string]any{"store": false}},
			wantErr: "requires alias or model",
		},
		{
			name:    "missing operation",
			rule:    RequestRewriteRule{Name: "missing-op", Alias: "chat", Enabled: true},
			wantErr: "requires set or delete",
		},
		{
			name:    "delete without override",
			rule:    RequestRewriteRule{Name: "bad-delete", Alias: "chat", Enabled: true, Delete: []string{"store"}},
			wantErr: "delete requires override",
		},
		{
			name: "valid",
			rule: RequestRewriteRule{Name: "valid", Model: "gpt-5.5", Enabled: true, Override: true, Delete: []string{"store"}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			cfg.RequestRewriteRules = []RequestRewriteRule{tt.rule}
			errs := cfg.Validate()
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("Validate() errors = %v, want none", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("Validate() errors = nil, want error")
			}
			if !strings.Contains(errs[0].Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %q, want containing %q", errs[0].Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateRejectsDuplicateRequestRewriteRuleNames(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.RequestRewriteRules = []RequestRewriteRule{
		{Name: "same", Alias: "a", Enabled: true, Set: map[string]any{"store": false}},
		{Name: "same", Model: "m", Enabled: true, Set: map[string]any{"store": true}},
	}
	errs := cfg.Validate()
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), `duplicate request rewrite rule "same"`) {
		t.Fatalf("Validate() errors = %v, want duplicate rule error", errs)
	}
}

func TestValidateRejectsInvalidModelsSource(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Default().Server,
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", ModelsSource: "manual"}},
	}

	err := cfg.Validate()
	if len(err) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", err)
	}
	if got := err[0].Error(); got != `provider "p1" has invalid models_source "manual"` {
		t.Fatalf("Validate() error = %q", got)
	}
}

func TestValidateRejectsModelsSourceWithoutModels(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Default().Server,
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", ModelsSource: "discovered"}},
	}

	err := cfg.Validate()
	if len(err) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", err)
	}
	if got := err[0].Error(); got != `provider "p1" has models_source "discovered" but no models` {
		t.Fatalf("Validate() error = %q", got)
	}
}

func TestSavePreservesEmptyCollectionsAsArrays(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = t.TempDir() + "/config.json"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Providers == nil || loaded.Aliases == nil {
		t.Fatalf("round-trip nil slices: providers=%#v aliases=%#v", loaded.Providers, loaded.Aliases)
	}

	var raw map[string]any
	data, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := raw["providers"].([]any); !ok {
		t.Fatalf("providers JSON = %#v, want array", raw["providers"])
	}
	if _, ok := raw["aliases"].([]any); !ok {
		t.Fatalf("aliases JSON = %#v, want array", raw["aliases"])
	}
}

func TestSaveLinearizesConcurrentWriters(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	lockFile, err := os.OpenFile(cfg.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(lock): %v", err)
	}
	defer lockFile.Close()
	if err := lockTestFile(lockFile); err != nil {
		t.Fatalf("Flock(lock): %v", err)
	}

	startedFirst := make(chan struct{})
	secondDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		cfg.UpsertProvider(Provider{ID: "first", BaseURL: "https://first.example.com/v1"})
		close(startedFirst)
		if err := cfg.Save(); err != nil {
			t.Errorf("first Save() error = %v", err)
		}
	}()

	<-startedFirst
	time.Sleep(20 * time.Millisecond)

	go func() {
		defer wg.Done()
		cfg.UpsertProvider(Provider{ID: "second", BaseURL: "https://second.example.com/v1"})
		if err := cfg.Save(); err != nil {
			t.Errorf("second Save() error = %v", err)
		}
		close(secondDone)
	}()

	select {
	case <-secondDone:
		t.Fatal("second Save() completed before external file lock was released")
	case <-time.After(20 * time.Millisecond):
	}

	if err := unlockTestFile(lockFile); err != nil {
		t.Fatalf("Flock(unlock): %v", err)
	}

	wg.Wait()

	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.FindProvider("first") == nil {
		t.Fatalf("final config = %#v, want first provider persisted", loaded.Providers)
	}
	if loaded.FindProvider("second") == nil {
		t.Fatalf("final config = %#v, want latest provider persisted", loaded.Providers)
	}
}
