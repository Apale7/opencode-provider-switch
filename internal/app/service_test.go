package app

import (
	"context"
	"encoding/json"
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
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
)

func TestSaveDesktopPrefsPersistsToConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	prefs, err := svc.SaveDesktopPrefs(context.Background(), DesktopPrefsInput{
		LaunchAtLogin:  true,
		AutoStartProxy: true,
		MinimizeToTray: true,
		Notifications:  true,
		Theme:          "dark",
		Language:       "zh-CN",
	})
	if err != nil {
		t.Fatalf("SaveDesktopPrefs() error = %v", err)
	}
	if !prefs.LaunchAtLogin || !prefs.AutoStartProxy || !prefs.MinimizeToTray || !prefs.Notifications || prefs.Theme != "dark" || prefs.Language != "zh-CN" {
		t.Fatalf("SaveDesktopPrefs() = %#v", prefs)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.Desktop.LaunchAtLogin || !cfg.Desktop.AutoStartProxy || !cfg.Desktop.MinimizeToTray || !cfg.Desktop.Notifications || cfg.Desktop.Theme != "dark" || cfg.Desktop.Language != "zh-CN" {
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
	if prefs.Theme != "system" || prefs.Language != "en-US" {
		t.Fatalf("normalized prefs = %#v", prefs)
	}
}

func TestGetDesktopPrefsDefaultsLanguageToEnglish(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	prefs, err := svc.GetDesktopPrefs(context.Background())
	if err != nil {
		t.Fatalf("GetDesktopPrefs() error = %v", err)
	}
	if prefs.Language != "en-US" {
		t.Fatalf("default language = %q, want en-US", prefs.Language)
	}
}

func TestSaveDesktopPrefsPreservesSystemLanguage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	prefs, err := svc.SaveDesktopPrefs(context.Background(), DesktopPrefsInput{Language: "system"})
	if err != nil {
		t.Fatalf("SaveDesktopPrefs() error = %v", err)
	}
	if prefs.Language != "system" {
		t.Fatalf("saved language = %q, want system", prefs.Language)
	}
}

func TestSaveProxySettingsPersistsToConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{
		ConnectTimeoutMs:        12000,
		ResponseHeaderTimeoutMs: 21000,
		FirstByteTimeoutMs:      22000,
		RequestReadTimeoutMs:    33000,
		StreamIdleTimeoutMs:     70000,
		Routing: ProxyRoutingSettingsInput{
			Strategy: "circuit-breaker",
			Params:   json.RawMessage(`{"failureThreshold":3,"baseCooldownMs":45000,"maxCooldownMs":90000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":false,"rateLimitCooldownMs":12000}`),
		},
	})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if result.Settings.ConnectTimeoutMs != 12000 || result.Settings.ResponseHeaderTimeoutMs != 21000 || result.Settings.FirstByteTimeoutMs != 22000 || result.Settings.RequestReadTimeoutMs != 33000 || result.Settings.StreamIdleTimeoutMs != 70000 {
		t.Fatalf("SaveProxySettings() = %#v", result.Settings)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Server.ConnectTimeoutMs != 12000 || cfg.Server.ResponseHeaderTimeoutMs != 21000 || cfg.Server.FirstByteTimeoutMs != 22000 || cfg.Server.RequestReadTimeoutMs != 33000 || cfg.Server.StreamIdleTimeoutMs != 70000 {
		t.Fatalf("persisted server settings = %#v", cfg.Server)
	}
	if cfg.Server.Routing.Strategy != routing.DefaultStrategy {
		t.Fatalf("routing strategy = %q, want %q", cfg.Server.Routing.Strategy, routing.DefaultStrategy)
	}
	params, err := routing.ResolveParams(cfg.Server.Routing)
	if err != nil {
		t.Fatalf("routing.ResolveParams() error = %v", err)
	}
	if got := params["failureThreshold"]; got != 3 {
		t.Fatalf("failureThreshold = %#v, want 3", got)
	}
	if got := params["countPostCommitErrors"]; got != false {
		t.Fatalf("countPostCommitErrors = %#v, want false", got)
	}
}

