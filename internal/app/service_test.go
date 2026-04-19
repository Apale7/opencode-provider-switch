package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestSaveDesktopPrefsPersistsToConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	prefs, err := svc.SaveDesktopPrefs(context.Background(), DesktopPrefsInput{
		LaunchAtLogin:  true,
		MinimizeToTray: true,
		Notifications:  true,
		Theme:          "dark",
		Language:       "zh-CN",
	})
	if err != nil {
		t.Fatalf("SaveDesktopPrefs() error = %v", err)
	}
	if !prefs.LaunchAtLogin || !prefs.MinimizeToTray || !prefs.Notifications || prefs.Theme != "dark" || prefs.Language != "zh-CN" {
		t.Fatalf("SaveDesktopPrefs() = %#v", prefs)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.Desktop.LaunchAtLogin || !cfg.Desktop.MinimizeToTray || !cfg.Desktop.Notifications || cfg.Desktop.Theme != "dark" || cfg.Desktop.Language != "zh-CN" {
		t.Fatalf("persisted desktop prefs = %#v", cfg.Desktop)
	}
}

func TestSaveDesktopPrefsNormalizesUnknownThemeAndLanguage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	prefs, err := svc.SaveDesktopPrefs(context.Background(), DesktopPrefsInput{
		Theme:    "night-mode",
		Language: "fr-FR",
	})
	if err != nil {
		t.Fatalf("SaveDesktopPrefs() error = %v", err)
	}
	if prefs.Theme != "system" || prefs.Language != "system" {
		t.Fatalf("normalized prefs = %#v", prefs)
	}
}

