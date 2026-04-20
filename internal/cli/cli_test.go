package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestProviderAddPreservesExistingFields(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
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
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://new.example.com/v1", "--skip-models"})
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
	configFile := filepath.Join(t.TempDir(), "ocswitch.json")
	t.Setenv(config.ConfigEnvVar, configFile)
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

func TestProviderAddDiscoverySuccessStoresCatalog(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if got := strings.Join(p.Models, ","); got != "gpt-4.1,gpt-4o" {
		t.Fatalf("Models = %q", got)
	}
	if p.ModelsSource != "discovered" {
		t.Fatalf("ModelsSource = %q, want discovered", p.ModelsSource)
	}
}

func TestProviderAddDiscoveryFailureStillSavesProvider(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: could not discover provider models") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if p.BaseURL != srv.URL+"/v1" {
		t.Fatalf("BaseURL = %q", p.BaseURL)
	}
}

func TestProviderAddDiscoveryFailureMarksCatalogUntrustedAfterConnectionChange(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://old.example.com/v1",
		Models:       []string{"old-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1"})
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
	if got := strings.Join(p.Models, ","); got != "old-model" {
		t.Fatalf("Models = %q, want old-model preserved", got)
	}
	if p.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty to disable strict validation", p.ModelsSource)
	}
	if !strings.Contains(stderr.String(), "keeping existing model catalog as untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestProviderAddSkipModelsMarksCatalogUntrustedAfterConnectionChange(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://old.example.com/v1",
		Models:       []string{"old-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://new.example.com/v1", "--skip-models"})
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
	if got := strings.Join(p.Models, ","); got != "old-model" {
		t.Fatalf("Models = %q, want old-model preserved", got)
	}
	if p.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty to disable strict validation", p.ModelsSource)
	}
	if !strings.Contains(stderr.String(), "connection changed with --skip-models") || !strings.Contains(stderr.String(), "untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestProviderAddSkipModelsKeepsDiscoveredCatalogWhenConnectionEquivalent(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      " https://example.com/v1/ ",
		Models:       []string{"old-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--skip-models"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if strings.Contains(stderr.String(), "untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if got := strings.Join(p.Models, ","); got != "old-model" {
		t.Fatalf("Models = %q", got)
	}
	if p.ModelsSource != "discovered" {
		t.Fatalf("ModelsSource = %q, want discovered", p.ModelsSource)
	}
	if p.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL = %q, want normalized", p.BaseURL)
	}
}