func TestSaveProxySettingsWarnsWhenProxyRunning(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	port := freePort(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	if err := svc.StartProxy(context.Background()); err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	defer func() { _ = svc.StopProxy(context.Background()) }()

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{ConnectTimeoutMs: 12000})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "restart proxy") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestSaveProxySettingsNormalizesNonPositiveValues(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{
		ConnectTimeoutMs:        0,
		ResponseHeaderTimeoutMs: -1,
		FirstByteTimeoutMs:      0,
		RequestReadTimeoutMs:    -50,
		StreamIdleTimeoutMs:     0,
	})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if result.Settings.ConnectTimeoutMs != config.DefaultConnectTimeoutMs ||
		result.Settings.ResponseHeaderTimeoutMs != config.DefaultResponseHeaderTimeoutMs ||
		result.Settings.FirstByteTimeoutMs != config.DefaultFirstByteTimeoutMs ||
		result.Settings.RequestReadTimeoutMs != config.DefaultRequestReadTimeoutMs ||
		result.Settings.StreamIdleTimeoutMs != config.DefaultStreamIdleTimeoutMs {
		t.Fatalf("normalized settings = %#v", result.Settings)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Server.ConnectTimeoutMs != config.DefaultConnectTimeoutMs ||
		cfg.Server.ResponseHeaderTimeoutMs != config.DefaultResponseHeaderTimeoutMs ||
		cfg.Server.FirstByteTimeoutMs != config.DefaultFirstByteTimeoutMs ||
		cfg.Server.RequestReadTimeoutMs != config.DefaultRequestReadTimeoutMs ||
		cfg.Server.StreamIdleTimeoutMs != config.DefaultStreamIdleTimeoutMs {
		t.Fatalf("persisted server settings = %#v", cfg.Server)
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

func TestStartProxyReturnsBindErrorWithoutRunningStatus(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	port := freePort(t)
	listener, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Server.APIKey = config.DefaultLocalAPIKey
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	err = svc.StartProxy(context.Background())
	if err == nil {
		t.Fatal("StartProxy() error = nil, want bind failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "bind") {
		t.Fatalf("StartProxy() error = %v, want bind failure", err)
	}

	status, statusErr := svc.GetProxyStatus(context.Background())
	if statusErr != nil {
		t.Fatalf("GetProxyStatus() error = %v", statusErr)
	}
	if status.Running {
		t.Fatalf("status = %#v, want stopped", status)
	}
	if status.LastError == "" {
		t.Fatalf("status = %#v, want last error", status)
	}
}

func TestConcurrentStartProxyCallsShareStartupResult(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	port := freePort(t)
	listener, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Server.APIKey = config.DefaultLocalAPIKey
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	errCh := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			errCh <- svc.StartProxy(context.Background())
		}()
	}
	close(start)

	for range 2 {
		err := <-errCh
		if err == nil {
			t.Fatal("StartProxy() error = nil, want bind failure")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "bind") {
			t.Fatalf("StartProxy() error = %v, want bind failure", err)
		}
	}

	status, statusErr := svc.GetProxyStatus(context.Background())
	if statusErr != nil {
		t.Fatalf("GetProxyStatus() error = %v", statusErr)
	}
	if status.Running {
		t.Fatalf("status = %#v, want stopped", status)
	}
	if status.LastError == "" {
		t.Fatalf("status = %#v, want last error", status)
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

func TestPingProviderBaseURLSupportsDraftInput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-draft" {
			t.Fatalf("Authorization = %q, want Bearer sk-draft", got)
		}
		if got := r.Header.Get("X-Test"); got != "1" {
			t.Fatalf("X-Test = %q, want 1", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}]}`))
	}))
	defer upstream.Close()

	svc := NewService(path)
	result, err := svc.PingProviderBaseURL(context.Background(), ProviderPingInput{
		Protocol: "openai-responses",
		BaseURL:  upstream.URL + "/v1",
		APIKey:   "sk-draft",
		Headers:  map[string]string{"X-Test": "1"},
	})
	if err != nil {
		t.Fatalf("PingProviderBaseURL() error = %v", err)
	}
	if !result.Reachable {
		t.Fatalf("result.Reachable = false, want true: %#v", result)
	}
	if result.BaseURL != upstream.URL+"/v1" {
		t.Fatalf("result.BaseURL = %q, want %q", result.BaseURL, upstream.URL+"/v1")
	}
}

func TestUpsertProviderUsesAnyReachableBaseURLForModelDiscovery(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4o"}]}`))
	}))
	defer second.Close()

	svc := NewService(path)
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:       "relay",
		Protocol: "openai-responses",
		BaseURL:  first.URL + "/v1",
		BaseURLs: []string{first.URL + "/v1", second.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want empty", result.Warnings)
	}
	if !strings.Contains(strings.Join(result.Provider.Models, ","), "gpt-4.1") {
		t.Fatalf("models = %#v", result.Provider.Models)
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

func TestReorderAliasTargetsPersistsOrder(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{ID: "p1", BaseURL: "https://p1.example.com/v1"})
	cfg.UpsertProvider(config.Provider{ID: "p2", BaseURL: "https://p2.example.com/v1"})
	cfg.UpsertAlias(config.Alias{
		Alias:   "chat",
		Enabled: true,
		Targets: []config.Target{
			{Provider: "p1", Model: "up-1", Enabled: true},
			{Provider: "p2", Model: "up-2", Enabled: false},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	view, err := svc.ReorderAliasTargets(context.Background(), AliasTargetReorderInput{
		Alias: "chat",
		Targets: []AliasTargetRefInput{
			{Provider: "p2", Model: "up-2"},
			{Provider: "p1", Model: "up-1"},
		},
	})
	if err != nil {
		t.Fatalf("ReorderAliasTargets() error = %v", err)
	}
	if len(view.Targets) != 2 || view.Targets[0].Provider != "p2" || view.Targets[0].Enabled {
		t.Fatalf("alias view targets = %#v, want p2 first and still disabled", view.Targets)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	alias := reloaded.FindAlias("chat")
	if alias == nil {
		t.Fatal("alias chat not found after reorder")
	}
	if len(alias.Targets) != 2 || alias.Targets[0].Provider != "p2" || alias.Targets[0].Enabled {
		t.Fatalf("persisted targets = %#v, want p2 first and still disabled", alias.Targets)
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

func TestReconcileRuntimeSnapshotReportsDriftCategories(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{Host: "127.0.0.1", Port: 9982},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	}
	fileSnapshot := opencode.FileConfigSnapshot{
		TargetPath:   "opencode.jsonc",
		DefaultModel: "ocswitch/legacy",
		SmallModel:   "ocswitch/legacy-mini",
		SyncedProviders: []opencode.FileProviderSnapshot{{
			Key:                "ocswitch",
			Protocol:           config.ProtocolOpenAIResponses,
			NPM:                "@ai-sdk/openai",
			ModelAliases:       []string{"legacy"},
			ContractConfigured: true,
		}},
	}
	runtimeSnapshot := opencode.RuntimeConfigSnapshot{
		BaseURL:         "http://runtime",
		Directory:       "/workspace/demo",
		Reachable:       true,
		ConfigLoaded:    true,
		ProvidersLoaded: true,
		DefaultModel:    "ocswitch/missing",
		SmallModel:      "bad-small-model",
		Providers: []opencode.RuntimeProviderSnapshot{{
			ID:       "ocswitch",
			NPM:      "@custom/runtime",
			ModelIDs: []string{"legacy-runtime"},
		}},
	}

	issues := reconcileRuntimeSnapshot(cfg, fileSnapshot, runtimeSnapshot)
	assertDoctorIssueCodes(t, issues, "runtime_provider_protocol_mismatch", "catalog_drift", "default_model_invalid", "small_model_invalid")
}

func TestPreviewOpenCodeSyncIncludesRuntimeUnreachableAndSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9982
	cfg.Server.APIKey = config.DefaultLocalAPIKey
	cfg.UpsertProvider(config.Provider{ID: "p1", BaseURL: "https://example.com/v1"})
	cfg.UpsertAlias(config.Alias{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{{Provider: "p1", Model: "up-1", Enabled: true}}})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	target := filepath.Join(t.TempDir(), "opencode.jsonc")
	svc := NewService(path)
	preview, err := svc.PreviewOpenCodeSync(context.Background(), SyncInput{
		Target:           target,
		RuntimeBaseURL:   "http://127.0.0.1:1",
		RuntimeDirectory: "/workspace/demo",
		SetModel:         "ocswitch/gpt-5.4",
	})
	if err != nil {
		t.Fatalf("PreviewOpenCodeSync() error = %v", err)
	}
	if !preview.WouldChange {
		t.Fatalf("preview = %#v, want WouldChange=true", preview)
	}
	assertDoctorIssueCodes(t, preview.DoctorIssues, "runtime_unreachable")
	if preview.RuntimeBaseURL != "http://127.0.0.1:1" {
		t.Fatalf("preview.RuntimeBaseURL = %q", preview.RuntimeBaseURL)
	}
	if preview.RuntimeDirectory != "/workspace/demo" {
		t.Fatalf("preview.RuntimeDirectory = %q", preview.RuntimeDirectory)
	}
	if preview.Summary.RuntimeReachable {
		t.Fatalf("summary = %#v, want runtime unreachable", preview.Summary)
	}
	if !preview.Summary.FileSnapshotAvailable {
		t.Fatalf("summary = %#v, want file snapshot available", preview.Summary)
	}
}

func TestQueryRequestTracesReturnsRequestedPage(t *testing.T) {
	svc := NewService(filepath.Join(t.TempDir(), "ocswitch.json"))
	svc.traces = proxy.NewTraceStore(10)
	baseTime := time.Now().UTC()
	for id := 1; id <= 5; id++ {
		if err := svc.traces.Add(context.Background(), proxy.RequestTrace{
			ID:        uint64(id),
			StartedAt: baseTime.Add(time.Duration(id) * time.Second),
			Protocol:  config.ProtocolOpenAIResponses,
			Alias:     "chat",
			Success:   true,
		}); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", id, err)
		}
	}

	result, err := svc.QueryRequestTraces(context.Background(), RequestTraceListInput{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("QueryRequestTraces() error = %v", err)
	}
	if result.Page != 2 || result.PageSize != 2 || result.Total != 5 {
		t.Fatalf("result metadata = %#v, want page=2 pageSize=2 total=5", result)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items count = %d, want 2", len(result.Items))
	}
	if result.Items[0].ID != 3 || result.Items[1].ID != 2 {
		t.Fatalf("items ids = %d,%d, want 3,2", result.Items[0].ID, result.Items[1].ID)
	}
}

func TestQueryRequestTracesAcceptsTimeRange(t *testing.T) {
	svc := NewService(filepath.Join(t.TempDir(), "ocswitch.json"))
	svc.traces = proxy.NewTraceStore(10)
	baseTime := time.Now().UTC().Add(-1 * time.Hour)
	for id := 1; id <= 3; id++ {
		if err := svc.traces.Add(context.Background(), proxy.RequestTrace{
			ID:        uint64(id),
			StartedAt: baseTime.Add(time.Duration(id) * time.Minute),
			Protocol:  config.ProtocolOpenAIResponses,
			Alias:     "chat",
			Success:   id != 2,
		}); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", id, err)
		}
	}

	result, err := svc.QueryRequestTraces(context.Background(), RequestTraceListInput{
		Page:        1,
		PageSize:    10,
		StartedFrom: baseTime.Add(90 * time.Second).Format(time.RFC3339Nano),
		StartedTo:   baseTime.Add(150 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("QueryRequestTraces() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != 2 {
		t.Fatalf("items = total %d %#v, want id=2", result.Total, result.Items)
	}
	if result.Stats.Success != 0 || result.Stats.Failed != 1 {
		t.Fatalf("stats = %#v, want failed only", result.Stats)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertDoctorIssueCodes(t *testing.T, issues []DoctorIssue, wantCodes ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, issue := range issues {
		seen[issue.Code] = true
	}
	for _, code := range wantCodes {
		if !seen[code] {
			t.Fatalf("issue codes = %#v, want %q", seen, code)
		}
	}
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
