package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
		{name: "valid without v1", input: "https://example.com/api"},
		{name: "valid bare host", input: "https://example.com"},
		{name: "missing", input: "", wantErr: "missing base_url"},
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
	if err := ValidateProviderBaseURL(ProtocolAnthropicMessages, "https://api.anthropic.com/api"); err != nil {
		t.Fatalf("unexpected error for non-/v1 anthropic base url: %v", err)
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

func TestDefaultFailoverStatusCodes(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if !reflect.DeepEqual(cfg.Server.FailoverStatusCodes, []int{401, 402, 403, 429}) {
		t.Fatalf("default failover status codes = %#v", cfg.Server.FailoverStatusCodes)
	}

	loaded, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded.Server.FailoverStatusCodes, []int{401, 402, 403, 429}) {
		t.Fatalf("loaded defaults = %#v", loaded.Server.FailoverStatusCodes)
	}
}

func TestFailoverStatusCodesSaveLoadNormalize(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.Server.FailoverStatusCodes = []int{429, 401, 401, 402, 403}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded.Server.FailoverStatusCodes, []int{401, 402, 403, 429}) {
		t.Fatalf("normalized failover status codes = %#v", loaded.Server.FailoverStatusCodes)
	}
}

func TestFailoverStatusCodesAllowExplicitEmpty(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.Server.FailoverStatusCodes = []int{}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Server.FailoverStatusCodes) != 0 {
		t.Fatalf("failover status codes = %#v, want empty", loaded.Server.FailoverStatusCodes)
	}
}

func TestValidateRejectsInvalidFailoverStatusCode(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Server.FailoverStatusCodes = []int{99}
	errs := cfg.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "server.failover_status_codes") {
		t.Fatalf("Validate() errors = %#v", errs)
	}
}

func TestStreamPrecommitBufferDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.Server.StreamPrecommitBufferMs != 0 {
		t.Fatalf("default stream precommit buffer = %d, want 0", cfg.Server.StreamPrecommitBufferMs)
	}
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.Server.StreamPrecommitBufferMs = 5000
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Server.StreamPrecommitBufferMs != 5000 {
		t.Fatalf("loaded stream precommit buffer = %d, want 5000", loaded.Server.StreamPrecommitBufferMs)
	}
	cfg.Server.StreamPrecommitBufferMs = -1
	errs := cfg.Validate()
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "server.stream_precommit_buffer_ms") {
		t.Fatalf("Validate() errors = %#v, want stream_precommit_buffer_ms error", errs)
	}
}

func TestExcludeFirstTokenLatencyFromRateDefaultsAndSaveLoad(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if !cfg.Server.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("default exclude first token latency from rate = false, want true")
	}
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.Server.ExcludeFirstTokenLatencyFromRate = false
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Server.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("loaded exclude first token latency from rate = true, want false")
	}
}

