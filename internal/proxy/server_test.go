package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
	_ "modernc.org/sqlite"
)

// newLegacyTestServer adapts pre-v2 in-memory fixtures. Production v1 migration
// happens in config decoding; provider-group tests call New directly to verify
// that unresolved runtime targets fail closed.
func newLegacyTestServer(cfg *config.Config) *Server {
	if cfg != nil {
		for i := range cfg.Aliases {
			for j := range cfg.Aliases[i].Targets {
				if strings.TrimSpace(cfg.Aliases[i].Targets[j].Group) == "" {
					cfg.Aliases[i].Targets[j].Group = config.DefaultGroupID
				}
			}
		}
	}
	return New(cfg)
}

func TestServerRuntimeRejectsUpstreamRedirects(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	runtime := newServerRuntime(config.Default(), routing.NewMemoryStateStore())
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("X-Api-Key", "group-secret")
	resp, err := runtime.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || redirected.Load() != 0 {
		t.Fatalf("status = %d, redirected requests = %d", resp.StatusCode, redirected.Load())
	}
}

func TestHandleResponsesWritesOpenAIErrorForMissingAlias(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"missing","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "missing" not found`)
	assertLocalTrace(t, srv, "missing", http.StatusNotFound, "alias_missing", `alias "missing" not found`)
}

func TestHandleResponsesLocalFailuresSetTraceStatusAndReasonCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       *config.Config
		model     string
		wantCode  string
		wantError string
	}{
		{
			name: "alias_missing",
			cfg: &config.Config{
				Server: config.Server{APIKey: config.DefaultLocalAPIKey},
			},
			model:     "gone",
			wantCode:  "alias_missing",
			wantError: `alias "gone" not found`,
		},
		{
			name: "protocol_mismatch",
			cfg: &config.Config{
				Server: config.Server{APIKey: config.DefaultLocalAPIKey},
				Providers: []config.Provider{{
					ID: "p1", BaseURL: "https://example.com",
					Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk"}},
				}},
				Aliases: []config.Alias{{
					Alias: "chat", Protocol: config.ProtocolAnthropicMessages, Enabled: true,
					Targets: []config.Target{{Provider: "p1", Model: "m1", Enabled: true}},
				}},
			},
			model:     "chat",
			wantCode:  "protocol_mismatch",
			wantError: `alias "chat" does not support protocol "openai-responses"`,
		},
		{
			name: "alias_disabled",
			cfg: &config.Config{
				Server: config.Server{APIKey: config.DefaultLocalAPIKey},
				Providers: []config.Provider{{
					ID: "p1", BaseURL: "https://example.com/v1",
					Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk"}},
				}},
				Aliases: []config.Alias{{
					Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: false,
					Targets: []config.Target{{Provider: "p1", Model: "m1", Enabled: true}},
				}},
			},
			model:     "chat",
			wantCode:  "alias_disabled",
			wantError: `alias "chat" is disabled`,
		},
		{
			name: "no_available_target",
			cfg: &config.Config{
				Server: config.Server{APIKey: config.DefaultLocalAPIKey},
				Providers: []config.Provider{{
					ID: "p1", BaseURL: "https://example.com/v1", Disabled: true,
					Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk"}},
				}},
				Aliases: []config.Alias{{
					Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
					Targets: []config.Target{{Provider: "p1", Model: "m1", Enabled: true}},
				}},
			},
			model:     "chat",
			wantCode:  "no_available_target",
			wantError: `alias "chat" has no available targets`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newLegacyTestServer(tc.cfg)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"stream":false}`, tc.model)))
			req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.handleResponses(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
			}
			assertLocalTrace(t, srv, tc.model, http.StatusNotFound, tc.wantCode, tc.wantError)
		})
	}
}

func assertLocalTrace(t *testing.T, srv *Server, alias string, wantStatus int, wantCode, wantError string) {
	t.Helper()
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if len(traces) == 0 {
		t.Fatal("expected at least one trace")
	}
	trace := traces[0]
	if trace.Alias != alias {
		t.Fatalf("trace.Alias = %q, want %q", trace.Alias, alias)
	}
	if trace.StatusCode != wantStatus {
		t.Fatalf("trace.StatusCode = %d, want %d", trace.StatusCode, wantStatus)
	}
	if trace.ErrorCode != wantCode {
		t.Fatalf("trace.ErrorCode = %q, want %q", trace.ErrorCode, wantCode)
	}
	if trace.Error != wantError {
		t.Fatalf("trace.Error = %q, want %q", trace.Error, wantError)
	}
	if trace.Success {
		t.Fatal("trace.Success = true, want false")
	}
}

// TestReloadConfigSwitchesRuntimeOnNoAvailableTargets ensures hot-reload uses
// structural ValidateForPersist: disabling the only group is persistable, so
// ReloadConfig must swap runtime and fail closed instead of keeping revoked keys.
func TestReloadConfigSwitchesRuntimeOnNoAvailableTargets(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	initial := config.Default()
	initial.Providers = []config.Provider{{
		ID: "p1", BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-live",
			Models: []string{"m1"},
		}},
	}}
	initial.Aliases = []config.Alias{{
		Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
		Targets: []config.Target{{
			Provider: "p1", Group: config.DefaultGroupID, Model: "m1", Enabled: true,
		}},
	}}
	if errs := initial.Validate(); len(errs) > 0 {
		t.Fatalf("initial Validate() = %v", errs)
	}

	srv := New(initial)

	// Initially routable: upstream is called.
	reqOK := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"chat","stream":false}`))
	reqOK.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	reqOK.Header.Set("Content-Type", "application/json")
	rrOK := httptest.NewRecorder()
	srv.handleResponses(rrOK, reqOK)
	if rrOK.Code != http.StatusOK {
		t.Fatalf("initial status = %d body=%s", rrOK.Code, rrOK.Body.String())
	}
	if hitCount.Load() != 1 {
		t.Fatalf("initial upstream hits = %d, want 1", hitCount.Load())
	}

	// Disable the only group (ConfigStore would accept this via ValidateForPersist).
	disabled := config.Default()
	disabled.Providers = []config.Provider{{
		ID: "p1", BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-revoked",
			Models: []string{"m1"}, Disabled: true,
		}},
	}}
	disabled.Aliases = []config.Alias{{
		Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
		Targets: []config.Target{{
			Provider: "p1", Group: config.DefaultGroupID, Model: "m1", Enabled: true,
		}},
	}}
	if errs := disabled.ValidateForPersist(); len(errs) > 0 {
		t.Fatalf("disabled ValidateForPersist() = %v, want none", errs)
	}
	if errs := disabled.Validate(); len(errs) == 0 {
		t.Fatal("disabled Validate() = nil, want no available targets error")
	}

	if err := srv.ReloadConfig(disabled); err != nil {
		t.Fatalf("ReloadConfig(disabled group) = %v, want nil", err)
	}

	// After reload: no upstream, fail closed with no available targets.
	beforeFail := hitCount.Load()
	reqFail := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"chat","stream":false}`))
	reqFail.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	reqFail.Header.Set("Content-Type", "application/json")
	rrFail := httptest.NewRecorder()
	srv.handleResponses(rrFail, reqFail)
	if rrFail.Code != http.StatusNotFound {
		t.Fatalf("after reload status = %d body=%s", rrFail.Code, rrFail.Body.String())
	}
	if hitCount.Load() != beforeFail {
		t.Fatalf("after reload upstream hits = %d, want %d (no new call)", hitCount.Load(), beforeFail)
	}
	assertOpenAIError(t, rrFail.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "chat" has no available targets`)
	assertLocalTrace(t, srv, "chat", http.StatusNotFound, "no_available_target", `alias "chat" has no available targets`)

	// Structural invalid (provider with empty groups) must still reject reload.
	// Runtime must keep the disabled-group snapshot (still fail closed, no upstream).
	emptyGroups := &config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: nil,
		}},
		Aliases: []config.Alias{{
			Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
			Targets: []config.Target{{
				Provider: "p1", Group: config.DefaultGroupID, Model: "m1", Enabled: true,
			}},
		}},
	}
	err := srv.ReloadConfig(emptyGroups)
	if err == nil {
		t.Fatal("ReloadConfig(empty groups) = nil, want structural error")
	}
	if !strings.Contains(err.Error(), "has no groups") {
		t.Fatalf("ReloadConfig(empty groups) = %v, want has no groups", err)
	}

	beforeStructural := hitCount.Load()
	reqStructural := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"chat","stream":false}`))
	reqStructural.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	reqStructural.Header.Set("Content-Type", "application/json")
	rrStructural := httptest.NewRecorder()
	srv.handleResponses(rrStructural, reqStructural)
	if rrStructural.Code != http.StatusNotFound {
		t.Fatalf("after rejected reload status = %d body=%s", rrStructural.Code, rrStructural.Body.String())
	}
	if hitCount.Load() != beforeStructural {
		t.Fatalf("after rejected reload upstream hits = %d, want %d", hitCount.Load(), beforeStructural)
	}
}

func TestProxyAPIKeyAuthRejectsConflictingHeaders(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{Server: config.Server{APIKey: "legacy-key"}})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer legacy-key")
	req.Header.Set("X-Api-Key", "other-key")
	rr := httptest.NewRecorder()

	srv.handleModels(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	assertOpenAIError(t, rr.Body.Bytes(), "invalid_api_key", "invalid_request_error", "conflicting api keys")
}

func TestHandleMessagesWritesAnthropicErrorForMissingAlias(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"missing","stream":true}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	assertAnthropicError(t, rr.Body.Bytes(), "invalid_request_error", `alias "missing" not found`)
}

func TestHandleCompletionsProxiesOpenAICompatibleRequest(t *testing.T) {
	t.Parallel()

	var seenPath string
	var seenAuth string
	var seenModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":17,"completion_tokens":9,"total_tokens":26}}`))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Name: config.DefaultGroupName,
				Protocol: config.ProtocolOpenAICompatible, APIKey: "sk-compat",
			}},
		}},
		Aliases: []config.Alias{{
			Alias:    "compat",
			Protocol: config.ProtocolOpenAICompatible,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "p1", Model: "up-compat", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"compat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if seenPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want /v1/chat/completions", seenPath)
	}
	if seenAuth != "Bearer sk-compat" {
		t.Fatalf("authorization = %q, want provider bearer", seenAuth)
	}
	if seenModel != "up-compat" {
		t.Fatalf("model = %q, want up-compat", seenModel)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || traces[0].Protocol != config.ProtocolOpenAICompatible {
		t.Fatalf("traces = %#v, want one openai-compatible trace", traces)
	}
	if traces[0].InputTokens != 17 || traces[0].OutputTokens != 9 {
		t.Fatalf("trace tokens = %d/%d, want 17/9", traces[0].InputTokens, traces[0].OutputTokens)
	}
}

