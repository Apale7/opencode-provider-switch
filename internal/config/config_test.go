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