func TestRequestRewriteRulesSaveLoadAndApply(t *testing.T) {
	t.Parallel()
	insertIndex := 1

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	cfg.RequestRewriteRules = []RequestRewriteRule{
		{
			Name:    "fast-tier",
			Alias:   "gpt-5.5-fast",
			Enabled: true,
			Ops: []RequestRewriteOperation{
				{Op: RequestRewriteOpSet, Path: "$.service_tier", Value: "priority", ValueSet: true},
				{Op: RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true},
				{Op: RequestRewriteOpSet, Path: "$.reasoning.effort", Value: "medium", ValueSet: true},
				{Op: RequestRewriteOpSet, Path: `$['meta:data']`, Value: "ok", ValueSet: true},
			},
		},
		{
			Name:      "provider-override",
			Alias:     "gpt-5.5-fast",
			Providers: []string{"p1"},
			Enabled:   true,
			Override:  true,
			Ops: []RequestRewriteOperation{
				{Op: RequestRewriteOpSet, Path: "$.reasoning.effort", Value: "high", ValueSet: true},
				{Op: RequestRewriteOpDelete, Path: "$.parallel_tool_calls"},
				{Op: RequestRewriteOpAppend, Path: "$.include", Value: "reasoning.encrypted_content", ValueSet: true},
				{Op: RequestRewriteOpInsert, Path: "$.tools", Index: &insertIndex, Value: map[string]any{"type": "web_search"}, ValueSet: true},
			},
		},
		{
			Name:    "disabled",
			Alias:   "gpt-5.5-fast",
			Enabled: false,
			Ops:     []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.disabled_field", Value: true, ValueSet: true}},
		},
		{
			Name:    "legacy",
			Alias:   "gpt-5.5-fast",
			Enabled: true,
			Set:     map[string]any{"legacy_field": true},
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.RequestRewriteRules) != 4 {
		t.Fatalf("rewrite rule count = %d, want 4", len(loaded.RequestRewriteRules))
	}
	if loaded.RequestRewriteRules[0].Name != "fast-tier" || !loaded.RequestRewriteRules[0].Enabled {
		t.Fatalf("first rule = %#v", loaded.RequestRewriteRules[0])
	}

	payload := map[string]any{
		"model":               "gpt-5.5",
		"service_tier":        "standard",
		"parallel_tool_calls": true,
		"include":             []any{"base"},
		"tools":               []any{map[string]any{"type": "function"}},
	}
	loaded.ApplyRequestRewriteRules("gpt-5.5-fast", "p1", "gpt-5.5", payload)
	if got := payload["service_tier"]; got != "standard" {
		t.Fatalf("service_tier = %#v, want request value", got)
	}
	if got := payload["store"]; got != false {
		t.Fatalf("store = %#v, want false", got)
	}
	if got := payload["meta:data"]; got != "ok" {
		t.Fatalf("quoted path value = %#v, want ok", got)
	}
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v, want effort high", payload["reasoning"])
	}
	if _, ok := payload["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls still present: %#v", payload)
	}
	include, ok := payload["include"].([]any)
	if !ok || len(include) != 2 || include[1] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", payload["include"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 2 || tools[1].(map[string]any)["type"] != "web_search" {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	if _, ok := payload["disabled_field"]; ok {
		t.Fatalf("disabled rule applied: %#v", payload)
	}
	if _, ok := payload["legacy_field"]; ok {
		t.Fatalf("legacy rule applied: %#v", payload)
	}

	otherProviderPayload := map[string]any{
		"model":               "gpt-5.5",
		"parallel_tool_calls": true,
	}
	loaded.ApplyRequestRewriteRules("gpt-5.5-fast", "p2", "gpt-5.5", otherProviderPayload)
	reasoning, ok = otherProviderPayload["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("alias-wide rule did not set nested reasoning: %#v", otherProviderPayload)
	}
	if reasoning["effort"] == "high" {
		t.Fatalf("provider-scoped rule applied to non-selected provider: %#v", otherProviderPayload)
	}
	if got := otherProviderPayload["store"]; got != false {
		t.Fatalf("alias-wide rule did not apply to provider without explicit scope: %#v", otherProviderPayload)
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
			rule:    RequestRewriteRule{Alias: "chat", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true}}},
			wantErr: "empty name",
		},
		{
			name:    "missing alias",
			rule:    RequestRewriteRule{Name: "missing-scope", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true}}},
			wantErr: "requires alias",
		},
		{
			name:    "missing operation",
			rule:    RequestRewriteRule{Name: "missing-op", Alias: "chat", Enabled: true},
			wantErr: "requires ops",
		},
		{
			name:    "delete without override",
			rule:    RequestRewriteRule{Name: "bad-delete", Alias: "chat", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpDelete, Path: "$.store"}}},
			wantErr: "delete requires override",
		},
		{
			name:    "invalid path",
			rule:    RequestRewriteRule{Name: "bad-path", Alias: "chat", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "store", Value: false, ValueSet: true}}},
			wantErr: "path must start with $",
		},
		{
			name:    "set missing value",
			rule:    RequestRewriteRule{Name: "missing-value", Alias: "chat", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.store"}}},
			wantErr: "set requires value",
		},
		{
			name: "valid",
			rule: RequestRewriteRule{Name: "valid", Alias: "chat", Providers: []string{"p1"}, Enabled: true, Override: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpDelete, Path: "$.store"}}},
		},
		{
			name: "legacy does not block validation",
			rule: RequestRewriteRule{Name: "legacy", Alias: "chat", Enabled: true, Set: map[string]any{"store": false}},
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
		{Name: "same", Alias: "b", Providers: []string{"p1"}, Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.store", Value: true, ValueSet: true}}},
	}
	errs := cfg.Validate()
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), `duplicate request rewrite rule "same"`) {
		t.Fatalf("Validate() errors = %v, want duplicate rule error", errs)
	}
}