func TestHandleResponsesAppliesRequestRewriteRules(t *testing.T) {
	t.Parallel()

	var seenPayload map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&seenPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[]}`))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}, {ID: "p2", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.5-fast",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "gpt-5.5", Enabled: true}, {Provider: "p2", Model: "gpt-5.5", Enabled: true}},
		}},
		RequestRewriteRules: []config.RequestRewriteRule{
			{Name: "disabled", Alias: "gpt-5.5-fast", Enabled: false, Ops: []config.RequestRewriteOperation{{Op: config.RequestRewriteOpSet, Path: "$.disabled_field", Value: true, ValueSet: true}}},
			{Name: "other-alias", Alias: "other", Enabled: true, Ops: []config.RequestRewriteOperation{{Op: config.RequestRewriteOpSet, Path: "$.other_field", Value: true, ValueSet: true}}},
			{Name: "alias-add", Alias: "gpt-5.5-fast", Enabled: true, Ops: []config.RequestRewriteOperation{
				{Op: config.RequestRewriteOpSet, Path: "$.service_tier", Value: "priority", ValueSet: true},
				{Op: config.RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true},
			}},
			{Name: "provider-override", Alias: "gpt-5.5-fast", ProviderGroups: []config.ProviderGroupSelector{{Provider: "p1", Group: config.DefaultGroupID}}, Enabled: true, Override: true, Ops: []config.RequestRewriteOperation{
				{Op: config.RequestRewriteOpSet, Path: "$.reasoningEffort", Value: "high", ValueSet: true},
				{Op: config.RequestRewriteOpDelete, Path: "$.parallel_tool_calls"},
			}},
			{Name: "other-provider", Alias: "gpt-5.5-fast", ProviderGroups: []config.ProviderGroupSelector{{Provider: "p2", Group: config.DefaultGroupID}}, Enabled: true, Ops: []config.RequestRewriteOperation{{Op: config.RequestRewriteOpSet, Path: "$.other_provider_field", Value: true, ValueSet: true}}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5-fast","stream":false,"service_tier":"standard","reasoningEffort":"low","parallel_tool_calls":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := seenPayload["model"]; got != "gpt-5.5" {
		t.Fatalf("model = %#v, want resolved upstream model", got)
	}
	if got := seenPayload["service_tier"]; got != "standard" {
		t.Fatalf("service_tier = %#v, want request value", got)
	}
	if got := seenPayload["store"]; got != false {
		t.Fatalf("store = %#v, want false", got)
	}
	if got := seenPayload["reasoningEffort"]; got != "high" {
		t.Fatalf("reasoningEffort = %#v, want high", got)
	}
	if _, ok := seenPayload["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls still present: %#v", seenPayload)
	}
	if _, ok := seenPayload["disabled_field"]; ok {
		t.Fatalf("disabled rule applied: %#v", seenPayload)
	}
	if _, ok := seenPayload["other_field"]; ok {
		t.Fatalf("non-matching alias rule applied: %#v", seenPayload)
	}
	if _, ok := seenPayload["other_provider_field"]; ok {
		t.Fatalf("non-selected provider rule applied: %#v", seenPayload)
	}

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || len(traces[0].Attempts) != 1 {
		t.Fatalf("traces = %#v, want one attempt", traces)
	}
	params, ok := traces[0].Attempts[0].RequestParams.(map[string]any)
	if !ok {
		t.Fatalf("trace request params = %#v", traces[0].Attempts[0].RequestParams)
	}
	if got := params["store"]; got != false {
		t.Fatalf("trace store = %#v, want rewritten payload", got)
	}
}

func TestHandleMessagesProxiesAnthropicRequest(t *testing.T) {
	t.Parallel()

	var seenPath string
	var seenAPIKey string
	var seenVersion string
	var seenModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		seenPath = r.URL.Path
		seenAPIKey = r.Header.Get("X-Api-Key")
		seenVersion = r.Header.Get("Anthropic-Version")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","usage":{"input_tokens":11,"output_tokens":7}}`))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:      "anthropic",
			BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-ant-upstream",
			}},
		}},
		Aliases: []config.Alias{{
			Alias:    "claude",
			Protocol: config.ProtocolAnthropicMessages,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "anthropic", Model: "claude-3-7-sonnet", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ocswitch/claude","stream":false}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if seenPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", seenPath)
	}
	if seenAPIKey != "sk-ant-upstream" {
		t.Fatalf("X-Api-Key = %q, want sk-ant-upstream", seenAPIKey)
	}
	if seenVersion != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want 2023-06-01", seenVersion)
	}
	if seenModel != "claude-3-7-sonnet" {
		t.Fatalf("model = %q, want claude-3-7-sonnet", seenModel)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || traces[0].Protocol != config.ProtocolAnthropicMessages {
		t.Fatalf("traces = %#v", traces)
	}
	if traces[0].InputTokens != 11 {
		t.Fatalf("trace input tokens = %d, want 11", traces[0].InputTokens)
	}
	if traces[0].OutputTokens != 7 {
		t.Fatalf("trace output tokens = %d, want 7", traces[0].OutputTokens)
	}
}

func TestHandleMessagesFailsOverOn429(t *testing.T) {
	t.Parallel()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer first.Close()

	var secondSeenModel string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode second payload: %v", err)
		}
		secondSeenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message"}`))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:    "claude",
			Protocol: config.ProtocolAnthropicMessages,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "p1", Model: "claude-a", Enabled: true}, {Provider: "p2", Model: "claude-b", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","stream":false}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if secondSeenModel != "claude-b" {
		t.Fatalf("second upstream model = %q, want claude-b", secondSeenModel)
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
}

func TestHandleResponsesFailsOverOn429(t *testing.T) {
	t.Parallel()

	var firstSeenModel string
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode first payload: %v", err)
		}
		firstSeenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer first.Close()

	var secondSeenModel string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode second payload: %v", err)
		}
		secondSeenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); body != "data: ok\n\n" {
		t.Fatalf("body = %q, want SSE payload", body)
	}
	if firstSeenModel != "up-1" {
		t.Fatalf("first upstream model = %q, want up-1", firstSeenModel)
	}
	if secondSeenModel != "up-2" {
		t.Fatalf("second upstream model = %q, want up-2", secondSeenModel)
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Failover-Count"); got != "1" {
		t.Fatalf("X-OCSWITCH-Failover-Count = %q, want 1", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p2" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p2", got)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if !traces[0].Failover || traces[0].FinalProvider != "p2" || traces[0].AttemptCount != 2 {
		t.Fatalf("trace = %#v", traces[0])
	}
	if got := traces[0].RequestHeaders["Authorization"]; got == "Bearer "+config.DefaultLocalAPIKey || got == "" {
		t.Fatalf("trace auth header = %q, want masked value", got)
	}
}