func TestStartStopProxyUpdatesStatus(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfgPathPort := freePort(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Port = cfgPathPort
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.APIKey = config.DefaultLocalAPIKey
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	if err := svc.StartProxy(context.Background()); err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	t.Cleanup(func() {
		_ = svc.StopProxy(context.Background())
	})

	status, err := svc.GetProxyStatus(context.Background())
	if err != nil {
		t.Fatalf("GetProxyStatus() error = %v", err)
	}
	if !status.Running {
		t.Fatalf("status.Running = false, want true")
	}

	assertEventually(t, func() bool {
		resp, err := http.Get("http://127.0.0.1:" + itoa(cfgPathPort) + "/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	if err := svc.StopProxy(context.Background()); err != nil {
		t.Fatalf("StopProxy() error = %v", err)
	}

	status, err = svc.GetProxyStatus(context.Background())
	if err != nil {
		t.Fatalf("GetProxyStatus() after stop error = %v", err)
	}
	if status.Running {
		t.Fatalf("status.Running = true, want false")
	}
}

func TestGetProxyStatusUsesCurrentConfigAddressWhenStopped(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	firstPort := freePort(t)
	secondPort := freePort(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = firstPort
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	if err := svc.StartProxy(context.Background()); err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	if err := svc.StopProxy(context.Background()); err != nil {
		t.Fatalf("StopProxy() error = %v", err)
	}

	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() reload error = %v", err)
	}
	cfg.Server.Port = secondPort
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() update error = %v", err)
	}

	status, err := svc.GetProxyStatus(context.Background())
	if err != nil {
		t.Fatalf("GetProxyStatus() error = %v", err)
	}
	if status.Running {
		t.Fatalf("status.Running = true, want false")
	}
	want := "127.0.0.1:" + itoa(secondPort)
	if status.BindAddress != want {
		t.Fatalf("status.BindAddress = %q, want %q", status.BindAddress, want)
	}
}

func TestUpsertProviderReturnsWarningsAndKeepsCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:           "relay",
		BaseURL:      "https://old.example.com/v1",
		APIKey:       "sk-old",
		Models:       []string{"gpt-4.1"},
		ModelsSource: "discovered",
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v1/models")
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer upstream.Close()

	svc := NewService(path)
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "relay",
		BaseURL: upstream.URL + "/v1",
		APIKey:  "sk-new",
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if result.Provider.BaseURL != upstream.URL+"/v1" {
		t.Fatalf("saved baseUrl = %q, want %q", result.Provider.BaseURL, upstream.URL+"/v1")
	}
	if !containsWarning(result.Warnings, "model discovery failed") {
		t.Fatalf("warnings %#v do not mention stale catalog preservation", result.Warnings)
	}
	if !containsWarning(result.Warnings, "could not discover provider models") {
		t.Fatalf("warnings %#v do not mention discovery failure", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("relay")
	if provider == nil {
		t.Fatal("provider relay not found after save")
	}
	if provider.ModelsSource != "" {
		t.Fatalf("provider.ModelsSource = %q, want empty", provider.ModelsSource)
	}
	if len(provider.Models) != 1 || provider.Models[0] != "gpt-4.1" {
		t.Fatalf("provider.Models = %#v, want existing catalog kept", provider.Models)
	}
}

func TestImportProvidersReturnsWarnings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "ocswitch.json")
	sourcePath := filepath.Join(dir, "opencode.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "keep", BaseURL: "https://existing.example.com/v1"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(`{
		"provider": {
			"keep": {
				"npm": "@ai-sdk/openai",
				"options": {"baseURL": "https://duplicate.example.com/v1", "apiKey": "sk-dup"}
			},
			"broken": {
				"npm": "@ai-sdk/openai",
				"options": {"baseURL": "https://broken.example.com", "apiKey": "sk-bad"}
			},
			"fresh": {
				"npm": "@ai-sdk/openai",
				"name": "Fresh",
				"options": {"baseURL": "https://fresh.example.com/v1", "apiKey": "sk-fresh"},
				"models": {"gpt-4.1": {}}
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.ImportProviders(context.Background(), ProviderImportInput{SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("ImportProviders() error = %v", err)
	}
	if result.Imported != 1 || result.Skipped != 2 {
		t.Fatalf("ImportProviders() = %#v, want imported=1 skipped=2", result)
	}
	if !containsWarning(result.Warnings, `skip "keep"`) {
		t.Fatalf("warnings %#v do not mention duplicate provider", result.Warnings)
	}
	if !containsWarning(result.Warnings, `skip "broken"`) {
		t.Fatalf("warnings %#v do not mention invalid provider", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if provider := reloaded.FindProvider("fresh"); provider == nil {
		t.Fatal("provider fresh not imported")
	}
}

func TestSetAliasTargetDisabledPersistsState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "relay", BaseURL: "https://relay.example.com/v1"})
	cfg.UpsertAlias(config.Alias{
		Alias:   "chat",
		Enabled: true,
		Targets: []config.Target{{Provider: "relay", Model: "gpt-4.1", Enabled: true}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	disabled, err := svc.SetAliasTargetDisabled(context.Background(), AliasTargetInput{
		Alias:    "chat",
		Provider: "relay",
		Model:    "gpt-4.1",
		Disabled: true,
	})
	if err != nil {
		t.Fatalf("SetAliasTargetDisabled(disable) error = %v", err)
	}
	if disabled.AvailableTargetCount != 0 || disabled.Targets[0].Enabled {
		t.Fatalf("disabled alias view = %#v", disabled)
	}

	enabled, err := svc.SetAliasTargetDisabled(context.Background(), AliasTargetInput{
		Alias:    "chat",
		Provider: "relay",
		Model:    "gpt-4.1",
		Disabled: false,
	})
	if err != nil {
		t.Fatalf("SetAliasTargetDisabled(enable) error = %v", err)
	}
	if enabled.AvailableTargetCount != 1 || !enabled.Targets[0].Enabled {
		t.Fatalf("enabled alias view = %#v", enabled)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	alias := reloaded.FindAlias("chat")
	if alias == nil {
		t.Fatal("alias chat not found after update")
	}
	if len(alias.Targets) != 1 || !alias.Targets[0].Enabled {
		t.Fatalf("persisted alias targets = %#v", alias.Targets)
	}
}

func TestUpsertAliasCanReEnableExistingAlias(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertAlias(config.Alias{Alias: "chat", DisplayName: "Chat", Enabled: false})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	alias, err := svc.UpsertAlias(context.Background(), AliasUpsertInput{
		Alias:       "chat",
		DisplayName: "Chat enabled",
		Disabled:    false,
	})
	if err != nil {
		t.Fatalf("UpsertAlias() error = %v", err)
	}
	if !alias.Enabled {
		t.Fatalf("alias.Enabled = false, want true: %#v", alias)
	}
	if alias.DisplayName != "Chat enabled" {
		t.Fatalf("alias.DisplayName = %q, want %q", alias.DisplayName, "Chat enabled")
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	persisted := reloaded.FindAlias("chat")
	if persisted == nil || !persisted.Enabled {
		t.Fatalf("persisted alias = %#v, want enabled", persisted)
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}
	return false
}

func assertEventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