func TestReorderRequestRewriteRulesPreservesStateAndRejectsBadOrders(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.RequestRewriteRules = []RequestRewriteRule{
		{Name: "first", Alias: "chat", Enabled: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true}}},
		{Name: "second", Alias: "chat", Providers: []string{"p1"}, Enabled: false, Override: true, Ops: []RequestRewriteOperation{{Op: RequestRewriteOpDelete, Path: "$.store"}}},
	}

	if err := cfg.ReorderRequestRewriteRules([]string{"second", "first"}); err != nil {
		t.Fatalf("ReorderRequestRewriteRules() error = %v", err)
	}
	rules := cfg.RequestRewriteRulesSnapshot()
	if len(rules) != 2 || rules[0].Name != "second" || rules[0].Enabled || rules[1].Name != "first" || !rules[1].Enabled {
		t.Fatalf("rules after reorder = %#v", rules)
	}

	for _, names := range [][]string{{"first"}, {"first", "first"}, {"first", "missing"}} {
		if err := cfg.ReorderRequestRewriteRules(names); err == nil {
			t.Fatalf("ReorderRequestRewriteRules(%#v) error = nil, want error", names)
		}
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

// Auto-alias simplify (Task 1.5): generation, lookup, priority, and cleanup.

func TestAutoGenerateAliases_CreatesNewAlias(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AutoAliasEnabled: true,
		Providers: []Provider{
			{ID: "p1", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p1.example.com/v1", Models: []string{"gpt-test"}},
		},
	}
	provider := cfg.Providers[0]

	created, updated := cfg.AutoGenerateAliases(provider)
	if !slices.Equal(created, []string{"gpt-test"}) {
		t.Fatalf("created = %#v, want [gpt-test]", created)
	}
	if len(updated) != 0 {
		t.Fatalf("updated = %#v, want empty", updated)
	}

	alias := cfg.FindAutoAlias("gpt-test")
	if alias == nil {
		t.Fatal("expected auto alias gpt-test")
	}
	if !alias.AutoGenerated || alias.Locked {
		t.Fatalf("alias flags = auto=%v locked=%v, want auto=true locked=false", alias.AutoGenerated, alias.Locked)
	}
	if alias.Protocol != ProtocolOpenAIResponses {
		t.Fatalf("alias protocol = %q, want %q", alias.Protocol, ProtocolOpenAIResponses)
	}
	if !alias.Enabled {
		t.Fatal("alias should be enabled")
	}
	wantTargets := []Target{{
		Provider:      "p1",
		Model:         "gpt-test",
		Enabled:       true,
		AutoGenerated: true,
	}}
	if !slices.Equal(alias.Targets, wantTargets) {
		t.Fatalf("targets = %#v, want %#v", alias.Targets, wantTargets)
	}
	if cfg.FindAlias("gpt-test") != nil {
		t.Fatal("FindAlias should not return auto-generated alias")
	}
}

func TestAutoGenerateAliases_AppendsTarget(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AutoAliasEnabled: true,
		ProviderPriority: []string{"p1", "p2"},
		Providers: []Provider{
			{ID: "p1", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p1.example.com/v1", Models: []string{"shared"}},
			{ID: "p2", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p2.example.com/v1", Models: []string{"shared"}},
		},
		Aliases: []Alias{{
			Alias:         "shared",
			Protocol:      ProtocolOpenAIResponses,
			Enabled:       true,
			AutoGenerated: true,
			Targets: []Target{{
				Provider:      "p1",
				Model:         "shared",
				Enabled:       true,
				AutoGenerated: true,
			}},
		}},
	}

	created, updated := cfg.AutoGenerateAliases(cfg.Providers[1])
	if len(created) != 0 {
		t.Fatalf("created = %#v, want empty", created)
	}
	if !slices.Equal(updated, []string{"shared"}) {
		t.Fatalf("updated = %#v, want [shared]", updated)
	}

	alias := cfg.FindAutoAlias("shared")
	if alias == nil {
		t.Fatal("expected auto alias shared")
	}
	wantTargets := []Target{
		{Provider: "p1", Model: "shared", Enabled: true, AutoGenerated: true},
		{Provider: "p2", Model: "shared", Enabled: true, AutoGenerated: true},
	}
	if !slices.Equal(alias.Targets, wantTargets) {
		t.Fatalf("targets = %#v, want %#v", alias.Targets, wantTargets)
	}

	// Idempotent: existing provider+model target is not duplicated.
	created, updated = cfg.AutoGenerateAliases(cfg.Providers[1])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("second call created=%#v updated=%#v, want both empty", created, updated)
	}
	if !slices.Equal(cfg.FindAutoAlias("shared").Targets, wantTargets) {
		t.Fatalf("targets changed after idempotent call: %#v", cfg.FindAutoAlias("shared").Targets)
	}
}

func TestAutoGenerateAliases_SkipsLockedAlias(t *testing.T) {
	t.Parallel()

	original := Alias{
		Alias:         "locked-model",
		Protocol:      ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: true,
		Locked:        true,
		Targets: []Target{{
			Provider:      "p1",
			Model:         "locked-model",
			Enabled:       true,
			AutoGenerated: true,
		}},
	}
	cfg := &Config{
		AutoAliasEnabled: true,
		Providers: []Provider{
			{ID: "p2", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p2.example.com/v1", Models: []string{"locked-model"}},
		},
		Aliases: []Alias{original},
	}

	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("created=%#v updated=%#v, want both empty", created, updated)
	}
	got := cfg.FindAutoAlias("locked-model")
	if got == nil {
		t.Fatal("locked auto alias disappeared")
	}
	if !slices.Equal(got.Targets, original.Targets) {
		t.Fatalf("locked targets changed: %#v", got.Targets)
	}
	if !got.Locked || !got.AutoGenerated {
		t.Fatalf("locked flags changed: auto=%v locked=%v", got.AutoGenerated, got.Locked)
	}
}

func TestAutoGenerateAliases_SkipsManualAlias(t *testing.T) {
	t.Parallel()

	original := Alias{
		Alias:         "manual-model",
		Protocol:      ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: false,
		Targets: []Target{{
			Provider: "manual-p",
			Model:    "upstream-x",
			Enabled:  true,
		}},
	}
	cfg := &Config{
		AutoAliasEnabled: true,
		Providers: []Provider{
			{ID: "p1", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p1.example.com/v1", Models: []string{"manual-model"}},
		},
		Aliases: []Alias{original},
	}

	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("created=%#v updated=%#v, want both empty", created, updated)
	}
	got := cfg.FindAlias("manual-model")
	if got == nil {
		t.Fatal("manual alias disappeared")
	}
	if got.AutoGenerated {
		t.Fatal("manual alias became auto-generated")
	}
	if !slices.Equal(got.Targets, original.Targets) {
		t.Fatalf("manual targets changed: %#v", got.Targets)
	}
	if cfg.FindAutoAlias("manual-model") != nil {
		t.Fatal("FindAutoAlias should not return manual alias")
	}
}

func TestAutoGenerateAliases_RespectsPriority(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AutoAliasEnabled: true,
		ProviderPriority: []string{"p-high", "p-low"},
		Providers: []Provider{
			{ID: "p-low", Protocol: ProtocolOpenAIResponses, BaseURL: "https://low.example.com/v1", Models: []string{"prio-model"}},
			{ID: "p-high", Protocol: ProtocolOpenAIResponses, BaseURL: "https://high.example.com/v1", Models: []string{"prio-model"}},
		},
	}

	// Create from lower-priority provider first, then append higher-priority one.
	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if !slices.Equal(created, []string{"prio-model"}) || len(updated) != 0 {
		t.Fatalf("first call created=%#v updated=%#v", created, updated)
	}
	created, updated = cfg.AutoGenerateAliases(cfg.Providers[1])
	if len(created) != 0 || !slices.Equal(updated, []string{"prio-model"}) {
		t.Fatalf("second call created=%#v updated=%#v", created, updated)
	}

	alias := cfg.FindAutoAlias("prio-model")
	if alias == nil {
		t.Fatal("expected auto alias prio-model")
	}
	if len(alias.Targets) != 2 {
		t.Fatalf("targets = %#v, want 2", alias.Targets)
	}
	if alias.Targets[0].Provider != "p-high" || alias.Targets[1].Provider != "p-low" {
		t.Fatalf("target order = %q then %q, want p-high then p-low", alias.Targets[0].Provider, alias.Targets[1].Provider)
	}
}

