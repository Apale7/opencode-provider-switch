package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestHandleResponsesWritesOpenAIErrorForMissingAlias(t *testing.T) {
	t.Parallel()

	srv := New(&config.Config{
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
}

func TestHandleMessagesWritesAnthropicErrorForMissingAlias(t *testing.T) {
	t.Parallel()

	srv := New(&config.Config{
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:       "anthropic",
			Protocol: config.ProtocolAnthropicMessages,
			BaseURL:  upstream.URL + "/v1",
			APIKey:   "sk-ant-upstream",
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", Protocol: config.ProtocolAnthropicMessages, BaseURL: first.URL + "/v1", APIKey: "sk-1"},
			{ID: "p2", Protocol: config.ProtocolAnthropicMessages, BaseURL: second.URL + "/v1", APIKey: "sk-2"},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1", APIKey: "sk-1"},
			{ID: "p2", BaseURL: second.URL + "/v1", APIKey: "sk-2"},
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

func TestHandleResponsesCapturesFinalOpenAIUsageFromCompletedEvent(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_123"}}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":20},"output_tokens":45,"output_tokens_details":{"reasoning_tokens":5}}}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	srv := New(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1"}},
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
		ID:           1,
		StartedAt:    time.Now().UTC(),
		DurationMs:   123,
		Protocol:     config.ProtocolOpenAIResponses,
		Success:      true,
		InputTokens:  input,
		OutputTokens: output,
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
	if got.InputTokens != 100 || got.OutputTokens != 40 {
		t.Fatalf("projected tokens = %d/%d, want 100/40", got.InputTokens, got.OutputTokens)
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

	srv := New(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1"}},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:       "anthropic",
			Protocol: config.ProtocolAnthropicMessages,
			BaseURL:  upstream.URL + "/v1",
			APIKey:   "sk-ant-upstream",
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1"},
			{ID: "p2", BaseURL: second.URL + "/v1"},
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

func TestHandleResponsesSSEBypassesIdleTimeout(t *testing.T) {
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

	srv := New(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey, StreamIdleTimeoutMs: 30},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1"}},
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
	if body := rr.Body.String(); body != "data: first\n\ndata: second\n\n" {
		t.Fatalf("body = %q, want both SSE chunks", body)
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

	srv := New(&config.Config{
		Server:    config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{ID: "p1", BaseURL: upstream.URL + "/v1"}},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:       "anthropic",
			Protocol: config.ProtocolAnthropicMessages,
			BaseURL:  upstream.URL + "/v1",
			APIKey:   "sk-ant-upstream",
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

	srv := New(&config.Config{Server: config.Server{
		ConnectTimeoutMs:        12000,
		ResponseHeaderTimeoutMs: 21000,
		FirstByteTimeoutMs:      22000,
		RequestReadTimeoutMs:    33000,
		StreamIdleTimeoutMs:     70000,
	}})

	transport, ok := srv.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", srv.client.Transport)
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1"},
			{ID: "p2", BaseURL: second.URL + "/v1"},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: first.URL + "/v1"},
			{ID: "p2", BaseURL: second.URL + "/v1"},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: disabled.URL + "/v1", Disabled: true},
			{ID: "p2", BaseURL: enabled.URL + "/v1"},
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

	srv := New(&config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true},
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

type blockingReader struct{}
type dataEOFReader struct{}
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

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
