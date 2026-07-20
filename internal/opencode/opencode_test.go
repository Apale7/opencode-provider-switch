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

func TestValidateOcswitchProvider(t *testing.T) {
	raw := Raw{}
	aliases := []string{"gpt-5.4", "gpt-5.4-mini"}
	baseURL := "http://127.0.0.1:9982/v1"
	apiKey := "ocswitch-local"
	EnsureOcswitchProvider("openai-responses", raw, baseURL, apiKey, aliases)

	providerRaw, _ := raw["provider"].(map[string]any)
	providerEntry, _ := providerRaw[ProviderKey].(map[string]any)
	opts, _ := providerEntry["options"].(map[string]any)
	if got, ok := opts["setCacheKey"].(bool); !ok || !got {
		t.Fatalf("provider.%s.options.setCacheKey = %#v, want true", ProviderKey, opts["setCacheKey"])
	}

	if err := ValidateOcswitchProvider("openai-responses", raw, baseURL, apiKey, aliases); err != nil {
		t.Fatalf("ValidateOcswitchProvider() unexpected error: %v", err)
	}
}

func TestValidateOcswitchProviderAnthropic(t *testing.T) {
	raw := Raw{}
	aliases := []string{"claude-3-7-sonnet"}
	baseURL := "http://127.0.0.1:9982/v1"
	apiKey := "ocswitch-local"
	EnsureOcswitchProvider("anthropic-messages", raw, baseURL, apiKey, aliases)

	providerRaw, _ := raw["provider"].(map[string]any)
	providerEntry, _ := providerRaw[AnthropicProviderKey].(map[string]any)
	if providerEntry == nil {
		t.Fatalf("missing provider.%s", AnthropicProviderKey)
	}
	opts, _ := providerEntry["options"].(map[string]any)
	headers, _ := opts["headers"].(map[string]any)
	if headers["anthropic-version"] != "2023-06-01" {
		t.Fatalf("provider.%s.options.headers = %#v", AnthropicProviderKey, headers)
	}

	if err := ValidateOcswitchProvider("anthropic-messages", raw, baseURL, apiKey, aliases); err != nil {
		t.Fatalf("ValidateOcswitchProvider() unexpected error: %v", err)
	}
}

func TestValidateOcswitchProviderOpenAICompatible(t *testing.T) {
	raw := Raw{}
	aliases := []string{"gpt-oss"}
	baseURL := "http://127.0.0.1:9982/v1"
	apiKey := "ocswitch-local"
	EnsureOcswitchProvider("openai-compatible", raw, baseURL, apiKey, aliases)

	providerRaw, _ := raw["provider"].(map[string]any)
	providerEntry, _ := providerRaw[CompatProviderKey].(map[string]any)
	if providerEntry == nil {
		t.Fatalf("missing provider.%s", CompatProviderKey)
	}
	if got, _ := providerEntry["npm"].(string); got != "@ai-sdk/openai-compatible" {
		t.Fatalf("provider.%s.npm = %q", CompatProviderKey, got)
	}
	if err := ValidateOcswitchProvider("openai-compatible", raw, baseURL, apiKey, aliases); err != nil {
		t.Fatalf("ValidateOcswitchProvider() unexpected error: %v", err)
	}
}

