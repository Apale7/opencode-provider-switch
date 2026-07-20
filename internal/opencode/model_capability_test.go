package opencode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelCapabilityProbeUsesUpstreamModels(t *testing.T) {
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
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4o"},{"id":"gpt-4.1"}]}`)
	}))
	defer srv.Close()

	probe := ProbeModelCapability(context.Background(), ProviderModelProbeTarget{
		ProviderID: "openai",
		Protocol:   "openai-responses",
		BaseURLs:   []string{srv.URL + "/v1"},
		APIKeys:    []string{"sk-test"},
		Headers:    map[string]string{"X-Test": "1"},
	}, "gpt-4o")

	if method != http.MethodGet {
		t.Fatalf("method = %q, want %q", method, http.MethodGet)
	}
	if path != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", path)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want Bearer sk-test", auth)
	}
	if custom != "1" {
		t.Fatalf("X-Test = %q, want 1", custom)
	}
	assertModelCapability(t, probe, "gpt-4o", "openai", "openai-responses", "upstream", false)
	if probe.ContextLimit <= 0 {
		t.Fatalf("ContextLimit = %d, want positive", probe.ContextLimit)
	}
	if probe.OutputLimit <= 0 {
		t.Fatalf("OutputLimit = %d, want positive", probe.OutputLimit)
	}
	if !containsString(probe.InputModalities, "text") {
		t.Fatalf("InputModalities = %#v, want text", probe.InputModalities)
	}
	if !containsString(probe.OutputModalities, "text") {
		t.Fatalf("OutputModalities = %#v, want text", probe.OutputModalities)
	}
}

func TestModelCapabilityProbeFallsBackToKnownDB(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	probe := ProbeModelCapability(context.Background(), ProviderModelProbeTarget{
		ProviderID: "deepseek",
		Protocol:   "openai-responses",
		BaseURLs:   []string{srv.URL + "/v1"},
		APIKeys:    []string{"sk-bad"},
	}, "deepseek-reasoner")

	assertModelCapability(t, probe, "deepseek-reasoner", "deepseek", "openai-responses", "known_db", false)
	if probe.ContextLimit <= 0 {
		t.Fatalf("ContextLimit = %d, want known_db positive", probe.ContextLimit)
	}
	if !containsString(probe.InputModalities, "text") {
		t.Fatalf("InputModalities = %#v, want text", probe.InputModalities)
	}
}

func TestModelCapabilityProbeFallsBackToDefault(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"bad gateway"}`)
	}))
	defer srv.Close()

	probe := ProbeModelCapability(context.Background(), ProviderModelProbeTarget{
		ProviderID: "unknown-provider",
		Protocol:   "openai-responses",
		BaseURLs:   []string{srv.URL + "/v1"},
		APIKeys:    []string{"sk-bad"},
	}, "unknown-model-for-capability-test")

	assertModelCapability(t, probe, "unknown-model-for-capability-test", "unknown-provider", "openai-responses", "fallback", true)
	assertDefaultModelCapability(t, probe)
}

func TestModelCapabilityProbeUnknownProtocolReturnsDefaultAndError(t *testing.T) {
	t.Parallel()

	probe := ProbeModelCapability(context.Background(), ProviderModelProbeTarget{
		ProviderID: "custom",
		Protocol:   "unknown-protocol",
		BaseURLs:   []string{"http://127.0.0.1:1/v1"},
		APIKeys:    []string{"sk-test"},
	}, "gpt-5")

	assertModelCapability(t, probe, "gpt-5", "custom", "unknown-protocol", "fallback", true)
	assertDefaultModelCapability(t, probe)
	if !strings.Contains(strings.ToLower(probe.ProbeError), "protocol") {
		t.Fatalf("ProbeError = %q, want protocol error", probe.ProbeError)
	}
}

func TestModelCapabilityProbeEmptyModelIDReturnsError(t *testing.T) {
	t.Parallel()

	probe := ProbeModelCapability(context.Background(), ProviderModelProbeTarget{
		ProviderID: "openai",
		Protocol:   "openai-responses",
		BaseURLs:   []string{"http://127.0.0.1:1/v1"},
		APIKeys:    []string{"sk-test"},
	}, "")

	if strings.TrimSpace(probe.ProbeError) == "" {
		t.Fatalf("ProbeError = %q, want non-empty error", probe.ProbeError)
	}
	if !strings.Contains(strings.ToLower(probe.ProbeError), "model") {
		t.Fatalf("ProbeError = %q, want model error", probe.ProbeError)
	}
}

func TestModelConfigFromCapabilityProbeMapsCapabilityFields(t *testing.T) {
	t.Parallel()

	probe, ok := KnownModelCapability("gpt-5")
	if !ok {
		t.Fatal("KnownModelCapability(gpt-5) not found")
	}
	probe.ModelID = "gpt-5"

	config := ModelConfigFromCapabilityProbe(probe)
	if config["reasoning"] != true {
		t.Fatalf("reasoning = %#v, want true", config["reasoning"])
	}
	if config["toolCall"] != true {
		t.Fatalf("toolCall = %#v, want true", config["toolCall"])
	}
	if config["attachment"] != true {
		t.Fatalf("attachment = %#v, want true", config["attachment"])
	}
}

func assertModelCapability(t *testing.T, probe ModelCapabilityProbe, modelID, providerID, protocol, source string, wantError bool) {
	t.Helper()
	if probe.ModelID != modelID {
		t.Fatalf("ModelID = %q, want %q", probe.ModelID, modelID)
	}
	if probe.ProviderID != providerID {
		t.Fatalf("ProviderID = %q, want %q", probe.ProviderID, providerID)
	}
	if probe.Protocol != protocol {
		t.Fatalf("Protocol = %q, want %q", probe.Protocol, protocol)
	}
	if probe.ProbeSource != source {
		t.Fatalf("ProbeSource = %q, want %q", probe.ProbeSource, source)
	}
	if wantError && strings.TrimSpace(probe.ProbeError) == "" {
		t.Fatalf("ProbeError = %q, want non-empty error", probe.ProbeError)
	}
	if !wantError && strings.TrimSpace(probe.ProbeError) != "" {
		t.Fatalf("ProbeError = %q, want empty", probe.ProbeError)
	}
}

func assertDefaultModelCapability(t *testing.T, probe ModelCapabilityProbe) {
	t.Helper()
	if probe.ContextLimit != 128000 {
		t.Fatalf("ContextLimit = %d, want 128000", probe.ContextLimit)
	}
	if probe.OutputLimit != 4096 {
		t.Fatalf("OutputLimit = %d, want 4096", probe.OutputLimit)
	}
	if len(probe.InputModalities) != 1 || probe.InputModalities[0] != "text" {
		t.Fatalf("InputModalities = %#v, want [text]", probe.InputModalities)
	}
	if len(probe.OutputModalities) != 1 || probe.OutputModalities[0] != "text" {
		t.Fatalf("OutputModalities = %#v, want [text]", probe.OutputModalities)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