func TestHandleResponsesSkipsOpenCircuitOnNextRequest(t *testing.T) {
	t.Parallel()

	var firstCalls int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer first.Close()

	var secondCalls int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{
			APIKey: config.DefaultLocalAPIKey,
			Routing: routing.Config{
				Strategy: routing.DefaultStrategy,
				Params:   json.RawMessage(`{"failureThreshold":1,"baseCooldownMs":60000,"maxCooldownMs":60000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":true,"rateLimitCooldownMs":60000}`),
			},
		},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
	firstReq.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	firstReq.Header.Set("Content-Type", "application/json")
	firstReq.Header.Set("Accept", "text/event-stream")
	firstResp := httptest.NewRecorder()
	srv.handleResponses(firstResp, firstReq)

	if firstResp.Code != http.StatusOK {
		t.Fatalf("first response status = %d, want %d", firstResp.Code, http.StatusOK)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 1 {
		t.Fatalf("first provider calls after first request = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 1 {
		t.Fatalf("second provider calls after first request = %d, want 1", got)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
	secondReq.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	secondReq.Header.Set("Content-Type", "application/json")
	secondReq.Header.Set("Accept", "text/event-stream")
	secondResp := httptest.NewRecorder()
	srv.handleResponses(secondResp, secondReq)

	if secondResp.Code != http.StatusOK {
		t.Fatalf("second response status = %d, want %d", secondResp.Code, http.StatusOK)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 1 {
		t.Fatalf("first provider calls after second request = %d, want still 1", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 2 {
		t.Fatalf("second provider calls after second request = %d, want 2", got)
	}
	if got := secondResp.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("second response X-OCSWITCH-Attempt = %q, want 2", got)
	}
	if got := secondResp.Header().Get("X-OCSWITCH-Failover-Count"); got != "1" {
		t.Fatalf("second response X-OCSWITCH-Failover-Count = %q, want 1", got)
	}

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("trace count = %d, want 2", len(traces))
	}
	latest := traces[0]
	if len(latest.Attempts) != 2 {
		t.Fatalf("latest trace attempts = %d, want 2", len(latest.Attempts))
	}
	if !latest.Attempts[0].Skipped || latest.Attempts[0].Provider != "p1" || latest.Attempts[0].Error != "circuit_open" {
		t.Fatalf("latest first attempt = %#v", latest.Attempts[0])
	}
	if latest.Attempts[1].Provider != "p2" || !latest.Attempts[1].Success {
		t.Fatalf("latest second attempt = %#v", latest.Attempts[1])
	}
}

func TestHandleResponsesClearsCircuitWhenAllAliasTargetsAreOpen(t *testing.T) {
	t.Parallel()

	var firstCalls int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer first.Close()

	var secondCalls int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{
			APIKey: config.DefaultLocalAPIKey,
			Routing: routing.Config{
				Strategy: routing.DefaultStrategy,
				Params:   json.RawMessage(`{"failureThreshold":1,"baseCooldownMs":60000,"maxCooldownMs":60000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":true,"rateLimitCooldownMs":60000}`),
			},
		},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	openCandidates := []routing.Candidate{
		{ProviderID: "p1", GroupID: config.DefaultGroupID, Model: "up-1"},
		{ProviderID: "p2", GroupID: config.DefaultGroupID, Model: "up-2"},
	}
	for _, candidate := range openCandidates {
		key := routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, candidate)
		srv.store.Update(key, func(routing.ProviderState) routing.ProviderState {
			return routing.ProviderState{
				Status:              "open",
				ConsecutiveFailures: 1,
				OpenUntil:           time.Now().Add(time.Hour),
				CooldownMs:          60000,
				OpenCount:           1,
				LastFailureReason:   string(routing.FailureRateLimited),
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 1 {
		t.Fatalf("first provider calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 0 {
		t.Fatalf("second provider calls = %d, want 0", got)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	latest := traces[0]
	if len(latest.Attempts) != 3 {
		t.Fatalf("latest trace attempts = %d, want 3", len(latest.Attempts))
	}
	if !latest.Attempts[0].Skipped || latest.Attempts[0].Error != "circuit_open" || !latest.Attempts[1].Skipped || latest.Attempts[1].Error != "circuit_open" {
		t.Fatalf("latest skipped attempts = %#v", latest.Attempts[:2])
	}
	if latest.Attempts[2].Provider != "p1" || !latest.Attempts[2].Success {
		t.Fatalf("latest retry attempt = %#v", latest.Attempts[2])
	}
	p1Key := routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, openCandidates[0])
	if state := srv.store.Snapshot(p1Key); state.Status == "open" || state.ConsecutiveFailures != 0 || state.LastFailureReason != "" {
		t.Fatalf("p1 circuit state = %#v, want cleared after successful retry", state)
	}
	p2Key := routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, openCandidates[1])
	if state := srv.store.Snapshot(p2Key); state != (routing.ProviderState{}) {
		t.Fatalf("p2 circuit state = %#v, want cleared", state)
	}
}

func TestHandleResponsesClientCancelStopsFailoverAndLeavesCircuitNeutral(t *testing.T) {
	t.Parallel()

	var firstCalls int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer first.Close()

	var secondCalls int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{
			APIKey: config.DefaultLocalAPIKey,
			Routing: routing.Config{
				Strategy: routing.DefaultStrategy,
				Params:   json.RawMessage(`{"failureThreshold":1,"baseCooldownMs":60000,"maxCooldownMs":60000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":true,"rateLimitCooldownMs":60000}`),
			},
		},
		Providers: []config.Provider{{ID: "p1", BaseURL: first.URL + "/v1", BaseURLs: []string{second.URL + "/v1"}, Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-1", APIKeys: []string{"sk-2"}}}}},
		Aliases:   []config.Alias{{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}}}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if got := atomic.LoadInt32(&firstCalls); got != 0 {
		t.Fatalf("first provider calls = %d, want 0", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 0 {
		t.Fatalf("second provider calls = %d, want 0", got)
	}
	if rr.Code == http.StatusBadGateway {
		t.Fatalf("status = %d, want non-502 cancel exit", rr.Code)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if len(traces[0].Attempts) != 1 || traces[0].Attempts[0].Result != TraceResultClientCanceled {
		t.Fatalf("trace attempts = %#v, want client canceled", traces[0].Attempts)
	}
	state := srv.store.Snapshot(routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, routing.Candidate{
		ProviderID: "p1", GroupID: config.DefaultGroupID, Model: "up-1",
	}))
	if state != (routing.ProviderState{}) {
		t.Fatalf("circuit state = %#v, want neutral", state)
	}
}

func TestHandleResponsesClientCancelLeavesHalfOpenStateNeutral(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases:   []config.Alias{{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}}}},
	})

	key := routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, routing.Candidate{
		ProviderID: "p1", GroupID: config.DefaultGroupID, Model: "up-1",
	})
	srv.store.Update(key, func(state routing.ProviderState) routing.ProviderState {
		return routing.ProviderState{Status: "half-open", ConsecutiveFailures: 2, LastFailureReason: string(routing.FailureRateLimited), OpenCount: 3, CooldownMs: 60000}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	state := srv.store.Snapshot(key)
	if state.Status != "half-open" || state.HalfOpenInFlight != 0 || state.ConsecutiveFailures != 2 || state.LastFailureReason != string(routing.FailureRateLimited) || state.OpenCount != 3 {
		t.Fatalf("circuit state = %#v, want neutral half-open", state)
	}
}

func TestHandleResponsesUsesFirstProviderAPIKeyInConfiguredOrder(t *testing.T) {
	atomic.StoreUint64(&reqCounter, 0)

	seen := make([]string, 0, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Name: config.DefaultGroupName,
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-first", APIKeys: []string{"sk-second"},
			}},
		}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
		req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		rr := httptest.NewRecorder()
		srv.handleResponses(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("response %d status = %d, want %d", i+1, rr.Code, http.StatusOK)
		}
	}

	if !slices.Equal(seen, []string{"Bearer sk-first", "Bearer sk-first"}) {
		t.Fatalf("seen auth headers = %#v", seen)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if got := traces[0].Attempts[0].APIKeyIndex; got != 1 {
		t.Fatalf("latest api key index = %d, want 1", got)
	}
	if got := traces[0].Attempts[0].APIKeyMasked; got == "" || strings.Contains(got, "second") {
		t.Fatalf("latest masked api key = %q", got)
	}
}

func TestHandleResponsesRetriesNextAPIKeyForSameProvider(t *testing.T) {
	atomic.StoreUint64(&reqCounter, 0)

	seen := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if len(seen) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Name: config.DefaultGroupName,
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-first", APIKeys: []string{"sk-second"},
			}},
		}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ocswitch/gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !slices.Equal(seen, []string{"Bearer sk-first", "Bearer sk-second"}) {
		t.Fatalf("seen auth headers = %#v", seen)
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Failover-Count"); got != "0" {
		t.Fatalf("X-OCSWITCH-Failover-Count = %q, want 0", got)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces[0].Attempts) != 2 {
		t.Fatalf("trace attempts = %d, want 2", len(traces[0].Attempts))
	}
	if got := traces[0].Attempts[0].APIKeyIndex; got != 1 {
		t.Fatalf("first trace api key index = %d, want 1", got)
	}
	if got := traces[0].Attempts[0].Attempt; got != 1 {
		t.Fatalf("first trace attempt = %d, want 1", got)
	}
	if got := traces[0].Attempts[0].StatusCode; got != http.StatusTooManyRequests {
		t.Fatalf("first trace status = %d, want %d", got, http.StatusTooManyRequests)
	}
	if !traces[0].Attempts[0].Retryable {
		t.Fatalf("first trace retryable = false, want true")
	}
	if got := traces[0].Attempts[1].APIKeyIndex; got != 2 {
		t.Fatalf("second trace api key index = %d, want 2", got)
	}
	if got := traces[0].Attempts[1].Attempt; got != 2 {
		t.Fatalf("second trace attempt = %d, want 2", got)
	}
	if !traces[0].Attempts[1].Success {
		t.Fatalf("second trace success = false, want true")
	}
}

func TestHandleResponsesCapturesFinalOpenAIUsageFromCompletedEvent(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_123"}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"hi"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":20},"output_tokens":45,"output_tokens_details":{"reasoning_tokens":5}}}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].InputTokens != 100 {
		t.Fatalf("trace input tokens = %d, want 100", traces[0].InputTokens)
	}
	if traces[0].OutputTokens != 40 {
		t.Fatalf("trace output tokens = %d, want 40", traces[0].OutputTokens)
	}
	if traces[0].GeneratedOutputTokens != 45 {
		t.Fatalf("trace generated output tokens = %d, want 45", traces[0].GeneratedOutputTokens)
	}
	if traces[0].FirstTokenMs <= 0 || traces[0].Attempts[0].FirstTokenMs <= 0 {
		t.Fatalf("first token trace/attempt = %d/%d, want positive", traces[0].FirstTokenMs, traces[0].Attempts[0].FirstTokenMs)
	}
}

func TestApplyUsageToTraceProjectsGeneratedOutputTokens(t *testing.T) {
	rawOutput := int64(45)
	output := int64(40)
	reasoning := int64(5)
	trace := RequestTrace{}
	applyUsageToTrace(&trace, tokenUsage{rawOutputTokens: &rawOutput, outputTokens: &output, reasoningTokens: &reasoning})
	if trace.OutputTokens != 40 || trace.GeneratedOutputTokens != 45 {
		t.Fatalf("projected tokens = output:%d generated:%d, want 40/45", trace.OutputTokens, trace.GeneratedOutputTokens)
	}

	trace = RequestTrace{}
	applyUsageToTrace(&trace, tokenUsage{outputTokens: &output, reasoningTokens: &reasoning})
	if trace.OutputTokens != 40 || trace.GeneratedOutputTokens != 45 {
		t.Fatalf("fallback projected tokens = output:%d generated:%d, want 40/45", trace.OutputTokens, trace.GeneratedOutputTokens)
	}
}

func TestSQLiteTraceStoreRoundTripsUsageJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	rawInput := int64(120)
	rawOutput := int64(45)
	rawTotal := int64(165)
	input := int64(100)
	output := int64(40)
	reasoning := int64(5)
	cacheRead := int64(20)

	trace := RequestTrace{
		ID:                    1,
		StartedAt:             time.Now().UTC(),
		DurationMs:            123,
		FirstTokenMs:          25,
		Protocol:              config.ProtocolOpenAIResponses,
		Success:               true,
		InputTokens:           input,
		OutputTokens:          output,
		GeneratedOutputTokens: rawOutput,
		Usage: TraceUsage{
			RawInputTokens:  &rawInput,
			RawOutputTokens: &rawOutput,
			RawTotalTokens:  &rawTotal,
			InputTokens:     &input,
			OutputTokens:    &output,
			ReasoningTokens: &reasoning,
			CacheReadTokens: &cacheRead,
			Source:          "openai-responses",
			Precision:       "exact",
			Notes:           []string{"final completed event"},
		},
	}

	if err := store.Add(context.Background(), trace); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	items, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("store.List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("trace count = %d, want 1", len(items))
	}
	got := items[0]
	if got.Usage.Source != "openai-responses" {
		t.Fatalf("usage source = %q, want openai-responses", got.Usage.Source)
	}
	if got.Usage.Precision != "exact" {
		t.Fatalf("usage precision = %q, want exact", got.Usage.Precision)
	}
	if got.Usage.InputTokens == nil || *got.Usage.InputTokens != 100 {
		t.Fatalf("usage input tokens = %#v, want 100", got.Usage.InputTokens)
	}
	if got.Usage.ReasoningTokens == nil || *got.Usage.ReasoningTokens != 5 {
		t.Fatalf("usage reasoning tokens = %#v, want 5", got.Usage.ReasoningTokens)
	}
	if got.Usage.CacheReadTokens == nil || *got.Usage.CacheReadTokens != 20 {
		t.Fatalf("usage cache read tokens = %#v, want 20", got.Usage.CacheReadTokens)
	}
	if len(got.Usage.Notes) != 1 || got.Usage.Notes[0] != "final completed event" {
		t.Fatalf("usage notes = %#v, want preserved note", got.Usage.Notes)
	}
	if got.InputTokens != 100 || got.OutputTokens != 40 || got.GeneratedOutputTokens != 45 || got.FirstTokenMs != 25 {
		t.Fatalf("projected fields = input:%d output:%d generated:%d firstToken:%d, want 100/40/45/25", got.InputTokens, got.OutputTokens, got.GeneratedOutputTokens, got.FirstTokenMs)
	}
}

