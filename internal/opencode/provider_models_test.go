package opencode

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestFetchProviderModels(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		var auth string
		var custom string
		var method string
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method = r.Method
			path = r.URL.Path
			auth = r.Header.Get("Authorization")
			custom = r.Header.Get("X-Test")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4.1"},{"id":"gpt-4o"}]}`))
		}))
		defer srv.Close()

		models, err := FetchProviderModels("openai-responses", srv.URL+"/v1", "sk-test", map[string]string{"X-Test": "1"})
		if err != nil {
			t.Fatalf("FetchProviderModels() error = %v", err)
		}
		if auth != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", auth)
		}
		if method != http.MethodGet {
			t.Fatalf("Method = %q", method)
		}
		if path != "/v1/models" {
			t.Fatalf("Path = %q", path)
		}
		if custom != "1" {
			t.Fatalf("X-Test = %q", custom)
		}
		want := []string{"gpt-4.1", "gpt-4o"}
		if !reflect.DeepEqual(models, want) {
			t.Fatalf("models = %#v, want %#v", models, want)
		}
	})

	t.Run("status error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"error":"bad key"}`)
		}))
		defer srv.Close()

		_, err := FetchProviderModels("openai-responses", srv.URL+"/v1", "", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "bad key") {
			t.Fatalf("error = %q", err.Error())
		}
	})

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		defer srv.Close()

		models, err := FetchProviderModels("openai-responses", srv.URL+"/v1", "", nil)
		if err != nil {
			t.Fatalf("FetchProviderModels() error = %v", err)
		}
		if len(models) != 0 {
			t.Fatalf("models = %#v, want empty", models)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{bad`)
		}))
		defer srv.Close()

		_, err := FetchProviderModels("openai-responses", srv.URL+"/v1", "", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("anthropic headers", func(t *testing.T) {
		t.Parallel()
		var auth string
		var apiKey string
		var version string
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			auth = r.Header.Get("Authorization")
			apiKey = r.Header.Get("X-Api-Key")
			version = r.Header.Get("Anthropic-Version")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-7-sonnet"}]}`))
		}))
		defer srv.Close()

		models, err := FetchProviderModels(config.ProtocolAnthropicMessages, srv.URL+"/v1", "sk-ant", nil)
		if err != nil {
			t.Fatalf("FetchProviderModels() error = %v", err)
		}
		if path != "/v1/models" {
			t.Fatalf("Path = %q, want /v1/models", path)
		}
		if auth != "" {
			t.Fatalf("Authorization = %q, want empty", auth)
		}
		if apiKey != "sk-ant" {
			t.Fatalf("X-Api-Key = %q, want sk-ant", apiKey)
		}
		if version != "2023-06-01" {
			t.Fatalf("Anthropic-Version = %q, want 2023-06-01", version)
		}
		want := []string{"claude-3-7-sonnet"}
		if !reflect.DeepEqual(models, want) {
			t.Fatalf("models = %#v, want %#v", models, want)
		}
	})
}

func TestFetchProviderModelsWithFallback(t *testing.T) {
	t.Parallel()

	t.Run("uses later base url after error", func(t *testing.T) {
		t.Parallel()
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"bad gateway"}`)
		}))
		defer first.Close()
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		}))
		defer second.Close()

		models, probe, err := FetchProviderModelsWithFallback("openai-responses", []string{first.URL + "/v1", second.URL + "/v1"}, "", nil)
		if err != nil {
			t.Fatalf("FetchProviderModelsWithFallback() error = %v", err)
		}
		if probe == nil || probe.BaseURL != second.URL+"/v1" || !probe.Reachable {
			t.Fatalf("probe = %#v", probe)
		}
		want := []string{"gpt-4.1"}
		if !reflect.DeepEqual(models, want) {
			t.Fatalf("models = %#v, want %#v", models, want)
		}
	})

	t.Run("treats reachable empty result as success", func(t *testing.T) {
		t.Parallel()
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		defer first.Close()
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		}))
		defer second.Close()

		models, probe, err := FetchProviderModelsWithFallback("openai-responses", []string{first.URL + "/v1", second.URL + "/v1"}, "", nil)
		if err != nil {
			t.Fatalf("FetchProviderModelsWithFallback() error = %v", err)
		}
		if probe == nil || probe.BaseURL != second.URL+"/v1" || !probe.Reachable {
			t.Fatalf("probe = %#v", probe)
		}
		want := []string{"gpt-4.1"}
		if !reflect.DeepEqual(models, want) {
			t.Fatalf("models = %#v, want %#v", models, want)
		}
	})
}