func TestProviderAddSkipModelsKeepsDiscoveredCatalogWhenHeadersEquivalent(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Headers:      nil,
		Models:       []string{"old-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--skip-models"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if strings.Contains(stderr.String(), "untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if p.ModelsSource != "discovered" {
		t.Fatalf("ModelsSource = %q, want discovered", p.ModelsSource)
	}
}

func TestProviderAddDiscoveryEmptyKeepsDiscoveredCatalogWhenConnectionUnchanged(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      srv.URL + "/v1",
		Models:       []string{"gpt-4.1"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if !strings.Contains(stderr.String(), "keeping existing model catalog") || strings.Contains(stderr.String(), "untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if got := strings.Join(p.Models, ","); got != "gpt-4.1" {
		t.Fatalf("Models = %q", got)
	}
	if p.ModelsSource != "discovered" {
		t.Fatalf("ModelsSource = %q, want discovered", p.ModelsSource)
	}
}

func TestProviderAddDiscoveryEmptyMarksCatalogUntrustedAfterConnectionChange(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://old.example.com/v1",
		Models:       []string{"gpt-4.1"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if !strings.Contains(stderr.String(), "keeping existing model catalog") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if got := strings.Join(p.Models, ","); got != "gpt-4.1" {
		t.Fatalf("Models = %q", got)
	}
	if p.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty to disable strict validation after connection change", p.ModelsSource)
	}
}

func TestProviderAddRejectsEmptyHeaderName(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Headers:      map[string]string{"X-Test": "1"},
		Models:       []string{"known-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--header", "=x"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid --header error")
	}
	if got := err.Error(); got != `invalid --header "=x" (header name must not be empty)` {
		t.Fatalf("error = %q", got)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil || p.Headers["X-Test"] != "1" || p.ModelsSource != "discovered" {
		t.Fatalf("provider after failed header parse = %#v", p)
	}
}

func TestProviderAddLastHeaderWinsCaseInsensitive(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Token"); got != "b" {
			t.Fatalf("X-Token = %q, want b", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}]}`))
	}))
	defer srv.Close()

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", srv.URL + "/v1", "--header", "X-Token=a", "--header", "x-token=b"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if got := p.Headers["x-token"]; got != "b" {
		t.Fatalf("Headers = %#v, want lower-cased x-token=b", p.Headers)
	}
	if len(p.Headers) != 1 {
		t.Fatalf("Headers = %#v, want single merged header", p.Headers)
	}
}

func TestProviderAddAllowsClearingAPIKey(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "p1", BaseURL: "https://example.com/v1", APIKey: "sk-old"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--api-key", "", "--skip-models"})
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
	if p.APIKey != "" {
		t.Fatalf("APIKey = %q, want cleared empty string", p.APIKey)
	}
}

func TestProviderAddAllowsClearingHeaders(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "p1",
		BaseURL: "https://example.com/v1",
		Headers: map[string]string{"x-token": "abc", "x-workspace": "team"},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--clear-headers", "--skip-models"})
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
	if p.Headers != nil {
		t.Fatalf("Headers = %#v, want nil after --clear-headers", p.Headers)
	}
}

func TestProviderAddAllowsExplicitEnableViaDisabledFalse(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "p1", BaseURL: "https://example.com/v1", Disabled: true})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--disabled=false", "--skip-models"})
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
	if p.Disabled {
		t.Fatal("Disabled = true, want explicit false applied")
	}
}

func TestProviderAddSkipModelsKeepsDiscoveredCatalogWhenHeaderCaseChangesOnly(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Headers:      map[string]string{"X-Test": "1"},
		Models:       []string{"old-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newProviderAddCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--id", "p1", "--base-url", "https://example.com/v1", "--header", "x-test=1", "--skip-models"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider add: %v", err)
	}
	if strings.Contains(stderr.String(), "untrusted") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil || p.ModelsSource != "discovered" {
		t.Fatalf("provider after update = %#v", p)
	}
}

func TestAliasAddPreservesExistingFields(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
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
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
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
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
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
	seed := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"provider\": {\n    \"ocswitch\": {\n      \"npm\": \"@ai-sdk/openai\",\n      \"name\": \"OpenCode Provider Switch CLI\",\n      \"options\": {\n        \"baseURL\": \"http://127.0.0.1:9982/v1\",\n        \"apiKey\": \"ocswitch-local\",\n        \"setCacheKey\": true\n      },\n      \"models\": {\n        \"gpt-5.4\": {\n          \"name\": \"custom-display-name\",\n          \"tags\": [\"reasoning\", \"priority\"],\n          \"variants\": [\n            {\"name\": \"high\", \"effort\": \"high\"}\n          ]\n        }\n      }\n    }\n  }\n}\n")
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
	if got := stdout.String(); got != "✓ no changes required at "+target+" [openai-responses]\n" {
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

func TestOpencodeSyncRejectsInvalidSelectedModel(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "p1", BaseURL: "https://example.com/v1"})
	cfg.UpsertAlias(config.Alias{
		Alias:   "gpt-5.4",
		Enabled: true,
		Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	target := filepath.Join(t.TempDir(), "opencode.jsonc")
	cmd := newOpencodeSyncCmd()
	cmd.SetArgs([]string{"--target", target, "--set-model", "ocswitch/missing"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid --set-model error")
	}
	if got := err.Error(); got != `--set-model "ocswitch/missing" is not a routable alias; available: ocswitch/gpt-5.4` {
		t.Fatalf("error = %q", got)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("expected no target file write, stat err = %v", statErr)
	}
}

func TestOpencodeSyncRejectsNonPrefixedSelectedModel(t *testing.T) {
	err := validateSyncedModelSelection("gpt-5.4", []string{"gpt-5.4"}, "--set-model")
	if err == nil {
		t.Fatal("expected prefix validation error")
	}
	if got := err.Error(); got != "--set-model must use the ocswitch/<alias> form" {
		t.Fatalf("error = %q", got)
	}
}

func TestHelpTextIncludesOperationalGuidance(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *cobra.Command
		wantLong    []string
		wantExample []string
	}{
		{
			name:        "root",
			cmd:         NewRootCmd("test"),
			wantLong:    []string{"Typical workflow:", "prefer command-local --help over README summaries"},
			wantExample: []string{"ocswitch provider add", "ocswitch serve"},
		},
		{
			name:        "alias root",
			cmd:         newAliasCmd(),
			wantLong:    []string{"ordered target chain", "Common workflow:"},
			wantExample: []string{"--model su8/gpt-5.4", "--model codex/GPT-5.4"},
		},
		{
			name:        "provider add",
			cmd:         newProviderAddCmd(),
			wantLong:    []string{"/v1/models endpoint", "stores the discovered", "Use --clear-headers", "Use\n--skip-models"},
			wantExample: []string{"--skip-models", "--header X-Workspace=my-team"},
		},
		{
			name:        "alias bind",
			cmd:         newAliasBindCmd(),
			wantLong:    []string{"combined form is recommended", "stored model catalog", "Order matters:"},
			wantExample: []string{"--model codex/GPT-5.4", "--disabled"},
		},
		{
			name:        "opencode sync",
			cmd:         newOpencodeSyncCmd(),
			wantLong:    []string{"does not follow", "writes alias", "exposure into provider.ocswitch.models"},
			wantExample: []string{"--dry-run", "--set-small-model ocswitch/gpt-5.4-mini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cmd.Long == "" {
				t.Fatal("Long help is empty")
			}
			if tt.cmd.Example == "" {
				t.Fatal("Example help is empty")
			}
			for _, want := range tt.wantLong {
				if !strings.Contains(tt.cmd.Long, want) {
					t.Fatalf("Long help missing %q\n%s", want, tt.cmd.Long)
				}
			}
			for _, want := range tt.wantExample {
				if !strings.Contains(tt.cmd.Example, want) {
					t.Fatalf("Example help missing %q\n%s", want, tt.cmd.Example)
				}
			}
		})
	}
	root := NewRootCmd("test")
	flag := root.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatal("--config flag not found")
	}
	for _, want := range []string{"$OCSWITCH_CONFIG", "$XDG_CONFIG_HOME/ocswitch/config.json", "~/.config/ocswitch/config.json"} {
		if !strings.Contains(flag.Usage, want) {
			t.Fatalf("config flag usage missing %q: %s", want, flag.Usage)
		}
	}
}

func TestAliasBindAcceptsSlashModelWithExplicitProvider(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "relay", BaseURL: "https://example.com/v1", Models: []string{"openrouter/google/gemini-2.5-pro"}, ModelsSource: "discovered"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAliasBindCmd()
	cmd.SetArgs([]string{"--alias", "gemini", "--provider", "relay", "--model", "openrouter/google/gemini-2.5-pro"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute alias bind: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	a := cfg.FindAlias("gemini")
	if a == nil || len(a.Targets) != 1 || a.Targets[0].Model != "openrouter/google/gemini-2.5-pro" {
		t.Fatalf("alias targets = %#v", a)
	}
}

func TestAliasUnbindAcceptsSlashModelWithExplicitProvider(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "relay", BaseURL: "https://example.com/v1"})
	cfg.UpsertAlias(config.Alias{Alias: "gemini", Enabled: true, Targets: []config.Target{{Provider: "relay", Model: "openrouter/google/gemini-2.5-pro", Enabled: true}}})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAliasUnbindCmd()
	cmd.SetArgs([]string{"--alias", "gemini", "--provider", "relay", "--model", "openrouter/google/gemini-2.5-pro"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute alias unbind: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	a := cfg.FindAlias("gemini")
	if a == nil || len(a.Targets) != 0 {
		t.Fatalf("alias targets = %#v", a)
	}
}

func TestImportedModelsDoNotBlockAliasBind(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Models:       []string{"subset-only"},
		ModelsSource: "imported",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAliasBindCmd()
	cmd.SetArgs([]string{"--alias", "gpt", "--provider", "p1", "--model", "different-real-model"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute alias bind: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	a := cfg.FindAlias("gpt")
	if a == nil || len(a.Targets) != 1 || a.Targets[0].Model != "different-real-model" {
		t.Fatalf("alias targets = %#v", a)
	}
}

func TestProviderImportOpencodeSkipsInvalidBaseURL(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	src := filepath.Join(t.TempDir(), "opencode.json")
	data := `{
	  "provider": {
	    "bad": {
	      "npm": "@ai-sdk/openai",
	      "options": {"baseURL": "https://example.com/api", "apiKey": "sk-test"}
	    }
	  }
	}`
	if err := os.WriteFile(src, []byte(data), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cmd := newProviderImportCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--from", src})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider import-opencode: %v", err)
	}
	if !strings.Contains(stdout.String(), `skip "bad" (invalid baseURL`) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.FindProvider("bad") != nil {
		t.Fatal("invalid imported provider should not be saved")
	}
}

func TestProviderImportOpencodeMissingFromFileReturnsError(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	missing := filepath.Join(t.TempDir(), "missing.jsonc")
	cmd := newProviderImportCmd()
	cmd.SetArgs([]string{"--from", missing})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing --from file error")
	}
	if !strings.Contains(err.Error(), missing) || (!strings.Contains(strings.ToLower(err.Error()), "no such file or directory") && !strings.Contains(strings.ToLower(err.Error()), "cannot find the file")) {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestProviderImportOpencodeOverwritePreservesLocalStateAndDemotesCatalogAfterAPIKeyChange(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		Name:         "Local Name",
		BaseURL:      "https://old.example.com/v1",
		APIKey:       "sk-old",
		Headers:      map[string]string{"X-Test": "1"},
		Models:       []string{"discovered-model"},
		ModelsSource: "discovered",
		Disabled:     true,
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	src := filepath.Join(t.TempDir(), "opencode.json")
	data := `{
	  "provider": {
	    "p1": {
	      "npm": "@ai-sdk/openai",
	      "name": "Imported Name",
	      "options": {"baseURL": "https://old.example.com/v1", "apiKey": "sk-new"},
	      "models": {"imported-model": {}}
	    }
	  }
	}`
	if err := os.WriteFile(src, []byte(data), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cmd := newProviderImportCmd()
	cmd.SetArgs([]string{"--from", src, "--overwrite"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider import-opencode: %v", err)
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if !p.Disabled {
		t.Fatal("Disabled = false, want preserved true")
	}
	if p.Headers["X-Test"] != "1" {
		t.Fatalf("Headers = %#v, want preserved local headers", p.Headers)
	}
	if p.Name != "Imported Name" {
		t.Fatalf("Name = %q, want imported name", p.Name)
	}
	if p.APIKey != "sk-new" {
		t.Fatalf("APIKey = %q, want imported value", p.APIKey)
	}
	if got := strings.Join(p.Models, ","); got != "imported-model" {
		t.Fatalf("Models = %q, want imported catalog after API key change", got)
	}
	if p.ModelsSource != "imported" {
		t.Fatalf("ModelsSource = %q, want imported after API key change", p.ModelsSource)
	}
}

func TestProviderImportOpencodeOverwriteClearsRemovedImportedModels(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Models:       []string{"old-imported-model"},
		ModelsSource: "imported",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	src := filepath.Join(t.TempDir(), "opencode.json")
	data := `{
	  "provider": {
	    "p1": {
	      "npm": "@ai-sdk/openai",
	      "options": {"baseURL": "https://example.com/v1", "apiKey": "sk-test"}
	    }
	  }
	}`
	if err := os.WriteFile(src, []byte(data), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cmd := newProviderImportCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--from", src, "--overwrite"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute provider import-opencode: %v", err)
	}
	if !strings.Contains(stdout.String(), `import "p1" [openai-responses] → https://example.com/v1 (models: )`) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	cfg, err = loadCfg()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	p := cfg.FindProvider("p1")
	if p == nil {
		t.Fatal("provider p1 not found")
	}
	if len(p.Models) != 0 {
		t.Fatalf("Models = %#v, want empty after imported models removed", p.Models)
	}
	if p.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty when imported models removed", p.ModelsSource)
	}
}

func TestDiscoveredModelsStillBlockUnknownAliasBind(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, filepath.Join(t.TempDir(), "ocswitch.json"))
	configPath = ""

	cfg, err := loadCfg()
	if err != nil {
		t.Fatalf("loadCfg: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "p1",
		BaseURL:      "https://example.com/v1",
		Models:       []string{"known-model"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAliasBindCmd()
	cmd.SetArgs([]string{"--alias", "gpt", "--provider", "p1", "--model", "unknown-model"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected alias bind error")
	}
	if !strings.Contains(err.Error(), `model "unknown-model" is not in provider "p1" discovered models`) {
		t.Fatalf("error = %q", err.Error())
	}
}