func TestSQLiteTraceStoreQueryReturnsRequestedPage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	baseTime := time.Now().UTC()
	for id := 1; id <= 5; id++ {
		if err := store.Add(context.Background(), RequestTrace{
			ID:        uint64(id),
			StartedAt: baseTime.Add(time.Duration(id) * time.Second),
			Protocol:  config.ProtocolOpenAIResponses,
			Alias:     "chat",
			Success:   true,
		}); err != nil {
			t.Fatalf("store.Add(%d) error = %v", id, err)
		}
	}

	result, err := store.Query(context.Background(), TraceQuery{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("store.Query() error = %v", err)
	}
	if result.Page != 2 || result.PageSize != 2 || result.Total != 5 {
		t.Fatalf("result metadata = %#v, want page=2 pageSize=2 total=5", result)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items count = %d, want 2", len(result.Items))
	}
	if result.Items[0].ID != 3 || result.Items[1].ID != 2 {
		t.Fatalf("items ids = %d,%d, want 3,2", result.Items[0].ID, result.Items[1].ID)
	}
}

func TestSQLiteTraceStoreQueryReturnsSummaryAndGetReturnsDetail(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	input := int64(12)
	trace := RequestTrace{
		ID:             7,
		StartedAt:      time.Now().UTC(),
		DurationMs:     80,
		FirstByteMs:    20,
		Protocol:       config.ProtocolOpenAIResponses,
		Alias:          "chat",
		Success:        true,
		StatusCode:     http.StatusOK,
		FinalProvider:  "p1",
		FinalModel:     "up-model",
		FinalURL:       "https://upstream.example/v1/responses",
		AttemptCount:   1,
		RequestHeaders: map[string]string{"X-Test": "present"},
		RequestParams:  map[string]any{"stream": true},
		Usage: TraceUsage{
			InputTokens: &input,
			Source:      config.ProtocolOpenAIResponses,
			Precision:   "exact",
		},
		Attempts: []TraceAttempt{{
			Attempt:         1,
			Provider:        "p1",
			Model:           "up-model",
			URL:             "https://upstream.example/v1/responses",
			StartedAt:       time.Now().UTC(),
			DurationMs:      80,
			StatusCode:      http.StatusOK,
			Success:         true,
			RequestHeaders:  map[string]string{"X-Attempt": "present"},
			RequestParams:   map[string]any{"model": "up-model"},
			ResponseHeaders: map[string]string{"Content-Type": "application/json"},
			ResponseBody:    `{}`,
		}},
	}
	if err := store.Add(context.Background(), trace); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	result, err := store.Query(context.Background(), TraceQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("store.Query() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("summary count = %d, want 1", len(result.Items))
	}
	summary := result.Items[0]
	if len(summary.Attempts) != 0 || summary.RequestHeaders != nil || summary.RequestParams != nil {
		t.Fatalf("summary contains detail fields: %#v", summary)
	}
	if summary.Usage.InputTokens == nil || *summary.Usage.InputTokens != input {
		t.Fatalf("summary usage input = %#v, want %d", summary.Usage.InputTokens, input)
	}

	detail, ok, err := store.Get(context.Background(), 7)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !ok {
		t.Fatal("store.Get() ok = false, want true")
	}
	if detail.RequestHeaders["X-Test"] != "present" || len(detail.Attempts) != 1 || detail.Attempts[0].ResponseBody != `{}` {
		t.Fatalf("detail = %#v, want full metadata", detail)
	}
}

func TestSQLiteTraceStoreQueryFiltersFailoverCounts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	baseTime := time.Now().UTC()
	for id, attempts := range map[uint64]int{1: 1, 2: 2} {
		if err := store.Add(context.Background(), RequestTrace{
			ID:           id,
			StartedAt:    baseTime.Add(time.Duration(id) * time.Second),
			Protocol:     config.ProtocolOpenAIResponses,
			Alias:        "chat",
			Success:      true,
			AttemptCount: attempts,
		}); err != nil {
			t.Fatalf("store.Add(%d) error = %v", id, err)
		}
	}

	zero, err := store.Query(context.Background(), TraceQuery{Page: 1, PageSize: 10, FailoverCounts: []int{0}})
	if err != nil {
		t.Fatalf("store.Query(zero) error = %v", err)
	}
	if len(zero.Items) != 1 || zero.Items[0].ID != 1 {
		t.Fatalf("zero failover items = %#v, want id=1", zero.Items)
	}
	one, err := store.Query(context.Background(), TraceQuery{Page: 1, PageSize: 10, FailoverCounts: []int{1}})
	if err != nil {
		t.Fatalf("store.Query(one) error = %v", err)
	}
	if len(one.Items) != 1 || one.Items[0].ID != 2 {
		t.Fatalf("one failover items = %#v, want id=2", one.Items)
	}
}

func TestSQLiteTraceStoreQueryFiltersTimeRangeAndReturnsStats(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	baseTime := time.Now().UTC().Add(-1 * time.Hour)
	traces := []RequestTrace{
		{ID: 1, StartedAt: baseTime, Protocol: config.ProtocolOpenAIResponses, Alias: "old", Success: true, StatusCode: http.StatusOK, AttemptCount: 1},
		{ID: 2, StartedAt: baseTime.Add(10 * time.Minute), Protocol: config.ProtocolOpenAIResponses, Alias: "chat", Success: true, StatusCode: http.StatusOK, Failover: true, AttemptCount: 2},
		{ID: 3, StartedAt: baseTime.Add(20 * time.Minute), Protocol: config.ProtocolOpenAIResponses, Alias: "chat", Success: false, StatusCode: http.StatusBadGateway, AttemptCount: 2},
	}
	for _, trace := range traces {
		if err := store.Add(context.Background(), trace); err != nil {
			t.Fatalf("store.Add(%d) error = %v", trace.ID, err)
		}
	}

	result, err := store.Query(context.Background(), TraceQuery{
		Page:        1,
		PageSize:    10,
		StartedFrom: baseTime.Add(5 * time.Minute),
		StartedTo:   baseTime.Add(25 * time.Minute),
	})
	if err != nil {
		t.Fatalf("store.Query() error = %v", err)
	}
	if result.Total != 2 || len(result.Items) != 2 || result.Items[0].ID != 3 || result.Items[1].ID != 2 {
		t.Fatalf("time-filtered items = total %d %#v, want ids 3,2", result.Total, result.Items)
	}
	if result.Stats.Success != 1 || result.Stats.Failover != 2 || result.Stats.Failed != 1 {
		t.Fatalf("stats = %#v, want success=1 failover=2 failed=1", result.Stats)
	}
	if len(result.AvailableAliases) != 1 || result.AvailableAliases[0] != "chat" {
		t.Fatalf("aliases = %#v, want chat only", result.AvailableAliases)
	}
	if len(result.AvailableStatusCodes) != 2 || result.AvailableStatusCodes[0] != http.StatusOK || result.AvailableStatusCodes[1] != http.StatusBadGateway {
		t.Fatalf("status codes = %#v, want 200,502", result.AvailableStatusCodes)
	}
}

func TestSQLiteTraceStoreQueryHealthTracesReturnsAggregationFieldsOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	defer store.Close()

	cacheRead := int64(9)
	startedAt := time.Now().UTC()
	trace := RequestTrace{
		ID:             99,
		StartedAt:      startedAt,
		DurationMs:     120,
		FirstByteMs:    30,
		Protocol:       config.ProtocolOpenAIResponses,
		Alias:          "chat",
		Success:        true,
		StatusCode:     http.StatusOK,
		FinalProvider:  "p2",
		FinalModel:     "m2",
		FinalURL:       "https://upstream.example/v1/responses",
		Failover:       true,
		AttemptCount:   2,
		InputTokens:    11,
		OutputTokens:   7,
		RequestHeaders: map[string]string{"X-Test": "present"},
		RequestParams:  map[string]any{"model": "m2"},
		Usage:          TraceUsage{CacheReadTokens: &cacheRead, Source: config.ProtocolOpenAIResponses},
		Attempts: []TraceAttempt{
			{Attempt: 1, Provider: "p1", Model: "m1", URL: "https://p1.example", DurationMs: 40, StatusCode: http.StatusBadGateway, Retryable: true, Result: "retryable_failure", RequestHeaders: map[string]string{"X-Attempt": "present"}, ResponseBody: `{"error":"x"}`},
			{Attempt: 2, Provider: "p2", Model: "m2", URL: "https://p2.example", DurationMs: 80, FirstByteMs: 30, StatusCode: http.StatusOK, Success: true, Result: "success", ResponseHeaders: map[string]string{"Content-Type": "application/json"}},
		},
	}
	if err := store.Add(context.Background(), trace); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	items, err := store.QueryHealthTraces(context.Background(), TraceQuery{StartedFrom: startedAt.Add(-time.Second), StartedTo: startedAt.Add(time.Second)})
	if err != nil {
		t.Fatalf("QueryHealthTraces() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}
	got := items[0]
	if got.ID != trace.ID || got.Alias != "chat" || !got.Failover || got.AttemptCount != 2 || got.FinalProvider != "p2" {
		t.Fatalf("trace summary = %#v", got)
	}
	if got.Usage.CacheReadTokens == nil || *got.Usage.CacheReadTokens != cacheRead || got.Usage.Source != "" {
		t.Fatalf("usage = %#v, want cache read only", got.Usage)
	}
	if got.RequestHeaders != nil || got.RequestParams != nil {
		t.Fatalf("detail payload decoded: headers=%#v params=%#v", got.RequestHeaders, got.RequestParams)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("attempt count = %d, want 2", len(got.Attempts))
	}
	if got.Attempts[0].Provider != "p1" || got.Attempts[0].URL != "" || got.Attempts[0].ResponseBody != "" || !got.Attempts[0].Retryable {
		t.Fatalf("first attempt = %#v, want aggregation fields only", got.Attempts[0])
	}
	if got.Attempts[1].Provider != "p2" || !got.Attempts[1].Success || got.Attempts[1].FirstByteMs != 30 || got.Attempts[1].ResponseHeaders != nil {
		t.Fatalf("second attempt = %#v, want aggregation fields only", got.Attempts[1])
	}
}

func TestSQLiteTraceStoreBackfillsMissingHealthAttempts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	ctx := context.Background()
	startedAt := time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC)
	trace := RequestTrace{
		ID:           41,
		StartedAt:    startedAt,
		Alias:        "chat",
		Success:      true,
		StatusCode:   http.StatusOK,
		AttemptCount: 2,
		Attempts: []TraceAttempt{
			{Attempt: 1, Provider: "p1", Model: "m1", StatusCode: http.StatusBadGateway, Retryable: true, Result: "retryable_failure"},
			{Attempt: 2, Provider: "p2", Model: "m2", StatusCode: http.StatusOK, Success: true, Result: "success"},
		},
	}
	if err := store.Add(ctx, trace); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.withDB(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, "DELETE FROM request_trace_attempts WHERE trace_id = ? AND attempt_index = 1", trace.ID)
		return err
	}); err != nil {
		t.Fatalf("delete one attempt error = %v", err)
	}
	if err := store.init(ctx, mustOpenSQLiteTraceTestDB(t, store.path)); err != nil {
		t.Fatalf("init() backfill error = %v", err)
	}
	items, err := store.QueryHealthTraces(ctx, TraceQuery{StartedFrom: startedAt.Add(-time.Second), StartedTo: startedAt.Add(time.Second)})
	if err != nil {
		t.Fatalf("QueryHealthTraces() error = %v", err)
	}
	if len(items) != 1 || len(items[0].Attempts) != 2 {
		t.Fatalf("health attempts = %#v", items)
	}
}