func TestEnsureOcswitchProviderPreservesExistingModelMetadata(t *testing.T) {
	raw := Raw{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{
					"gpt-5.4": map[string]any{
						"name": "custom-display-name",
						"limit": map[string]any{
							"context": float64(272000),
							"output":  float64(128000),
						},
						"cost": map[string]any{
							"input":  float64(1.75),
							"output": float64(14),
						},
						"variants": map[string]any{
							"high": map[string]any{"reasoningEffort": "high"},
						},
						"options": map[string]any{"serviceTier": "priority"},
					},
				},
			},
		},
	}

	changed := EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})
	if changed {
		t.Fatal("EnsureOcswitchProvider() reported change for unchanged same-name alias")
	}

	providerRaw := raw["provider"].(map[string]any)
	providerEntry := providerRaw[ProviderKey].(map[string]any)
	models := providerEntry["models"].(map[string]any)
	model := models["gpt-5.4"].(map[string]any)
	if got := model["name"]; got != "custom-display-name" {
		t.Fatalf("model name = %#v, want custom-display-name preserved", got)
	}
	if _, ok := model["limit"].(map[string]any); !ok {
		t.Fatalf("model limit metadata missing: %#v", model["limit"])
	}
	if _, ok := model["cost"].(map[string]any); !ok {
		t.Fatalf("model cost metadata missing: %#v", model["cost"])
	}
	if _, ok := model["variants"].(map[string]any); !ok {
		t.Fatalf("model variants metadata missing: %#v", model["variants"])
	}
	if _, ok := model["options"].(map[string]any); !ok {
		t.Fatalf("model options metadata missing: %#v", model["options"])
	}
}

func TestEnsureOcswitchProviderAddsModelCapabilityMetadata(t *testing.T) {
	raw := Raw{}
	capabilities := map[string]ModelCapabilityProbe{
		"gpt-capable": {
			ModelID:           "gpt-capable",
			Protocol:          "openai-responses",
			ContextLimit:      200000,
			OutputLimit:       8192,
			InputModalities:   []string{"text", "image"},
			OutputModalities:  []string{"text"},
			SupportsReasoning: true,
			SupportsTools:     true,
			SupportsImages:    true,
			ProbeSource:       ModelCapabilityProbeSourceUpstream,
		},
	}

	changed := EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-capable"}, capabilities)
	if !changed {
		t.Fatal("EnsureOcswitchProvider() reported unchanged for new provider with capabilities")
	}

	model := mustOcswitchModel(t, raw, ProviderKey, "gpt-capable")
	if got := model["name"]; got != "gpt-capable" {
		t.Fatalf("model name = %#v, want gpt-capable", got)
	}
	assertModelLimit(t, model, 200000, 8192)
	assertStringValues(t, model["inputModalities"], []string{"text", "image"})
	assertStringValues(t, model["outputModalities"], []string{"text"})
	assertBoolValue(t, model, "reasoning", true)
	assertBoolValue(t, model, "toolCall", true)
	assertBoolValue(t, model, "attachment", true)
}

func TestEnsureOcswitchProviderMergesModelCapabilitiesWithoutOverwritingUserValues(t *testing.T) {
	raw := Raw{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{
					"gpt-existing": map[string]any{
						"name": "custom-display-name",
						"limit": map[string]any{
							"context": int64(100000),
						},
						"reasoning": false,
					},
				},
			},
		},
	}
	capabilities := map[string]ModelCapabilityProbe{
		"gpt-existing": {
			ModelID:           "gpt-existing",
			Protocol:          "openai-responses",
			ContextLimit:      200000,
			OutputLimit:       16384,
			InputModalities:   []string{"text", "image"},
			OutputModalities:  []string{"text"},
			SupportsReasoning: true,
			SupportsTools:     true,
			SupportsImages:    true,
			ProbeSource:       ModelCapabilityProbeSourceUpstream,
		},
	}

	changed := EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-existing"}, capabilities)
	if !changed {
		t.Fatal("EnsureOcswitchProvider() reported unchanged when capabilities filled missing fields")
	}

	model := mustOcswitchModel(t, raw, ProviderKey, "gpt-existing")
	if got := model["name"]; got != "custom-display-name" {
		t.Fatalf("model name = %#v, want custom-display-name preserved", got)
	}
	assertModelLimit(t, model, 100000, 16384)
	assertStringValues(t, model["inputModalities"], []string{"text", "image"})
	assertStringValues(t, model["outputModalities"], []string{"text"})
	assertBoolValue(t, model, "reasoning", false)
	assertBoolValue(t, model, "toolCall", true)
	assertBoolValue(t, model, "attachment", true)
}

