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

	"github.com/anomalyco/opencode-provider-switch/internal/config"
)

func TestHandleResponsesWritesOpenAIErrorForMissingAlias(t *testing.T) {
	t.Parallel()

	srv := New(&config.Config{
		Server: config.Server{APIKey: "ops-local"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"missing","stream":true}`))
	req.Header.Set("Authorization", "Bearer ops-local")
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
		Server: config.Server{APIKey: "ops-local"},
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

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ops/gpt-5.4","stream":true}`))
	req.Header.Set("Authorization", "Bearer ops-local")
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
	if got := rr.Header().Get("X-OPS-Attempt"); got != "2" {
		t.Fatalf("X-OPS-Attempt = %q, want 2", got)
	}
	if got := rr.Header().Get("X-OPS-Failover-Count"); got != "1" {
		t.Fatalf("X-OPS-Failover-Count = %q, want 1", got)
	}
	if got := rr.Header().Get("X-OPS-Provider"); got != "p2" {
		t.Fatalf("X-OPS-Provider = %q, want p2", got)
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
		Server: config.Server{APIKey: "ops-local"},
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
	req.Header.Set("Authorization", "Bearer ops-local")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.handleResponses(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if calledSecond {
		t.Fatal("second upstream should not be called for 400 response")
	}
	if got := rr.Header().Get("X-OPS-Provider"); got != "p1" {
		t.Fatalf("X-OPS-Provider = %q, want p1", got)
	}
	if body := rr.Body.String(); body != `{"error":{"message":"bad request"}}` {
		t.Fatalf("body = %q", body)
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
