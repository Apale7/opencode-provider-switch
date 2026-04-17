package opencode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalConfigDirIgnoresOpencodeConfigDir(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/custom-opencode")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-home")

	got := GlobalConfigDir()
	want := filepath.Join("/tmp/xdg-home", "opencode")
	if got != want {
		t.Fatalf("GlobalConfigDir() = %q, want %q", got, want)
	}
}

func TestResolveGlobalConfigPathPrecedence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(root, "ignored"))

	dir := filepath.Join(root, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path, existed := ResolveGlobalConfigPath()
	if existed {
		t.Fatalf("expected existed=false before files are created")
	}
	wantDefault := filepath.Join(dir, "opencode.jsonc")
	if path != wantDefault {
		t.Fatalf("default path = %q, want %q", path, wantDefault)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	path, existed = ResolveGlobalConfigPath()
	if !existed || path != filepath.Join(dir, "config.json") {
		t.Fatalf("expected config.json, got path=%q existed=%v", path, existed)
	}

	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}
	path, existed = ResolveGlobalConfigPath()
	if !existed || path != filepath.Join(dir, "opencode.json") {
		t.Fatalf("expected opencode.json, got path=%q existed=%v", path, existed)
	}

	if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write opencode.jsonc: %v", err)
	}
	path, existed = ResolveGlobalConfigPath()
	if !existed || path != filepath.Join(dir, "opencode.jsonc") {
		t.Fatalf("expected opencode.jsonc, got path=%q existed=%v", path, existed)
	}
}

func TestValidateOpsProvider(t *testing.T) {
	raw := Raw{}
	aliases := []string{"gpt-5.4", "gpt-5.4-mini"}
	baseURL := "http://127.0.0.1:9982/v1"
	apiKey := "ops-local"
	EnsureOpsProvider(raw, baseURL, apiKey, aliases)

	providerRaw, _ := raw["provider"].(map[string]any)
	opsRaw, _ := providerRaw["ops"].(map[string]any)
	opts, _ := opsRaw["options"].(map[string]any)
	if got, ok := opts["setCacheKey"].(bool); !ok || !got {
		t.Fatalf("provider.ops.options.setCacheKey = %#v, want true", opts["setCacheKey"])
	}

	if err := ValidateOpsProvider(raw, baseURL, apiKey, aliases); err != nil {
		t.Fatalf("ValidateOpsProvider() unexpected error: %v", err)
	}
}

func TestRenderSaveDataReplacesExistingProviderOpsOnly(t *testing.T) {
	raw := Raw{
		"$schema": "https://opencode.ai/config.json",
		"model":   "ops/gpt-5.4",
		"provider": map[string]any{
			"anthropic": map[string]any{"npm": "@ai-sdk/anthropic"},
			"ops": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OPS",
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ops-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
			"openai": map[string]any{"npm": "@ai-sdk/openai"},
		},
		"small_model": "ops/gpt-5.4-mini",
	}
	original := []byte("{\n  \"model\": \"ops/old\",\n  \"provider\": {\n    \"anthropic\": {\"npm\": \"@ai-sdk/anthropic\"},\n    \"ops\": {\n      \"npm\": \"old\",\n      \"options\": {\"baseURL\": \"http://old/v1\"},\n      \"models\": {\"old\": {\"name\": \"old\"}}\n    },\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  },\n  \"small_model\": \"ops/old-mini\"\n}\n")

	got, err := patchProviderOpsDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderOpsDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"model"`, `"provider"`, `"small_model"`})
	assertStringOrder(t, string(got), []string{`"anthropic"`, `"ops"`, `"openai"`})
	if strings.Contains(string(got), `"npm": "old"`) {
		t.Fatalf("old provider.ops content still present: %s", string(got))
	}
	var saved Raw
	if err := json.Unmarshal(got, &saved); err != nil {
		t.Fatalf("unmarshal patched json: %v", err)
	}
	if err := ValidateOpsProvider(saved, "http://127.0.0.1:9982/v1", "ops-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOpsProvider(saved) error: %v", err)
	}
}

func TestRenderSaveDataInsertsOpsWithoutReorderingProviderKeys(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"anthropic": map[string]any{"npm": "@ai-sdk/anthropic"},
			"ops": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OPS",
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ops-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
			"openai": map[string]any{"npm": "@ai-sdk/openai"},
		},
	}
	original := []byte("{\n  \"provider\": {\n    \"anthropic\": {\"npm\": \"@ai-sdk/anthropic\"},\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  },\n  \"model\": \"ops/gpt-5.4\"\n}\n")

	got, err := patchProviderOpsDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderOpsDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"anthropic"`, `"openai"`, `"ops"`})
}

