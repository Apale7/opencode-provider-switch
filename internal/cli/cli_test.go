package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/anomalyco/opencode-provider-switch/internal/config"
)

func TestProviderAddPreservesExistingFields(t *testing.T) {
	t.Setenv("OLPX_CONFIG", filepath.Join(t.TempDir(), "olpx.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:       "p1",
		Name:     "Old",
		BaseURL:  "https://old.example.com/v1",
		APIKey:   "sk-old",
		Headers:  map[string]string{"X-Test": "1"},
		Disabled: true,
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://new.example.com/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if p.Name != "Old" {
		t.Fatalf("Name = %q, want Old", p.Name)
	}
	if p.APIKey != "sk-old" {
		t.Fatalf("APIKey = %q, want sk-old", p.APIKey)
	}
	if p.Headers["X-Test"] != "1" {
		t.Fatalf("Headers = %#v, want preserved header", p.Headers)
	}
	if p.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("BaseURL = %q, want updated URL", p.BaseURL)
	}
	if !p.Disabled {
		t.Fatal("Disabled = false, want true to preserve provider state")
	}
}

func TestProviderAddRejectsInvalidBaseURL(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "olpx.json")
	t.Setenv("OLPX_CONFIG", configFile)
	configPath = ""

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/api"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid --base-url error")
	}
	if got := err.Error(); got != "invalid --base-url: base_url must end with /v1" {
		t.Fatalf("error = %q", got)
	}
	if _, statErr := os.Stat(configFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected no config file write, stat err = %v", statErr)
	}
}

func TestAliasAddPreservesExistingFields(t *testing.T) {
	t.Setenv("OLPX_CONFIG", filepath.Join(t.TempDir(), "olpx.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertAlias(config.Alias{
		Alias:       "gpt-5.4",
		DisplayName: "Old Name",
		Enabled:     true,
		Targets:     []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAliasAddCmd()
	cmd.SetArgs([]string{"--name", "gpt-5.4"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute alias add: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	a := cfg.FindAlias("gpt-5.4")
	if a == nil {
		t.Fatal("alias gpt-5.4 not found")
	}
	if a.DisplayName != "Old Name" {
		t.Fatalf("DisplayName = %q, want Old Name", a.DisplayName)
	}
	if !a.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if len(a.Targets) != 1 || a.Targets[0].Provider != "p1" || a.Targets[0].Model != "up-1" {
		t.Fatalf("Targets = %#v, want preserved target", a.Targets)
	}
}

func TestProviderEnableDisableCommands(t *testing.T) {
	t.Setenv("OLPX_CONFIG", filepath.Join(t.TempDir(), "olpx.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "p1",
		BaseURL: "https://example.com/v1",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	disableCmd := newProviderDisableCmd()
	disableCmd.SetArgs([]string{"p1"})
	if err := disableCmd.Execute(); err != nil {
		t.Fatalf("execute provider disable: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload after disable: %v", err)
	}
	if provider := cfg.FindProvider("p1"); provider == nil || !provider.Disabled {
		t.Fatalf("provider after disable = %#v, want disabled=true", provider)
	}

	enableCmd := newProviderEnableCmd()
	enableCmd.SetArgs([]string{"p1"})
	if err := enableCmd.Execute(); err != nil {
		t.Fatalf("execute provider enable: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload after enable: %v", err)
	}
	if provider := cfg.FindProvider("p1"); provider == nil || provider.Disabled {
		t.Fatalf("provider after enable = %#v, want disabled=false", provider)
	}
}

func TestOpencodeSyncDoesNotPanicOnSliceModelMetadata(t *testing.T) {
	t.Setenv("OLPX_CONFIG", filepath.Join(t.TempDir(), "olpx.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "p1",
		BaseURL: "https://example.com/v1",
	})
	cfg.UpsertAlias(config.Alias{
		Alias:   "gpt-5.4",
		Enabled: true,
		Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	target := filepath.Join(t.TempDir(), "opencode.jsonc")
	seed := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"provider\": {\n    \"olpx\": {\n      \"npm\": \"@ai-sdk/openai\",\n      \"name\": \"OpenCode LocalProxy CLI\",\n      \"options\": {\n        \"baseURL\": \"http://127.0.0.1:9982/v1\",\n        \"apiKey\": \"olpx-local\",\n        \"setCacheKey\": true\n      },\n      \"models\": {\n        \"gpt-5.4\": {\n          \"name\": \"custom-display-name\",\n          \"tags\": [\"reasoning\", \"priority\"],\n          \"variants\": [\n            {\"name\": \"high\", \"effort\": \"high\"}\n          ]\n        }\n      }\n    }\n  }\n}\n")
	if err := os.WriteFile(target, seed, 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}

	cmd := newOpencodeSyncCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--target", target})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("opencode sync panicked with slice metadata: %v", r)
		}
	}()

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute opencode sync: %v", err)
	}
	if got := stdout.String(); got != "✓ no changes required at "+target+"\n" {
		t.Fatalf("stdout = %q", got)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target config: %v", err)
	}
	if !bytes.Equal(data, seed) {
		t.Fatalf("sync rewrote unchanged config:\n%s", string(data))
	}
}