func TestEnsureOcswitchProviderUsesSafeDefaultsForMissingCapabilityFields(t *testing.T) {
	raw := Raw{}
	capabilities := map[string]ModelCapabilityProbe{
		"gpt-fallback": {
			ModelID:     "gpt-fallback",
			Protocol:    "openai-responses",
			ProbeSource: ModelCapabilityProbeSourceFallback,
			ProbeError:  "probe failed",
		},
	}

	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-fallback"}, capabilities)

	model := mustOcswitchModel(t, raw, ProviderKey, "gpt-fallback")
	assertModelLimit(t, model, SafeDefaultContextLimit, SafeDefaultOutputLimit)
	assertStringValues(t, model["inputModalities"], []string{"text"})
	assertStringValues(t, model["outputModalities"], []string{"text"})
	assertBoolValue(t, model, "reasoning", false)
	assertBoolValue(t, model, "toolCall", false)
	assertBoolValue(t, model, "attachment", false)
}

func TestEnsureOcswitchProviderNilCapabilitiesKeepsMinimalModelConfig(t *testing.T) {
	raw := Raw{}
	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-minimal"})

	model := mustOcswitchModel(t, raw, ProviderKey, "gpt-minimal")
	if len(model) != 1 || model["name"] != "gpt-minimal" {
		t.Fatalf("model = %#v, want only name without capability metadata", model)
	}
}

func TestEnsureOcswitchProviderDoesNotPanicOnComparableMetadata(t *testing.T) {
	raw := Raw{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{
					"gpt-5.4": map[string]any{
						"name": "custom-display-name",
						"tags": []any{"reasoning", "priority"},
						"variants": []any{
							map[string]any{"name": "high", "effort": "high"},
						},
					},
				},
			},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("EnsureOcswitchProvider() panicked with slice metadata: %v", r)
		}
	}()

	changed := EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})
	if changed {
		t.Fatal("EnsureOcswitchProvider() reported change for unchanged alias metadata with slices")
	}
}

func TestValidateOcswitchProviderAllowsCustomModelMetadata(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{
					"gpt-5.4": map[string]any{
						"name":    "custom-display-name",
						"options": map[string]any{"serviceTier": "priority"},
					},
				},
			},
		},
	}

	if err := ValidateOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOcswitchProvider() unexpected error for custom metadata: %v", err)
	}
}

func TestPatchProviderDocumentReplacesExistingProviderOnly(t *testing.T) {
	raw := Raw{
		"$schema": "https://opencode.ai/config.json",
		"model":   "ocswitch/gpt-5.4",
		"provider": map[string]any{
			"anthropic": map[string]any{"npm": "@ai-sdk/anthropic"},
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
			"openai": map[string]any{"npm": "@ai-sdk/openai"},
		},
		"small_model": "ocswitch/gpt-5.4-mini",
	}
	original := []byte("{\n  \"model\": \"ocswitch/old\",\n  \"provider\": {\n    \"anthropic\": {\"npm\": \"@ai-sdk/anthropic\"},\n    \"ocswitch\": {\n      \"npm\": \"old\",\n      \"options\": {\"baseURL\": \"http://old/v1\"},\n      \"models\": {\"old\": {\"name\": \"old\"}}\n    },\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  },\n  \"small_model\": \"ocswitch/old-mini\"\n}\n")

	got, err := patchProviderDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"model"`, `"provider"`, `"small_model"`})
	assertStringOrder(t, string(got), []string{`"anthropic"`, `"ocswitch"`, `"openai"`})
	if strings.Contains(string(got), `"npm": "old"`) {
		t.Fatalf("old provider.ocswitch content still present: %s", string(got))
	}
	var saved Raw
	if err := json.Unmarshal(got, &saved); err != nil {
		t.Fatalf("unmarshal patched json: %v", err)
	}
	if err := ValidateOcswitchProvider("openai-responses", saved, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOcswitchProvider(saved) error: %v", err)
	}
}

func TestPatchProviderDocumentInsertsOcswitchWithoutReorderingProviderKeys(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"anthropic": map[string]any{"npm": "@ai-sdk/anthropic"},
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
			"openai": map[string]any{"npm": "@ai-sdk/openai"},
		},
	}
	original := []byte("{\n  \"provider\": {\n    \"anthropic\": {\"npm\": \"@ai-sdk/anthropic\"},\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  },\n  \"model\": \"ocswitch/gpt-5.4\"\n}\n")

	got, err := patchProviderDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"anthropic"`, `"openai"`, `"ocswitch"`})
}