func TestAutoGenerateAliases_DisabledWhenFlagOff(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AutoAliasEnabled: false,
		Providers: []Provider{
			{ID: "p1", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p1.example.com/v1", Models: []string{"off-model"}},
		},
	}

	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("created=%#v updated=%#v, want both empty when flag off", created, updated)
	}
	if len(cfg.Aliases) != 0 {
		t.Fatalf("aliases = %#v, want none", cfg.Aliases)
	}
	if cfg.IsAutoAliasEnabled() {
		t.Fatal("IsAutoAliasEnabled() = true, want false")
	}
}

func TestFindProvidersByModel(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ProviderPriority: []string{"p2", "p1", "p3"},
		Providers: []Provider{
			{ID: "p1", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p1.example.com/v1", Models: []string{"m-a", "m-b"}},
			{ID: "p2", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p2.example.com/v1", Models: []string{"m-a"}},
			{ID: "p3", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p3.example.com/v1", Models: []string{"m-a"}, Disabled: true},
			{ID: "p4", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p4.example.com/v1", Models: []string{"m-b"}},
		},
	}

	got := cfg.FindProvidersByModel("m-a")
	if len(got) != 2 {
		t.Fatalf("FindProvidersByModel(m-a) = %#v, want 2 providers", got)
	}
	if got[0].ID != "p2" || got[1].ID != "p1" {
		t.Fatalf("order = %q, %q; want p2, p1", got[0].ID, got[1].ID)
	}

	gotB := cfg.FindProvidersByModel("m-b")
	if len(gotB) != 2 || gotB[0].ID != "p1" || gotB[1].ID != "p4" {
		t.Fatalf("FindProvidersByModel(m-b) = %#v, want p1 then p4", gotB)
	}

	if got := cfg.FindProvidersByModel("missing"); len(got) != 0 {
		t.Fatalf("missing model = %#v, want empty", got)
	}
	if got := cfg.FindProvidersByModel("  "); len(got) != 0 {
		t.Fatalf("blank model = %#v, want empty", got)
	}
}

func TestRemoveProviderAutoTargets(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Aliases: []Alias{
			{
				Alias:         "only-p1",
				Enabled:       true,
				AutoGenerated: true,
				Targets: []Target{
					{Provider: "p1", Model: "only-p1", Enabled: true, AutoGenerated: true},
				},
			},
			{
				Alias:         "shared",
				Enabled:       true,
				AutoGenerated: true,
				Targets: []Target{
					{Provider: "p1", Model: "shared", Enabled: true, AutoGenerated: true},
					{Provider: "p2", Model: "shared", Enabled: true, AutoGenerated: true},
				},
			},
			{
				Alias:         "locked-auto",
				Enabled:       true,
				AutoGenerated: true,
				Locked:        true,
				Targets: []Target{
					{Provider: "p1", Model: "locked-auto", Enabled: true, AutoGenerated: true},
				},
			},
			{
				Alias:   "manual",
				Enabled: true,
				Targets: []Target{
					{Provider: "p1", Model: "manual-up", Enabled: true},
				},
			},
		},
	}

	emptied := cfg.RemoveProviderAutoTargets("p1")
	if !slices.Equal(emptied, []string{"only-p1"}) {
		t.Fatalf("emptied = %#v, want [only-p1]", emptied)
	}

	if cfg.FindAutoAlias("only-p1") != nil {
		t.Fatal("empty auto alias only-p1 should be removed")
	}

	shared := cfg.FindAutoAlias("shared")
	if shared == nil {
		t.Fatal("shared auto alias should remain")
	}
	wantShared := []Target{{Provider: "p2", Model: "shared", Enabled: true, AutoGenerated: true}}
	if !slices.Equal(shared.Targets, wantShared) {
		t.Fatalf("shared targets = %#v, want %#v", shared.Targets, wantShared)
	}

	locked := cfg.FindAutoAlias("locked-auto")
	if locked == nil || len(locked.Targets) != 1 || locked.Targets[0].Provider != "p1" {
		t.Fatalf("locked auto alias changed: %#v", locked)
	}

	manual := cfg.FindAlias("manual")
	if manual == nil || len(manual.Targets) != 1 || manual.Targets[0].Provider != "p1" {
		t.Fatalf("manual alias changed: %#v", manual)
	}
}

func TestFindAliasAndFindAutoAliasSeparation(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Aliases: []Alias{
			{
				Alias:         "auto-only",
				Enabled:       true,
				AutoGenerated: true,
				Targets:       []Target{{Provider: "p1", Model: "auto-only", Enabled: true, AutoGenerated: true}},
			},
			{
				Alias:   "manual-only",
				Enabled: true,
				Targets: []Target{{Provider: "p1", Model: "manual-up", Enabled: true}},
			},
		},
	}

	if cfg.FindAlias("auto-only") != nil {
		t.Fatal("FindAlias must ignore auto-generated aliases")
	}
	if cfg.FindAutoAlias("auto-only") == nil {
		t.Fatal("FindAutoAlias should find auto-only")
	}
	if cfg.FindAlias("manual-only") == nil {
		t.Fatal("FindAlias should find manual-only")
	}
	if cfg.FindAutoAlias("manual-only") != nil {
		t.Fatal("FindAutoAlias must ignore manual aliases")
	}
	if cfg.FindAlias("missing") != nil || cfg.FindAutoAlias("missing") != nil {
		t.Fatal("missing name should return nil for both finders")
	}
}

func TestSetProviderPriority_DedupesCompletesAndReordersAutoTargets(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1"},
			{ID: "p3", BaseURL: "https://p3.example.com/v1"},
		},
		Aliases: []Alias{
			{
				Alias:         "auto-m",
				Enabled:       true,
				AutoGenerated: true,
				Targets: []Target{
					{Provider: "p3", Model: "auto-m", Enabled: true, AutoGenerated: true},
					{Provider: "p1", Model: "auto-m", Enabled: true, AutoGenerated: true},
					{Provider: "p2", Model: "auto-m", Enabled: true, AutoGenerated: true},
				},
			},
			{
				Alias:         "locked-m",
				Enabled:       true,
				AutoGenerated: true,
				Locked:        true,
				Targets: []Target{
					{Provider: "p3", Model: "locked-m", Enabled: true, AutoGenerated: true},
					{Provider: "p1", Model: "locked-m", Enabled: true, AutoGenerated: true},
				},
			},
			{
				Alias:   "manual-m",
				Enabled: true,
				Targets: []Target{
					{Provider: "p3", Model: "manual-m", Enabled: true},
					{Provider: "p1", Model: "manual-m", Enabled: true},
				},
			},
		},
	}

	cfg.SetProviderPriority([]string{"p2", "unknown", "p2", "p1", ""})

	wantPriority := []string{"p2", "p1", "p3"}
	if !slices.Equal(cfg.ProviderPriority, wantPriority) {
		t.Fatalf("ProviderPriority = %#v, want %#v", cfg.ProviderPriority, wantPriority)
	}
	if !slices.Equal(cfg.ProviderPriorityOrder(), wantPriority) {
		t.Fatalf("ProviderPriorityOrder() = %#v, want %#v", cfg.ProviderPriorityOrder(), wantPriority)
	}

	auto := cfg.FindAutoAlias("auto-m")
	if auto == nil {
		t.Fatal("auto-m missing")
	}
	if auto.Targets[0].Provider != "p2" || auto.Targets[1].Provider != "p1" || auto.Targets[2].Provider != "p3" {
		t.Fatalf("auto targets order = %#v, want p2,p1,p3", auto.Targets)
	}

	locked := cfg.FindAutoAlias("locked-m")
	if locked == nil || locked.Targets[0].Provider != "p3" || locked.Targets[1].Provider != "p1" {
		t.Fatalf("locked targets should stay original order: %#v", locked)
	}

	manual := cfg.FindAlias("manual-m")
	if manual == nil || manual.Targets[0].Provider != "p3" || manual.Targets[1].Provider != "p1" {
		t.Fatalf("manual targets should stay original order: %#v", manual)
	}
}