func TestRenderSaveDataInsertsProviderAtTopLevelEnd(t *testing.T) {
	raw := Raw{
		"model": "ops/gpt-5.4",
		"provider": map[string]any{
			"ops": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OPS",
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ops-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
		},
		"small_model": "ops/gpt-5.4-mini",
	}
	original := []byte("{\n  \"model\": \"ops/gpt-5.4\",\n  \"small_model\": \"ops/gpt-5.4-mini\"\n}\n")

	got, err := patchProviderOpsDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderOpsDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"model"`, `"small_model"`, `"provider"`})
}

func TestRenderSaveDataAcceptsJSONCAndProducesValidJSON(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"ops": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OPS",
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ops-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
		},
	}
	original := []byte("{\n  // comment\n  \"provider\": {\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"},\n  },\n}\n")

	got, err := patchProviderOpsDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderOpsDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	if bytes.Contains(got, []byte("// comment")) {
		t.Fatalf("expected normalized json output without comments, got %s", string(got))
	}
}

func TestRenderSaveDataRejectsInvalidJSONC(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"ops": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "OPS",
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ops-local",
					"setCacheKey": true,
				},
			},
		},
	}

	if _, err := patchProviderOpsDocument([]byte(`{"provider": {`), raw); err == nil {
		t.Fatal("expected invalid json/jsonc error")
	}
}

func TestRenderSaveDataRejectsNonObjectProvider(t *testing.T) {
	raw := Raw{}
	EnsureOpsProvider(raw, "http://127.0.0.1:9982/v1", "ops-local", []string{"gpt-5.4"})

	if _, err := patchProviderOpsDocument([]byte(`{"provider":"bad"}`), raw); err == nil {
		t.Fatal("expected provider object error")
	}
}

func TestRenderSaveDataRejectsNonObjectTopLevel(t *testing.T) {
	raw := Raw{}
	EnsureOpsProvider(raw, "http://127.0.0.1:9982/v1", "ops-local", []string{"gpt-5.4"})

	if _, err := patchProviderOpsDocument([]byte(`[]`), raw); err == nil {
		t.Fatal("expected top-level object error")
	}
	if _, err := patchProviderOpsDocument([]byte("{} trailing"), raw); err == nil {
		t.Fatal("expected single top-level object error")
	}
}

func TestRenderSaveDataWritesValidJSONToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.WriteFile(path, []byte("{\n  \"model\": \"ops/gpt-5.4\",\n  \"provider\": {\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  }\n}\n"), 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}
	raw := Raw{}
	EnsureOpsProvider(raw, "http://127.0.0.1:9982/v1", "ops-local", []string{"gpt-5.4"})

	if err := Save(path, raw); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	assertValidJSON(t, got)
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load(saved) error: %v", err)
	}
	if err := ValidateOpsProvider(loaded, "http://127.0.0.1:9982/v1", "ops-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOpsProvider(loaded) error: %v", err)
	}
}

func assertValidJSON(t *testing.T, data []byte) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatalf("invalid json output: %s", string(data))
	}
}

func assertStringOrder(t *testing.T, body string, parts []string) {
	t.Helper()
	last := -1
	for _, part := range parts {
		idx := strings.Index(body, part)
		if idx < 0 {
			t.Fatalf("missing %q in output: %s", part, body)
		}
		if idx < last {
			t.Fatalf("order mismatch for %q in output: %s", part, body)
		}
		last = idx
	}
}