func TestPatchProviderDocumentInsertsProviderAtTopLevelEnd(t *testing.T) {
	raw := Raw{
		"model": "ocswitch/gpt-5.4",
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
		},
		"small_model": "ocswitch/gpt-5.4-mini",
	}
	original := []byte("{\n  \"model\": \"ocswitch/gpt-5.4\",\n  \"small_model\": \"ocswitch/gpt-5.4-mini\"\n}\n")

	got, err := patchProviderDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"model"`, `"small_model"`, `"provider"`})
}

func TestPatchProviderDocumentAcceptsJSONCAndProducesValidJSON(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
				"models": map[string]any{"gpt-5.4": map[string]any{"name": "gpt-5.4"}},
			},
		},
	}
	original := []byte("{\n  // comment\n  \"provider\": {\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"},\n  },\n}\n")

	got, err := patchProviderDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	if bytes.Contains(got, []byte("// comment")) {
		t.Fatalf("expected normalized json output without comments, got %s", string(got))
	}
}

func TestImportCustomProvidersAllowsEmptyAPIKey(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"openai-empty": map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": "Empty Key",
				"options": map[string]any{
					"baseURL": "https://example.com/v1",
				},
			},
		},
	}

	imports := ImportCustomProviders(raw)
	if len(imports) != 1 {
		t.Fatalf("len(imports) = %d, want 1", len(imports))
	}
	if imports[0].ID != "openai-empty" {
		t.Fatalf("id = %q, want openai-empty", imports[0].ID)
	}
	if imports[0].APIKey != "" {
		t.Fatalf("api key = %q, want empty", imports[0].APIKey)
	}
}

func TestImportCustomProvidersSortsModels(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"p1": map[string]any{
				"npm": "@ai-sdk/openai",
				"options": map[string]any{
					"baseURL": "https://example.com/v1",
				},
				"models": map[string]any{
					"z-model": map[string]any{},
					"a-model": map[string]any{},
				},
			},
		},
	}

	imports := ImportCustomProviders(raw)
	if len(imports) != 1 {
		t.Fatalf("len(imports) = %d, want 1", len(imports))
	}
	if got := strings.Join(imports[0].Models, ","); got != "a-model,z-model" {
		t.Fatalf("Models = %q", got)
	}
}

func TestImportCustomProvidersAnthropic(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"anthropic-custom": map[string]any{
				"npm":  "@ai-sdk/anthropic",
				"name": "Anthropic Custom",
				"options": map[string]any{
					"baseURL": "https://api.anthropic.com/v1",
					"apiKey":  "sk-ant",
					"headers": map[string]any{"anthropic-version": "2023-06-01"},
				},
				"models": map[string]any{
					"claude-3-7-sonnet": map[string]any{},
				},
			},
		},
	}

	imports := ImportCustomProviders(raw)
	if len(imports) != 1 {
		t.Fatalf("len(imports) = %d, want 1", len(imports))
	}
	if imports[0].Protocol != "anthropic-messages" {
		t.Fatalf("protocol = %q, want anthropic-messages", imports[0].Protocol)
	}
	if imports[0].Headers["anthropic-version"] != "2023-06-01" {
		t.Fatalf("headers = %#v", imports[0].Headers)
	}
}

func TestImportCustomProvidersOpenAICompatible(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			"compat-custom": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Compat Custom",
				"options": map[string]any{
					"baseURL": "https://compat.example.com/v1",
					"apiKey":  "sk-compat",
				},
				"models": map[string]any{
					"gpt-oss": map[string]any{},
				},
			},
		},
	}

	imports := ImportCustomProviders(raw)
	if len(imports) != 1 {
		t.Fatalf("len(imports) = %d, want 1", len(imports))
	}
	if imports[0].Protocol != "openai-compatible" {
		t.Fatalf("protocol = %q, want openai-compatible", imports[0].Protocol)
	}
	if imports[0].ID != "compat-custom" || imports[0].APIKey != "sk-compat" {
		t.Fatalf("import = %#v", imports[0])
	}
}

func TestPatchProviderDocumentAddsAnthropicSyncedProvider(t *testing.T) {
	raw := Raw{}
	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})
	EnsureOcswitchProvider("anthropic-messages", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"claude-3-7-sonnet"})
	original := []byte("{\n  \"provider\": {\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  }\n}\n")

	got, err := patchProviderDocument(original, raw)
	if err != nil {
		t.Fatalf("patchProviderDocument() error: %v", err)
	}
	assertValidJSON(t, got)
	assertStringOrder(t, string(got), []string{`"openai"`, `"ocswitch"`, `"ocswitch-anthropic"`})

	var saved Raw
	if err := json.Unmarshal(got, &saved); err != nil {
		t.Fatalf("unmarshal patched json: %v", err)
	}
	if err := ValidateOcswitchProvider("openai-responses", saved, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOcswitchProvider(openai) error: %v", err)
	}
	if err := ValidateOcswitchProvider("anthropic-messages", saved, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"claude-3-7-sonnet"}); err != nil {
		t.Fatalf("ValidateOcswitchProvider(anthropic) error: %v", err)
	}
}
func TestPatchProviderDocumentRejectsInvalidJSONC(t *testing.T) {
	raw := Raw{
		"provider": map[string]any{
			ProviderKey: map[string]any{
				"npm":  "@ai-sdk/openai",
				"name": ProviderName,
				"options": map[string]any{
					"baseURL":     "http://127.0.0.1:9982/v1",
					"apiKey":      "ocswitch-local",
					"setCacheKey": true,
				},
			},
		},
	}

	if _, err := patchProviderDocument([]byte(`{"provider": {`), raw); err == nil {
		t.Fatal("expected invalid json/jsonc error")
	}
}

func TestPatchProviderDocumentRejectsNonObjectProvider(t *testing.T) {
	raw := Raw{}
	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})

	if _, err := patchProviderDocument([]byte(`{"provider":"bad"}`), raw); err == nil {
		t.Fatal("expected provider object error")
	}
}

func TestPatchProviderDocumentRejectsNonObjectTopLevel(t *testing.T) {
	raw := Raw{}
	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})

	if _, err := patchProviderDocument([]byte(`[]`), raw); err == nil {
		t.Fatal("expected top-level object error")
	}
	if _, err := patchProviderDocument([]byte(`{} trailing`), raw); err == nil {
		t.Fatal("expected single top-level object error")
	}
}

func TestSaveWritesValidJSONToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.WriteFile(path, []byte("{\n  \"model\": \"ocswitch/gpt-5.4\",\n  \"provider\": {\n    \"openai\": {\"npm\": \"@ai-sdk/openai\"}\n  }\n}\n"), 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}
	raw := Raw{}
	EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})

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
	if err := ValidateOcswitchProvider("openai-responses", loaded, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"}); err != nil {
		t.Fatalf("ValidateOcswitchProvider(loaded) error: %v", err)
	}
}

func TestSavePreservesExistingModelMetadataForSameAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	seed := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"provider\": {\n    \"ocswitch\": {\n      \"npm\": \"@ai-sdk/openai\",\n      \"name\": \"OpenCode Provider Switch CLI\",\n      \"options\": {\n        \"baseURL\": \"http://127.0.0.1:9982/v1\",\n        \"apiKey\": \"ocswitch-local\",\n        \"setCacheKey\": true\n      },\n      \"models\": {\n        \"gpt-5.4\": {\n          \"name\": \"custom-display-name\",\n          \"limit\": {\n            \"context\": 272000,\n            \"output\": 128000\n          },\n          \"options\": {\n            \"serviceTier\": \"priority\"\n          }\n        }\n      }\n    }\n  }\n}\n")
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}

	raw, err := Load(path)
	if err != nil {
		t.Fatalf("Load(seed) error: %v", err)
	}
	changed := EnsureOcswitchProvider("openai-responses", raw, "http://127.0.0.1:9982/v1", "ocswitch-local", []string{"gpt-5.4"})
	if changed {
		t.Fatal("EnsureOcswitchProvider() reported change for preserved same-name alias metadata")
	}
	if err := Save(path, raw); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load(saved) error: %v", err)
	}
	providerRaw := loaded["provider"].(map[string]any)
	providerEntry := providerRaw[ProviderKey].(map[string]any)
	models := providerEntry["models"].(map[string]any)
	model := models["gpt-5.4"].(map[string]any)
	if got := model["name"]; got != "custom-display-name" {
		t.Fatalf("saved model name = %#v, want custom-display-name preserved", got)
	}
	if _, ok := model["limit"].(map[string]any); !ok {
		t.Fatalf("saved limit metadata missing: %#v", model["limit"])
	}
	if _, ok := model["options"].(map[string]any); !ok {
		t.Fatalf("saved options metadata missing: %#v", model["options"])
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

func mustOcswitchModel(t *testing.T, raw Raw, providerKey string, alias string) map[string]any {
	t.Helper()
	providerRaw, _ := raw["provider"].(map[string]any)
	providerEntry, _ := providerRaw[providerKey].(map[string]any)
	models, _ := providerEntry["models"].(map[string]any)
	model, _ := models[alias].(map[string]any)
	if model == nil {
		t.Fatalf("missing provider.%s.models.%s in %#v", providerKey, alias, raw)
	}
	return model
}

func assertModelLimit(t *testing.T, model map[string]any, wantContext int64, wantOutput int64) {
	t.Helper()
	limit, _ := model["limit"].(map[string]any)
	if limit == nil {
		t.Fatalf("model limit missing: %#v", model["limit"])
	}
	assertNumericValue(t, limit, "context", wantContext)
	assertNumericValue(t, limit, "output", wantOutput)
}

func assertNumericValue(t *testing.T, values map[string]any, key string, want int64) {
	t.Helper()
	switch got := values[key].(type) {
	case int:
		if int64(got) == want {
			return
		}
	case int64:
		if got == want {
			return
		}
	case float64:
		if int64(got) == want && got == float64(want) {
			return
		}
	}
	t.Fatalf("%s = %#v, want %d", key, values[key], want)
}

func assertStringValues(t *testing.T, got any, want []string) {
	t.Helper()
	var values []string
	switch typed := got.(type) {
	case []string:
		values = typed
	case []any:
		for _, item := range typed {
			value, ok := item.(string)
			if !ok {
				t.Fatalf("string values contain non-string item: %#v", got)
			}
			values = append(values, value)
		}
	default:
		t.Fatalf("string values = %#v, want %#v", got, want)
	}
	if len(values) != len(want) {
		t.Fatalf("string values = %#v, want %#v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("string values = %#v, want %#v", values, want)
		}
	}
}

func assertBoolValue(t *testing.T, values map[string]any, key string, want bool) {
	t.Helper()
	got, ok := values[key].(bool)
	if !ok || got != want {
		t.Fatalf("%s = %#v, want %v", key, values[key], want)
	}
}