func TestAutoAliasEnabled_DefaultTrueAndExplicitFalse(t *testing.T) {
	t.Parallel()

	if !Default().IsAutoAliasEnabled() {
		t.Fatal("Default().IsAutoAliasEnabled() = false, want true")
	}

	dir := t.TempDir()

	// Legacy config without auto_alias_enabled keeps the Default() true value.
	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := `{"server":{"host":"127.0.0.1","port":9982},"providers":[],"aliases":[]}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy): %v", err)
	}
	loadedLegacy, err := Load(legacyPath)
	if err != nil {
		t.Fatalf("Load(legacy) error = %v", err)
	}
	if !loadedLegacy.IsAutoAliasEnabled() {
		t.Fatal("legacy config missing auto_alias_enabled should default to enabled")
	}

	// Explicit false must remain off after Load.
	offPath := filepath.Join(dir, "off.json")
	off := `{"server":{"host":"127.0.0.1","port":9982},"providers":[],"aliases":[],"auto_alias_enabled":false}`
	if err := os.WriteFile(offPath, []byte(off), 0o600); err != nil {
		t.Fatalf("WriteFile(off): %v", err)
	}
	loadedOff, err := Load(offPath)
	if err != nil {
		t.Fatalf("Load(off) error = %v", err)
	}
	if loadedOff.IsAutoAliasEnabled() {
		t.Fatal("explicit auto_alias_enabled=false should stay disabled")
	}

	// Save/Load round-trip preserves explicit false (non-omitempty persistence).
	cfg := Default()
	cfg.path = filepath.Join(dir, "roundtrip.json")
	cfg.AutoAliasEnabled = false
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load(roundtrip) error = %v", err)
	}
	if reloaded.IsAutoAliasEnabled() {
		t.Fatal("round-trip should preserve AutoAliasEnabled=false")
	}
}

func TestProviderAutoAliasEnabled_DefaultTrueAndExplicitFalse(t *testing.T) {
	t.Parallel()

	var legacy Provider
	if !legacy.EffectiveAutoAliasEnabled() || !legacy.IsAutoAliasEnabled() {
		t.Fatal("nil AutoAliasEnabled should default to true")
	}

	off := false
	pOff := Provider{AutoAliasEnabled: &off}
	if pOff.EffectiveAutoAliasEnabled() {
		t.Fatal("explicit false should disable provider auto alias")
	}

	on := true
	pOn := Provider{AutoAliasEnabled: &on}
	if !pOn.EffectiveAutoAliasEnabled() {
		t.Fatal("explicit true should enable provider auto alias")
	}

	dir := t.TempDir()
	// Legacy provider without auto_alias_enabled field.
	legacyPath := filepath.Join(dir, "provider-legacy.json")
	legacyJSON := `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p1","base_url":"https://p1.example.com/v1","api_key":"sk"}],"aliases":[]}`
	if err := os.WriteFile(legacyPath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy): %v", err)
	}
	loadedLegacy, err := Load(legacyPath)
	if err != nil {
		t.Fatalf("Load(legacy) error = %v", err)
	}
	p := loadedLegacy.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 missing")
	}
	if p.AutoAliasEnabled != nil {
		t.Fatalf("legacy provider AutoAliasEnabled = %#v, want nil", p.AutoAliasEnabled)
	}
	if !p.EffectiveAutoAliasEnabled() {
		t.Fatal("legacy provider should default auto alias enabled")
	}

	// Persist explicit false and round-trip.
	cfg := Default()
	cfg.path = filepath.Join(dir, "provider-off.json")
	falseVal := false
	cfg.UpsertProvider(Provider{
		ID:               "p-off",
		BaseURL:          "https://off.example.com/v1",
		APIKey:           "sk-off",
		AutoAliasEnabled: &falseVal,
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := reloaded.FindProvider("p-off")
	if got == nil {
		t.Fatal("provider p-off missing after reload")
	}
	if got.AutoAliasEnabled == nil || *got.AutoAliasEnabled {
		t.Fatalf("AutoAliasEnabled = %#v, want false", got.AutoAliasEnabled)
	}
	if got.EffectiveAutoAliasEnabled() {
		t.Fatal("EffectiveAutoAliasEnabled() = true, want false")
	}
}

func TestAutoGenerateAliases_RespectsProviderSwitch(t *testing.T) {
	t.Parallel()

	falseVal := false
	cfg := &Config{
		AutoAliasEnabled: true,
		Providers: []Provider{{
			ID:               "p1",
			Protocol:         ProtocolOpenAIResponses,
			BaseURL:          "https://p1.example.com/v1",
			Models:           []string{"provider-off-model"},
			AutoAliasEnabled: &falseVal,
		}},
	}
	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("created=%#v updated=%#v, want both empty when provider switch off", created, updated)
	}
	if len(cfg.Aliases) != 0 {
		t.Fatalf("aliases = %#v, want none", cfg.Aliases)
	}
}

