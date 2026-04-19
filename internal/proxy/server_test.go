package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	traces := srv.traces.List(10)
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

	traces := srv.traces.List(10)
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