func TestSQLiteTraceStoreBackfillSkipsInvalidAttemptsJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	ctx := context.Background()
	startedAt := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	if err := store.Add(ctx, RequestTrace{
		ID:           51,
		StartedAt:    startedAt,
		Alias:        "chat",
		Success:      true,
		StatusCode:   http.StatusOK,
		AttemptCount: 1,
		Attempts:     []TraceAttempt{{Attempt: 1, Provider: "p1", Model: "m1", Success: true, StatusCode: http.StatusOK}},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.withDB(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, "UPDATE request_traces SET attempts_json = ? WHERE id = ?", `{bad-json`, uint64(51))
		return err
	}); err != nil {
		t.Fatalf("corrupt attempts_json error = %v", err)
	}
	if err := store.init(ctx, mustOpenSQLiteTraceTestDB(t, store.path)); err != nil {
		t.Fatalf("init() with invalid attempts_json error = %v", err)
	}
	items, err := store.QueryHealthTraces(ctx, TraceQuery{StartedFrom: startedAt.Add(-time.Second), StartedTo: startedAt.Add(time.Second)})
	if err != nil {
		t.Fatalf("QueryHealthTraces() error = %v", err)
	}
	if len(items) != 1 || len(items[0].Attempts) != 1 {
		t.Fatalf("existing derived attempts = %#v", items)
	}
}

func TestSQLiteTraceStoreQueryCatalogUsesAggregates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}
	ctx := context.Background()
	base := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	items := []RequestTrace{
		{ID: 1, StartedAt: base, Alias: "chat", Success: true, StatusCode: 200, AttemptCount: 1},
		{ID: 2, StartedAt: base.Add(time.Minute), Alias: "chat", Success: false, StatusCode: 502, AttemptCount: 2, Failover: true},
		{ID: 3, StartedAt: base.Add(2 * time.Minute), Alias: "code", Success: true, StatusCode: 200, AttemptCount: 0},
	}
	for _, item := range items {
		if err := store.Add(ctx, item); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	result, err := store.Query(ctx, TraceQuery{Page: 1, PageSize: 2, StartedFrom: base.Add(-time.Second), StartedTo: base.Add(3 * time.Minute)})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !reflect.DeepEqual(result.AvailableAliases, []string{"chat", "code"}) {
		t.Fatalf("AvailableAliases = %#v", result.AvailableAliases)
	}
	if !reflect.DeepEqual(result.AvailableStatusCodes, []int{200, 502}) {
		t.Fatalf("AvailableStatusCodes = %#v", result.AvailableStatusCodes)
	}
	if !reflect.DeepEqual(result.AvailableFailoverCounts, []int{0, 1}) {
		t.Fatalf("AvailableFailoverCounts = %#v", result.AvailableFailoverCounts)
	}
	if result.Stats.Success != 2 || result.Stats.Failed != 1 || result.Stats.Failover != 1 {
		t.Fatalf("Stats = %#v", result.Stats)
	}
}

func TestSQLiteTraceStoreSeedsRequestCounterFromExistingMaxID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "ocswitch.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	atomic.StoreUint64(&reqCounter, 0)
	store, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() error = %v", err)
	}

	trace := RequestTrace{
		ID:         188,
		StartedAt:  time.Now().UTC(),
		DurationMs: 10,
		Protocol:   config.ProtocolOpenAIResponses,
		Success:    true,
	}
	if err := store.Add(context.Background(), trace); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}
	_ = store.Close()

	atomic.StoreUint64(&reqCounter, 0)
	store2, err := NewSQLiteTraceStore(configPath)
	if err != nil {
		t.Fatalf("NewSQLiteTraceStore() second error = %v", err)
	}
	defer store2.Close()

	got := atomic.AddUint64(&reqCounter, 1)
	if got != 189 {
		t.Fatalf("next request id = %d, want 189", got)
	}
}

func mustOpenSQLiteTraceTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestHandleResponsesIgnoresEarlierZeroUsageAndUsesFinalCompletedUsage(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.completed",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":8}}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].InputTokens != 12 {
		t.Fatalf("trace input tokens = %d, want 12", traces[0].InputTokens)
	}
	if traces[0].OutputTokens != 8 {
		t.Fatalf("trace output tokens = %d, want 8", traces[0].OutputTokens)
	}
}

func TestHandleMessagesMergesAnthropicStreamingUsage(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: message_start",
			`data: {"type":"message_start","message":{"usage":{"input_tokens":30,"cache_read_input_tokens":4}}}`,
			"",
			"event: message_delta",
			`data: {"type":"message_delta","usage":{"output_tokens":18}}`,
			"",
			"event: message_stop",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:      "anthropic",
			BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-ant-upstream",
			}},
		}},
		Aliases: []config.Alias{{
			Alias:    "claude",
			Protocol: config.ProtocolAnthropicMessages,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "anthropic", Model: "claude-3-7-sonnet", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","stream":true}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].InputTokens != 30 {
		t.Fatalf("trace input tokens = %d, want 30", traces[0].InputTokens)
	}
	if traces[0].OutputTokens != 18 {
		t.Fatalf("trace output tokens = %d, want 18", traces[0].OutputTokens)
	}
}

func TestHandleResponsesFailsOverOnEmptySSE200(t *testing.T) {
	t.Parallel()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); body != "data: ok\n\n" {
		t.Fatalf("body = %q, want SSE payload from second upstream", body)
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
}