func TestSetAutoAliasEnabled(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if !cfg.IsAutoAliasEnabled() {
		t.Fatal("default should be enabled")
	}
	cfg.SetAutoAliasEnabled(false)
	if cfg.IsAutoAliasEnabled() {
		t.Fatal("SetAutoAliasEnabled(false) did not stick")
	}
	cfg.SetAutoAliasEnabled(true)
	if !cfg.IsAutoAliasEnabled() {
		t.Fatal("SetAutoAliasEnabled(true) did not stick")
	}
}

func TestLockAutoAlias(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AutoAliasEnabled: true,
		Aliases: []Alias{{
			Alias:         "auto-lock",
			Protocol:      ProtocolOpenAIResponses,
			Enabled:       true,
			AutoGenerated: true,
			Targets: []Target{{
				Provider:      "p1",
				Model:         "auto-lock",
				Enabled:       true,
				AutoGenerated: true,
			}},
		}, {
			Alias:    "manual",
			Protocol: ProtocolOpenAIResponses,
			Enabled:  true,
			Targets:  []Target{{Provider: "p1", Model: "manual", Enabled: true}},
		}},
	}

	locked, err := cfg.LockAutoAlias("auto-lock")
	if err != nil {
		t.Fatalf("LockAutoAlias() error = %v", err)
	}
	if !locked.Locked || !locked.AutoGenerated {
		t.Fatalf("locked flags = auto=%v locked=%v", locked.AutoGenerated, locked.Locked)
	}
	got := cfg.FindAutoAlias("auto-lock")
	if got == nil || !got.Locked || !got.AutoGenerated {
		t.Fatalf("stored alias = %#v", got)
	}

	// Locked alias is not modified by generation or cleanup.
	cfg.Providers = []Provider{{
		ID: "p2", Protocol: ProtocolOpenAIResponses, BaseURL: "https://p2.example.com/v1",
		Models: []string{"auto-lock"},
	}}
	created, updated := cfg.AutoGenerateAliases(cfg.Providers[0])
	if len(created) != 0 || len(updated) != 0 {
		t.Fatalf("generation touched locked alias: created=%#v updated=%#v", created, updated)
	}
	emptied := cfg.RemoveProviderAutoTargets("p1")
	if len(emptied) != 0 {
		t.Fatalf("cleanup emptied locked alias: %#v", emptied)
	}
	still := cfg.FindAutoAlias("auto-lock")
	if still == nil || len(still.Targets) != 1 || still.Targets[0].Provider != "p1" {
		t.Fatalf("locked targets changed: %#v", still)
	}

	if _, err := cfg.LockAutoAlias("manual"); err == nil {
		t.Fatal("LockAutoAlias(manual) should fail")
	}
	if _, err := cfg.LockAutoAlias("missing"); err == nil {
		t.Fatal("LockAutoAlias(missing) should fail")
	}
	if _, err := cfg.LockAutoAlias(""); err == nil {
		t.Fatal("LockAutoAlias(\"\") should fail")
	}
}

func TestLoadAppliesProviderPriorityToAutoAliasTargets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	raw := `{
  "providers": [
    {"id":"p1","base_url":"https://p1.example","models":["model"]},
    {"id":"p2","base_url":"https://p2.example","models":["model"]}
  ],
  "provider_priority": ["p2", "p1"],
  "aliases": [{
    "alias":"model",
    "enabled":true,
    "auto_generated":true,
    "targets":[
      {"provider":"p1","model":"model","enabled":true,"auto_generated":true},
      {"provider":"p2","model":"model","enabled":true,"auto_generated":true}
    ]
  }]
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	alias := cfg.FindAutoAlias("model")
	if alias == nil || len(alias.Targets) != 2 {
		t.Fatalf("FindAutoAlias() = %#v", alias)
	}
	if got := []string{alias.Targets[0].Provider, alias.Targets[1].Provider}; !reflect.DeepEqual(got, []string{"p2", "p1"}) {
		t.Fatalf("target order = %#v, want [p2 p1]", got)
	}
}