func TestHandleResponsesSSEIdleTimeoutEmitsProtocolError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: first\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(90 * time.Millisecond)
		_, _ = w.Write([]byte("data: second\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 30},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "data: first\n\n") {
		t.Fatalf("body = %q, want first SSE chunk", body)
	}
	if strings.Contains(body, "data: second") {
		t.Fatalf("body = %q, second chunk should not be forwarded after idle timeout", body)
	}
	if !strings.Contains(body, "upstream_stream_idle_timeout") {
		t.Fatalf("body = %q, want protocol idle timeout error", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || traces[0].Success || traces[0].Attempts[0].Result != "stream_idle_timeout" {
		t.Fatalf("trace = %#v, want failed stream_idle_timeout", traces)
	}
}

func TestHandleResponsesTerminalMarkerSuccessDespiteUpstreamHang(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.completed",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":4,"output_tokens":2}}}`,
			"",
			"",
		}, "\n")))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 30},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	started := time.Now()
	srv.handleResponses(rr, req)
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("handleResponses elapsed = %s, want early return after terminal marker", elapsed)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); !strings.Contains(body, "response.completed") || strings.Contains(body, "upstream_stream_idle_timeout") {
		t.Fatalf("body = %q, want completed event without synthetic idle error", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || !traces[0].Success || traces[0].InputTokens != 4 || traces[0].OutputTokens != 2 {
		t.Fatalf("trace = %#v, want success and usage from terminal event", traces)
	}
}

func TestHandleResponsesPrecommitMetadataOnlyFailsOver(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp-1"}}`,
			"",
		}, "\n")))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 200, StreamPrecommitBufferMs: 30},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
	if body := rr.Body.String(); strings.Contains(body, "response.created") || !strings.Contains(body, "response.output_text.delta") {
		t.Fatalf("body = %q, want only second provider content", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || !traces[0].Failover || len(traces[0].Attempts) != 2 || traces[0].Attempts[0].Result != "precommit_no_content_timeout" {
		t.Fatalf("trace = %#v, want precommit retry then failover", traces)
	}
}

func TestHandleResponsesPrecommitOutputItemCommits(t *testing.T) {
	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.output_item.added",
			`data: {"type":"response.output_item.added","item":{"id":"item-1","type":"message","status":"in_progress","content":[]}}`,
			"",
			"",
		}, "\n")))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 200, StreamPrecommitBufferMs: 30},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "1" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 1", got)
	}
	if calledSecond {
		t.Fatal("second provider should not be called after output_item commit")
	}
	if body := rr.Body.String(); !strings.Contains(body, "response.output_item.added") || strings.Contains(body, "response.output_text.delta") {
		t.Fatalf("body = %q, want first provider output_item", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || traces[0].Failover || traces[0].Success || len(traces[0].Attempts) != 1 || traces[0].Attempts[0].Result != "stream_idle_timeout" {
		t.Fatalf("trace = %#v, want output_item committed then post-commit idle failure without failover", traces)
	}
}

func TestHandleMessagesPrecommitContentBlockStartCommits(t *testing.T) {
	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: message_start",
			`data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			"",
			"event: content_block_start",
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"",
			"",
		}, "\n")))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 200, StreamPrecommitBufferMs: 30},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:    "claude",
			Protocol: config.ProtocolAnthropicMessages,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "p1", Model: "claude-1", Enabled: true}, {Provider: "p2", Model: "claude-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","stream":true}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "1" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 1", got)
	}
	if calledSecond {
		t.Fatal("second provider should not be called after content_block_start commit")
	}
	if body := rr.Body.String(); !strings.Contains(body, "content_block_start") || strings.Contains(body, "content_block_delta") {
		t.Fatalf("body = %q, want first provider content_block_start", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || traces[0].Failover || traces[0].Success || len(traces[0].Attempts) != 1 || traces[0].Attempts[0].Result != "stream_idle_timeout" {
		t.Fatalf("trace = %#v, want content_block_start committed then post-commit idle failure without failover", traces)
	}
}

func TestHandleCompletionsPrecommitFakeToolCallFailsOver(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\"}}]}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 200, StreamPrecommitBufferMs: 30},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAICompatible, APIKey: "sk-1"}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAICompatible, APIKey: "sk-2"}}},
		},
		Aliases: []config.Alias{{
			Alias:    "chat",
			Protocol: config.ProtocolOpenAICompatible,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "p1", Model: "chat-1", Enabled: true}, {Provider: "p2", Model: "chat-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"chat","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
	if body := rr.Body.String(); strings.Contains(body, "tool_calls") || !strings.Contains(body, "content") {
		t.Fatalf("body = %q, want fake tool call hidden and second provider content", body)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || !traces[0].Failover || len(traces[0].Attempts) != 2 || traces[0].Attempts[0].Result != "precommit_no_content_timeout" {
		t.Fatalf("trace = %#v, want OpenAI-compatible fake-start precommit retry", traces)
	}
}

func TestHandleResponsesPrecommitContinuousMetadataExpires(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"unknown\":true}\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 80, StreamPrecommitBufferMs: 30},
		Providers: []config.Provider{{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}, {ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases:   []config.Alias{{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}}}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
	}
	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if traces[0].Attempts[0].Result != "precommit_no_content_timeout" {
		t.Fatalf("first attempt = %#v, want absolute precommit timeout", traces[0].Attempts[0])
	}
}

func TestHandleResponsesPrecommitUnknownNonEmptyDataCommits(t *testing.T) {
	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"unknown\":true}\n\n"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 80, StreamPrecommitBufferMs: 50},
		Providers: []config.Provider{{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}, {ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases:   []config.Alias{{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}}}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if calledSecond {
		t.Fatal("second provider should not be called after unknown non-empty SSE commits")
	}
	if body := rr.Body.String(); body != "data: {\"unknown\":true}\n\n" {
		t.Fatalf("body = %q, want original unknown data frame", body)
	}
}

func TestSSEStreamStateClassifiesSplitFramesAndTerminals(t *testing.T) {
	state := newSSEStreamState(config.ProtocolOpenAIResponses)
	if signal := state.Add([]byte("event: response.output_text.delta\ndata: {\"type\":")); signal.commitWorth || signal.terminal {
		t.Fatalf("partial frame signal = %#v, want none", signal)
	}
	if signal := state.Add([]byte("\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")); !signal.commitWorth || !signal.firstTokenWorth || signal.terminal {
		t.Fatalf("completed content frame signal = %#v, want commit-worthy first token", signal)
	}
	if signal := newSSEStreamState(config.ProtocolOpenAICompatible).Add([]byte("data: [DONE]\n\n")); !signal.terminal || signal.commitWorth {
		t.Fatalf("OpenAI-compatible DONE signal = %#v, want terminal", signal)
	}
	if signal := newSSEStreamState(config.ProtocolAnthropicMessages).Add([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")); !signal.terminal || signal.commitWorth {
		t.Fatalf("Anthropic stop signal = %#v, want terminal", signal)
	}
	if signal := newSSEStreamState(config.ProtocolAnthropicMessages).Add([]byte("event: message_stop\n\n")); !signal.terminal || signal.commitWorth {
		t.Fatalf("Anthropic empty stop signal = %#v, want terminal", signal)
	}
}

func TestSSEStreamStateSeparatesCommitWorthFromFirstToken(t *testing.T) {
	created := newSSEStreamState(config.ProtocolOpenAIResponses).Add([]byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"))
	if created.commitWorth || created.firstTokenWorth || created.terminal {
		t.Fatalf("created signal = %#v, want no signal", created)
	}
	emptyItem := newSSEStreamState(config.ProtocolOpenAIResponses).Add([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\"}}\n\n"))
	if !emptyItem.commitWorth || emptyItem.firstTokenWorth || emptyItem.terminal {
		t.Fatalf("empty item signal = %#v, want commit without first token", emptyItem)
	}
	responseReasoning := newSSEStreamState(config.ProtocolOpenAIResponses).Add([]byte("event: response.reasoning.delta\ndata: {\"type\":\"response.reasoning.delta\",\"delta\":\"think\"}\n\n"))
	if !responseReasoning.commitWorth || !responseReasoning.firstTokenWorth || responseReasoning.terminal {
		t.Fatalf("responses reasoning signal = %#v, want first token", responseReasoning)
	}
	responseEncryptedReasoning := newSSEStreamState(config.ProtocolOpenAIResponses).Add([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"reasoning\",\"encrypted_content\":\"opaque\",\"summary\":[]}}\n\n"))
	if !responseEncryptedReasoning.commitWorth || !responseEncryptedReasoning.firstTokenWorth || responseEncryptedReasoning.terminal {
		t.Fatalf("responses encrypted reasoning signal = %#v, want first token", responseEncryptedReasoning)
	}
	compatibleRole := newSSEStreamState(config.ProtocolOpenAICompatible).Add([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
	if compatibleRole.commitWorth || compatibleRole.firstTokenWorth || compatibleRole.terminal {
		t.Fatalf("compatible role signal = %#v, want metadata only", compatibleRole)
	}
	compatibleReasoning := newSSEStreamState(config.ProtocolOpenAICompatible).Add([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n"))
	if !compatibleReasoning.commitWorth || !compatibleReasoning.firstTokenWorth || compatibleReasoning.terminal {
		t.Fatalf("compatible reasoning signal = %#v, want first token", compatibleReasoning)
	}
	anthropicStart := newSSEStreamState(config.ProtocolAnthropicMessages).Add([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\"}}\n\n"))
	if !anthropicStart.commitWorth || anthropicStart.firstTokenWorth || anthropicStart.terminal {
		t.Fatalf("anthropic block start signal = %#v, want commit without first token", anthropicStart)
	}
	anthropicDelta := newSSEStreamState(config.ProtocolAnthropicMessages).Add([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\n"))
	if !anthropicDelta.commitWorth || !anthropicDelta.firstTokenWorth || anthropicDelta.terminal {
		t.Fatalf("anthropic delta signal = %#v, want first token", anthropicDelta)
	}
}

func TestNextSSEFrameUsesEarliestSeparator(t *testing.T) {
	buf := bytes.NewBufferString("data: one\n\ndata: two\r\n\r\n")
	frame, ok := nextSSEFrame(buf)
	if !ok || frame != "data: one" {
		t.Fatalf("first frame = %q ok=%v, want earliest LF frame", frame, ok)
	}
	frame, ok = nextSSEFrame(buf)
	if !ok || frame != "data: two" {
		t.Fatalf("second frame = %q ok=%v, want CRLF frame", frame, ok)
	}
}

func TestSSEStreamErrorEventShapes(t *testing.T) {
	message := "timeout message"
	responses := string(sseStreamErrorEvent(config.ProtocolOpenAIResponses, "upstream_stream_idle_timeout", message))
	if !strings.HasPrefix(responses, "event: error\n") || !strings.Contains(responses, `"type":"error"`) || !strings.Contains(responses, `"code":"upstream_stream_idle_timeout"`) {
		t.Fatalf("OpenAI Responses error event = %q", responses)
	}
	compatible := string(sseStreamErrorEvent(config.ProtocolOpenAICompatible, "upstream_stream_idle_timeout", message))
	if strings.Contains(compatible, "event: error") || !strings.Contains(compatible, `"error"`) || !strings.Contains(compatible, `"code":"upstream_stream_idle_timeout"`) {
		t.Fatalf("OpenAI-compatible error event = %q", compatible)
	}
	anthropic := string(sseStreamErrorEvent(config.ProtocolAnthropicMessages, "upstream_stream_idle_timeout", message))
	if !strings.HasPrefix(anthropic, "event: error\n") || !strings.Contains(anthropic, `"type":"api_error"`) || strings.Contains(anthropic, "upstream_stream_idle_timeout") {
		t.Fatalf("Anthropic error event = %q", anthropic)
	}
}

func TestSSEPrecommitBufferCapForcesCommit(t *testing.T) {
	firstChunk := []byte(strings.Repeat(": keepalive\n\n", (ssePrecommitBufferCapBytes/12)+2))
	srv := newLegacyTestServer(&config.Config{Server: config.Server{APIKey: config.DefaultLocalAPIKey}})
	result, err := srv.runSSEPrecommitBuffer(ssePrecommitInput{
		body:            bytes.NewReader(nil),
		firstChunk:      firstChunk,
		protocol:        config.ProtocolOpenAIResponses,
		idleTimeout:     50 * time.Millisecond,
		precommitWindow: 50 * time.Millisecond,
		classifier:      newSSEStreamState(config.ProtocolOpenAIResponses),
	})
	if err != nil {
		t.Fatalf("runSSEPrecommitBuffer() error = %v", err)
	}
	if result.terminal || result.buffered.Len() < ssePrecommitBufferCapBytes {
		t.Fatalf("result = terminal:%v len:%d, want cap-forced commit", result.terminal, result.buffered.Len())
	}
}

func TestHandleResponsesMarksBrokenStreamAsFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: first\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("Hijack() error = %v", err)
		}
		_ = conn.Close()
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].Success {
		t.Fatalf("trace = %#v, want failed trace", traces[0])
	}
	if traces[0].Error == "" {
		t.Fatalf("trace = %#v, want error", traces[0])
	}
	if len(traces[0].Attempts) != 1 {
		t.Fatalf("attempts = %#v, want 1", traces[0].Attempts)
	}
	if traces[0].Attempts[0].Success {
		t.Fatalf("attempt = %#v, want failed attempt", traces[0].Attempts[0])
	}
	if traces[0].Attempts[0].Result != "stream_error" {
		t.Fatalf("attempt result = %q, want stream_error", traces[0].Attempts[0].Result)
	}
	if traces[0].Usage.Precision != "unavailable" {
		t.Fatalf("usage precision = %q, want unavailable", traces[0].Usage.Precision)
	}
	if traces[0].Usage.Source != config.ProtocolOpenAIResponses {
		t.Fatalf("usage source = %q, want %q", traces[0].Usage.Source, config.ProtocolOpenAIResponses)
	}
	if len(traces[0].Usage.Notes) == 0 {
		t.Fatalf("usage notes = %#v, want stream failure note", traces[0].Usage.Notes)
	}
}

func TestHandleResponsesMarksDownstreamDisconnectAsClientCanceled(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: ok\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{
			APIKey: config.DefaultLocalAPIKey,
			Routing: routing.Config{
				Strategy: routing.DefaultStrategy,
				Params:   json.RawMessage(`{"failureThreshold":1,"baseCooldownMs":60000,"maxCooldownMs":60000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":true,"rateLimitCooldownMs":60000}`),
			},
		},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}},
		Aliases:   []config.Alias{{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}}}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	w := &writeErrorResponseWriter{header: http.Header{}, err: errors.New("write: broken pipe")}

	srv.handleResponses(w, req)

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 || len(traces[0].Attempts) != 1 {
		t.Fatalf("traces = %#v, want one attempt", traces)
	}
	if got := traces[0].Attempts[0].Result; got != TraceResultDownstreamCanceled {
		t.Fatalf("attempt result = %q, want downstream canceled", got)
	}
	state := srv.store.Snapshot(routing.StateKeyForCandidate(routing.DefaultStrategy, config.ProtocolOpenAIResponses, routing.Candidate{
		ProviderID: "p1", GroupID: config.DefaultGroupID, Model: "up-1",
	}))
	if state != (routing.ProviderState{}) {
		t.Fatalf("circuit state = %#v, want neutral", state)
	}
}

func TestHandleMessagesMarksBrokenStreamUsageAsPartial(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: message_start",
			`data: {"type":"message_start","message":{"usage":{"input_tokens":30,"cache_read_input_tokens":4,"cache_creation":{"ephemeral_5m_input_tokens":8,"ephemeral_1h_input_tokens":2}}}}`,
			"",
		}, "\n")))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("Hijack() error = %v", err)
		}
		_ = conn.Close()
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:      "anthropic",
			BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-ant-upstream",
			}},
		}},
		Aliases: []config.Alias{{
			Alias:    "claude",
			Protocol: config.ProtocolAnthropicMessages,
			Enabled:  true,
			Targets:  []config.Target{{Provider: "anthropic", Model: "claude-3-7-sonnet", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","stream":true}`))
	req.Header.Set("X-Api-Key", config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleMessages(rr, req)

	traces, err := srv.traces.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("traces.List() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].Success {
		t.Fatalf("trace = %#v, want failed trace", traces[0])
	}
	if traces[0].Usage.Precision != "partial" {
		t.Fatalf("usage precision = %q, want partial", traces[0].Usage.Precision)
	}
	if traces[0].InputTokens != 30 {
		t.Fatalf("trace input tokens = %d, want 30", traces[0].InputTokens)
	}
	if traces[0].Usage.CacheReadTokens == nil || *traces[0].Usage.CacheReadTokens != 4 {
		t.Fatalf("cache read tokens = %#v, want 4", traces[0].Usage.CacheReadTokens)
	}
	if traces[0].Usage.CacheWriteTokens == nil || *traces[0].Usage.CacheWriteTokens != 8 {
		t.Fatalf("cache write tokens = %#v, want 8", traces[0].Usage.CacheWriteTokens)
	}
	if traces[0].Usage.CacheWrite1HTokens == nil || *traces[0].Usage.CacheWrite1HTokens != 2 {
		t.Fatalf("cache write 1h tokens = %#v, want 2", traces[0].Usage.CacheWrite1HTokens)
	}
	if len(traces[0].Usage.Notes) == 0 {
		t.Fatalf("usage notes = %#v, want stream failure note", traces[0].Usage.Notes)
	}
}

func TestNewUsesConfiguredTimeouts(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{Server: config.Server{
		ConnectTimeoutMs:        12000,
		ResponseHeaderTimeoutMs: 21000,
		FirstByteTimeoutMs:      22000,
		RequestReadTimeoutMs:    33000,
		StreamIdleTimeoutMs:     70000,
	}})

	runtime := srv.currentRuntime()
	transport, ok := runtime.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", runtime.client.Transport)
	}
	if transport.ResponseHeaderTimeout != 21*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 21s", transport.ResponseHeaderTimeout)
	}
}

func TestHandleResponsesDoesNotFailOverOn400(t *testing.T) {
	t.Parallel()

	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if calledSecond {
		t.Fatal("second upstream should not be called for 400 response")
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p1" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p1", got)
	}
	if body := rr.Body.String(); body != `{"error":{"message":"bad request"}}` {
		t.Fatalf("body = %q", body)
	}
}

func TestHandleResponsesFailsOverOnDefaultConfigured4xx(t *testing.T) {
	t.Parallel()

	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			t.Parallel()

			calledSecond := false
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`{"error":{"message":"configured failover"}}`))
			}))
			defer first.Close()
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calledSecond = true
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: ok\n\n"))
			}))
			defer second.Close()

			srv := newLegacyTestServer(&config.Config{
				Server: config.Server{APIKey: config.DefaultLocalAPIKey},
				Providers: []config.Provider{
					{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
					{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
				},
				Aliases: []config.Alias{{
					Alias:   "gpt-5.4",
					Enabled: true,
					Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
				}},
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
			req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			srv.handleResponses(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
			}
			if !calledSecond {
				t.Fatal("second upstream was not called")
			}
			if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "2" {
				t.Fatalf("X-OCSWITCH-Attempt = %q, want 2", got)
			}
		})
	}
}

func TestHandleResponsesRespectsCustomFailoverStatusCodes(t *testing.T) {
	t.Parallel()

	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, FailoverStatusCodes: []int{http.StatusBadRequest}},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !calledSecond {
		t.Fatal("second upstream was not called")
	}
}

func TestHandleResponsesCanDisableConfigured4xxFailover(t *testing.T) {
	t.Parallel()

	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, FailoverStatusCodes: []int{}},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
	if calledSecond {
		t.Fatal("second upstream should not be called")
	}
}

func TestHandleResponsesAlwaysFailsOverOn5xx(t *testing.T) {
	t.Parallel()

	calledSecond := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"server error"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledSecond = true
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey, FailoverStatusCodes: []int{}},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !calledSecond {
		t.Fatal("second upstream was not called")
	}
}

func TestHandleResponsesReturnsLastRetryableFailure(t *testing.T) {
	t.Parallel()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream unavailable"}}`))
	}))
	defer second.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: second.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadGateway)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want empty from last failure", got)
	}
	if body := rr.Body.String(); body != `{"error":{"message":"upstream unavailable"}}` {
		t.Fatalf("body = %q", body)
	}
}

func TestCopyForwardHeadersDropsDynamicConnectionHeaders(t *testing.T) {
	t.Parallel()

	src := http.Header{}
	src.Set("Connection", "X-Trace-Id, Keep-Alive")
	src.Set("X-Trace-Id", "abc")
	src.Set("Keep-Alive", "timeout=5")
	src.Set("OpenAI-Beta", "assistants=v2")
	src.Set("X-Forwarded-For", "1.2.3.4")
	dst := http.Header{}

	copyForwardHeaders(dst, src)

	if got := dst.Get("X-Trace-Id"); got != "" {
		t.Fatalf("X-Trace-Id = %q, want empty", got)
	}
	if got := dst.Get("Keep-Alive"); got != "" {
		t.Fatalf("Keep-Alive = %q, want empty", got)
	}
	if got := dst.Get("X-Forwarded-For"); got != "" {
		t.Fatalf("X-Forwarded-For = %q, want empty", got)
	}
	if got := dst.Get("OpenAI-Beta"); got != "assistants=v2" {
		t.Fatalf("OpenAI-Beta = %q, want assistants=v2", got)
	}
}

func TestReadChunkWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("returns data", func(t *testing.T) {
		t.Parallel()
		buf := make([]byte, 8)
		n, err := readChunkWithTimeout(bytes.NewBufferString("abc"), buf, 50*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 3 || string(buf[:n]) != "abc" {
			t.Fatalf("read = %d %q, want 3 abc", n, string(buf[:n]))
		}
	})

	t.Run("times out", func(t *testing.T) {
		t.Parallel()
		buf := make([]byte, 8)
		n, err := readChunkWithTimeout(blockingReader{}, buf, 20*time.Millisecond)
		if !errors.Is(err, errStreamIdleTimeout) {
			t.Fatalf("err = %v, want errStreamIdleTimeout", err)
		}
		if n != 0 {
			t.Fatalf("n = %d, want 0", n)
		}
	})
}
func TestHandleResponsesSkipsDisabledProviders(t *testing.T) {
	t.Parallel()

	calledDisabled := false
	disabled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledDisabled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer disabled.Close()

	var seenModel string
	enabled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer enabled.Close()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: disabled.URL + "/v1", Disabled: true, Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: enabled.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}, {Provider: "p2", Model: "up-2", Enabled: true}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if calledDisabled {
		t.Fatal("disabled provider should be skipped before any upstream call")
	}
	if seenModel != "up-2" {
		t.Fatalf("enabled upstream model = %q, want up-2", seenModel)
	}
	if got := rr.Header().Get("X-OCSWITCH-Attempt"); got != "1" {
		t.Fatalf("X-OCSWITCH-Attempt = %q, want 1", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Failover-Count"); got != "0" {
		t.Fatalf("X-OCSWITCH-Failover-Count = %q, want 0", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p2" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p2", got)
	}
	if body := rr.Body.String(); body != "data: ok\n\n" {
		t.Fatalf("body = %q, want SSE payload", body)
	}
}

func TestHandleModelsSkipsAliasesWithoutAvailableTargets(t *testing.T) {
	t.Parallel()

	srv := newLegacyTestServer(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true, Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
		Aliases: []config.Alias{
			{Alias: "ok", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}}},
			{Alias: "no-route", Enabled: true, Targets: []config.Target{{Provider: "p2", Model: "up-2", Enabled: true}}},
			{Alias: "alias-disabled", Enabled: false, Targets: []config.Target{{Provider: "p1", Model: "up-3", Enabled: true}}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	rr := httptest.NewRecorder()

	srv.handleModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); !strings.Contains(body, `"id":"ok"`) {
		t.Fatalf("models body = %q, want alias ok", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `"id":"no-route"`) {
		t.Fatalf("models body = %q, disabled-provider alias should be hidden", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `"id":"alias-disabled"`) {
		t.Fatalf("models body = %q, disabled alias should be hidden", body)
	}
}

func TestRequestReadErrorTimeout(t *testing.T) {
	t.Parallel()

	status, message := requestReadError(timeoutErr{})
	if status != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusRequestTimeout)
	}
	if message != "request body read timeout" {
		t.Fatalf("message = %q", message)
	}
}

func TestReadFirstChunk(t *testing.T) {
	t.Parallel()

	t.Run("returns data", func(t *testing.T) {
		t.Parallel()
		buf, err := readFirstChunk(bytes.NewBufferString("abc"), 50*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(buf) != "abc" {
			t.Fatalf("buf = %q, want abc", string(buf))
		}
	})

	t.Run("returns eof", func(t *testing.T) {
		t.Parallel()
		buf, err := readFirstChunk(bytes.NewReader(nil), 50*time.Millisecond)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("err = %v, want EOF", err)
		}
		if buf != nil {
			t.Fatalf("buf = %v, want nil", buf)
		}
	})

	t.Run("returns data with eof", func(t *testing.T) {
		t.Parallel()
		buf, err := readFirstChunk(dataEOFReader{}, 50*time.Millisecond)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("err = %v, want EOF", err)
		}
		if string(buf) != "abc" {
			t.Fatalf("buf = %q, want abc", string(buf))
		}
	})

	t.Run("times out", func(t *testing.T) {
		t.Parallel()
		buf, err := readFirstChunk(blockingReader{}, 20*time.Millisecond)
		if !errors.Is(err, errFirstByteTimeout) {
			t.Fatalf("err = %v, want errFirstByteTimeout", err)
		}
		if buf != nil {
			t.Fatalf("buf = %v, want nil", buf)
		}
	})
}

func TestHandleRequest_AutoAliasFallback(t *testing.T) {
	t.Parallel()

	var seenAuth string
	var seenModel string
	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		hitCount.Add(1)
		seenAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-auto","output":[]}`))
	}))
	defer upstream.Close()

	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("manual-only provider must not receive auto-alias fallback traffic")
		w.WriteHeader(http.StatusOK)
	}))
	defer wrong.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:           config.Server{APIKey: config.DefaultLocalAPIKey},
		AutoAliasEnabled: true,
		Providers: []config.Provider{
			{ID: "p-wrong", BaseURL: wrong.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-wrong", Models: []string{"other"}}}},
			{ID: "p-auto", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-auto", Models: []string{"auto-model"}}}},
		},
		Aliases: []config.Alias{{
			Alias:         "auto-model",
			Protocol:      config.ProtocolOpenAIResponses,
			Enabled:       true,
			AutoGenerated: true,
			Targets: []config.Target{{
				Provider:      "p-auto",
				Model:         "remote-auto",
				Enabled:       true,
				AutoGenerated: true,
			}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"auto-model","stream":false}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if hitCount.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hitCount.Load())
	}
	if seenAuth != "Bearer sk-auto" {
		t.Fatalf("authorization = %q, want Bearer sk-auto", seenAuth)
	}
	if seenModel != "remote-auto" {
		t.Fatalf("model = %q, want remote-auto", seenModel)
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p-auto" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p-auto", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Remote-Model"); got != "remote-auto" {
		t.Fatalf("X-OCSWITCH-Remote-Model = %q, want remote-auto", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Alias"); got != "auto-model" {
		t.Fatalf("X-OCSWITCH-Alias = %q, want auto-model", got)
	}
}

func TestHandleRequest_DirectProviderFallbackForbidden(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatal("direct provider/group catalog routing is forbidden")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:           config.Server{APIKey: config.DefaultLocalAPIKey},
		AutoAliasEnabled: true,
		Providers: []config.Provider{
			{ID: "p-direct", BaseURL: upstream.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-direct", Models: []string{"direct-model"}}}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"direct-model","stream":false}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
	if hitCount.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hitCount.Load())
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "direct-model" not found`)
	assertLocalTrace(t, srv, "direct-model", http.StatusNotFound, "alias_missing", `alias "direct-model" not found`)
}

func TestHandleRequest_ManualAliasPriority(t *testing.T) {
	t.Parallel()

	var seenAuth string
	var seenModel string
	var hitCount atomic.Int32
	manualUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		hitCount.Add(1)
		seenAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-manual","output":[]}`))
	}))
	defer manualUp.Close()

	autoUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("auto alias target must not win over same-name manual alias")
		w.WriteHeader(http.StatusOK)
	}))
	defer autoUp.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:           config.Server{APIKey: config.DefaultLocalAPIKey},
		AutoAliasEnabled: true,
		Providers: []config.Provider{
			{ID: "p-manual", BaseURL: manualUp.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-manual", Models: []string{"shared-model"}}}},
			{ID: "p-auto", BaseURL: autoUp.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-auto", Models: []string{"shared-model"}}}},
		},
		Aliases: []config.Alias{
			{
				Alias:         "shared-model",
				Protocol:      config.ProtocolOpenAIResponses,
				Enabled:       true,
				AutoGenerated: false,
				Targets:       []config.Target{{Provider: "p-manual", Model: "manual-upstream", Enabled: true}},
			},
			{
				Alias:         "shared-model",
				Protocol:      config.ProtocolOpenAIResponses,
				Enabled:       true,
				AutoGenerated: true,
				Targets: []config.Target{{
					Provider:      "p-auto",
					Model:         "auto-upstream",
					Enabled:       true,
					AutoGenerated: true,
				}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"shared-model","stream":false}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if hitCount.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hitCount.Load())
	}
	if seenAuth != "Bearer sk-manual" {
		t.Fatalf("authorization = %q, want Bearer sk-manual", seenAuth)
	}
	if seenModel != "manual-upstream" {
		t.Fatalf("model = %q, want manual-upstream", seenModel)
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p-manual" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p-manual", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Remote-Model"); got != "manual-upstream" {
		t.Fatalf("X-OCSWITCH-Remote-Model = %q, want manual-upstream", got)
	}
}

func TestHandleRequest_AutoAliasDisabled(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	autoOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer sk-auto-only" {
			t.Errorf("authorization = %q, want Bearer sk-auto-only", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream payload: %v", err)
		}
		if got := payload["model"]; got != "should-not-use" {
			t.Errorf("upstream model = %v, want should-not-use", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_auto","object":"response","output":[]}`))
	}))
	defer autoOnly.Close()

	directUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatal("direct provider fallback is forbidden")
		w.WriteHeader(http.StatusOK)
	}))
	defer directUp.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:           config.Server{APIKey: config.DefaultLocalAPIKey},
		AutoAliasEnabled: false,
		Providers: []config.Provider{
			{ID: "p-auto-only", BaseURL: autoOnly.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-auto-only", Models: []string{"other"}}}},
			{ID: "p-direct", BaseURL: directUp.URL + "/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-direct", Models: []string{"disabled-auto-model"}}}},
		},
		Aliases: []config.Alias{{
			Alias:         "disabled-auto-model",
			Protocol:      config.ProtocolOpenAIResponses,
			Enabled:       true,
			AutoGenerated: true,
			Targets: []config.Target{{
				Provider:      "p-auto-only",
				Model:         "should-not-use",
				Enabled:       true,
				AutoGenerated: true,
			}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"disabled-auto-model","stream":false}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if hitCount.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hitCount.Load())
	}
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "p-auto-only" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want p-auto-only", got)
	}
}

func TestHandleRequest_NoDirectFallbackFromGroupCatalog(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatal("group model catalog must not generate direct candidates")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	srv := newLegacyTestServer(&config.Config{
		Server:           config.Server{APIKey: config.DefaultLocalAPIKey},
		AutoAliasEnabled: true,
		Providers: []config.Provider{{
			ID:      "p-openai",
			BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID:       "default",
				Protocol: config.ProtocolOpenAIResponses,
				APIKey:   "sk-openai",
				Models:   []string{"proto-model"},
			}},
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"proto-model","stream":false}`))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
	if hitCount.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hitCount.Load())
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "proto-model" not found`)
}

type blockingReader struct{}
type dataEOFReader struct{}
type timeoutErr struct{}
type writeErrorResponseWriter struct {
	header http.Header
	status int
	err    error
}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func (w *writeErrorResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *writeErrorResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *writeErrorResponseWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func (blockingReader) Read(p []byte) (int, error) {
	time.Sleep(200 * time.Millisecond)
	return 0, nil
}

func (dataEOFReader) Read(p []byte) (int, error) {
	copy(p, []byte("abc"))
	return 3, io.EOF
}

func assertOpenAIError(t *testing.T, body []byte, wantCode, wantType, wantMessage string) {
	t.Helper()
	var payload openAIErrorEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if payload.Error.Code != wantCode {
		t.Fatalf("error.code = %q, want %q", payload.Error.Code, wantCode)
	}
	if payload.Error.Type != wantType {
		t.Fatalf("error.type = %q, want %q", payload.Error.Type, wantType)
	}
	if payload.Error.Message != wantMessage {
		t.Fatalf("error.message = %q, want %q", payload.Error.Message, wantMessage)
	}
}

func assertAnthropicError(t *testing.T, body []byte, wantType, wantMessage string) {
	t.Helper()
	var payload anthropicErrorEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal anthropic error body: %v", err)
	}
	if payload.Type != "error" {
		t.Fatalf("type = %q, want error", payload.Type)
	}
	if payload.Error.Type != wantType {
		t.Fatalf("error.type = %q, want %q", payload.Error.Type, wantType)
	}
	if payload.Error.Message != wantMessage {
		t.Fatalf("error.message = %q, want %q", payload.Error.Message, wantMessage)
	}
}
