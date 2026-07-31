package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
	_ "modernc.org/sqlite"
)

func TestNewServiceReportsTraceStoreStatus(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	overview, err := svc.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.TraceStore.Mode != "sqlite" {
		t.Fatalf("trace store mode = %q, want sqlite", overview.TraceStore.Mode)
	}
	if overview.TraceStore.Path == "" || overview.TraceStore.Error != "" {
		t.Fatalf("trace store status = %#v, want sqlite path without error", overview.TraceStore)
	}
}

func TestNewServiceReportsTraceStoreFallbackError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "ocswitch.json")
	dbPath := filepath.Join(dir, "traces.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec("CREATE VIEW request_traces AS SELECT 1 AS id"); err != nil {
		_ = db.Close()
		t.Fatalf("create incompatible trace view error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	svc := NewService(configPath)
	overview, err := svc.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.TraceStore.Mode != "memory" {
		t.Fatalf("trace store mode = %q, want memory", overview.TraceStore.Mode)
	}
	if overview.TraceStore.Error == "" {
		t.Fatalf("trace store status = %#v, want error", overview.TraceStore)
	}
}

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

func TestUpsertProviderCreateDefaultGroupMultiKeyAndClearViaGroup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	ctx := context.Background()

	created, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:              "p1",
		BaseURL:         "https://example.com/v1",
		BaseURLStrategy: config.ProviderBaseURLStrategyOrdered,
		DefaultGroup:    testDefaultGroupInput(config.ProtocolOpenAIResponses, true, "sk-1", "sk-2"),
	})
	if err != nil {
		t.Fatalf("UpsertProvider(create) error = %v", err)
	}
	if len(created.Provider.Groups) != 1 || created.Provider.Groups[0].APIKeyCount != 2 {
		t.Fatalf("create groups = %#v", created.Provider.Groups)
	}

	// Clearing keys is group-owned — use UpdateProviderGroup, not shared Upsert.
	_, err = svc.UpdateProviderGroup(ctx, ProviderGroupUpdateInput{
		ProviderID: "p1",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        nil,
		},
	})
	if err != nil {
		t.Fatalf("UpdateProviderGroup(clear) error = %v", err)
	}
	providers, err := svc.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(providers) != 1 || len(providers[0].Groups) != 1 || providers[0].Groups[0].APIKeyCount != 0 {
		t.Fatalf("providers after clear = %#v", providers)
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
	excludeFirstTokenLatency := false

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{
		ConnectTimeoutMs:                 12000,
		ResponseHeaderTimeoutMs:          21000,
		FirstByteTimeoutMs:               22000,
		RequestReadTimeoutMs:             33000,
		StreamIdleTimeoutMs:              70000,
		StreamPrecommitBufferMs:          4000,
		ExcludeFirstTokenLatencyFromRate: &excludeFirstTokenLatency,
		FailoverStatusCodes:              []int{403, 401, 401, 402, 429},
		Routing: ProxyRoutingSettingsInput{
			Strategy: "circuit-breaker",
			Params:   json.RawMessage(`{"failureThreshold":3,"baseCooldownMs":45000,"maxCooldownMs":90000,"backoffMultiplier":2,"halfOpenMaxRequests":1,"closeAfterSuccesses":1,"countPostCommitErrors":false,"rateLimitCooldownMs":12000}`),
		},
	})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if result.Settings.ConnectTimeoutMs != 12000 || result.Settings.ResponseHeaderTimeoutMs != 21000 || result.Settings.FirstByteTimeoutMs != 22000 || result.Settings.RequestReadTimeoutMs != 33000 || result.Settings.StreamIdleTimeoutMs != 70000 || result.Settings.StreamPrecommitBufferMs != 4000 || result.Settings.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("SaveProxySettings() = %#v", result.Settings)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Server.ConnectTimeoutMs != 12000 || cfg.Server.ResponseHeaderTimeoutMs != 21000 || cfg.Server.FirstByteTimeoutMs != 22000 || cfg.Server.RequestReadTimeoutMs != 33000 || cfg.Server.StreamIdleTimeoutMs != 70000 || cfg.Server.StreamPrecommitBufferMs != 4000 || cfg.Server.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("persisted server settings = %#v", cfg.Server)
	}
	if !reflect.DeepEqual(cfg.Server.FailoverStatusCodes, []int{401, 402, 403, 429}) {
		t.Fatalf("persisted failover status codes = %#v", cfg.Server.FailoverStatusCodes)
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

func TestSaveProxySettingsRejectsInvalidFailoverStatusCode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	_, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{FailoverStatusCodes: []int{600}})
	if err == nil || !strings.Contains(err.Error(), "server.failover_status_codes") {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
}

func TestSaveProxySettingsAllowsEmptyFailoverStatusCodes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{FailoverStatusCodes: []int{}})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if len(result.Settings.FailoverStatusCodes) != 0 {
		t.Fatalf("failover status codes = %#v, want empty", result.Settings.FailoverStatusCodes)
	}
}

func TestSaveProxySettingsDefaultsExcludeFirstTokenLatencyFromRate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if !result.Settings.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("exclude first token latency = false, want default true")
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.Server.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("persisted exclude first token latency = false, want default true")
	}
}

func TestSaveProxySettingsPreservesExcludedFirstTokenLatencyWhenOmitted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.ExcludeFirstTokenLatencyFromRate = false
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{ConnectTimeoutMs: 12000})
	if err != nil {
		t.Fatalf("SaveProxySettings() error = %v", err)
	}
	if result.Settings.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("exclude first token latency = true, want preserved false")
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if loaded.Server.ExcludeFirstTokenLatencyFromRate {
		t.Fatalf("persisted exclude first token latency = true, want preserved false")
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
		cfg.Server.StreamIdleTimeoutMs != config.DefaultStreamIdleTimeoutMs ||
		cfg.Server.StreamPrecommitBufferMs != config.DefaultStreamPrecommitBufferMs {
		t.Fatalf("persisted server settings = %#v", cfg.Server)
	}
}

func TestSaveProxySettingsRejectsNegativePrecommitBuffer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	_, err := svc.SaveProxySettings(context.Background(), ProxySettingsInput{StreamPrecommitBufferMs: -1})
	if err == nil || !strings.Contains(err.Error(), "server.stream_precommit_buffer_ms") {
		t.Fatalf("SaveProxySettings() error = %v, want precommit validation error", err)
	}
}

func TestExportImportConfigPreservesOptionalConfigBlocks(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Desktop = config.Desktop{Theme: "dark", Language: "zh-CN"}
	cfg.Providers = []config.Provider{{
		ID: "p1", BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-1", APIKeys: []string{"sk-2"},
		}},
	}}
	cfg.Aliases = []config.Alias{{Alias: "chat", Enabled: true, Targets: []config.Target{testDefaultTarget("p1", "gpt", true)}}}
	cfg.RequestRewriteRules = []config.RequestRewriteRule{{Name: "rule1", Alias: "chat", Enabled: true, Ops: []config.RequestRewriteOperation{{Op: config.RequestRewriteOpSet, Path: "$.temperature", Value: float64(0.2), ValueSet: true}}}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	exported, err := svc.ExportConfig(context.Background())
	if err != nil {
		t.Fatalf("ExportConfig() error = %v", err)
	}
	importPath := filepath.Join(t.TempDir(), "imported.json")
	importSvc := NewService(importPath)
	if _, err := importSvc.ImportConfig(context.Background(), ConfigImportInput{Content: exported.Content}); err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	imported, err := config.Load(importPath)
	if err != nil {
		t.Fatalf("config.Load(imported) error = %v", err)
	}
	if imported.Desktop.Language != "zh-CN" || imported.Desktop.Theme != "dark" {
		t.Fatalf("imported desktop = %#v", imported.Desktop)
	}
	if len(imported.Providers) != 1 || imported.Providers[0].ID != "p1" {
		t.Fatalf("imported providers = %#v", imported.Providers)
	}
	if g := imported.Providers[0].FindGroup(config.DefaultGroupID); g == nil || !reflect.DeepEqual(g.EffectiveAPIKeys(), []string{"sk-1", "sk-2"}) {
		t.Fatalf("imported default group keys = %#v", imported.Providers[0].Groups)
	}
	if len(imported.Aliases) != 1 || imported.Aliases[0].Alias != "chat" {
		t.Fatalf("imported aliases = %#v", imported.Aliases)
	}
	if len(imported.RequestRewriteRules) != 1 || imported.RequestRewriteRules[0].Name != "rule1" {
		t.Fatalf("imported rewrite rules = %#v", imported.RequestRewriteRules)
	}
}

func TestImportConfigRejectsPartialConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Desktop = config.Desktop{Theme: "dark", Language: "zh-CN"}
	cfg.Providers = []config.Provider{testProviderWithDefaultGroup("p1", "https://example.com/v1")}
	cfg.Aliases = []config.Alias{{Alias: "chat", Enabled: true, Targets: []config.Target{testDefaultTarget("p1", "gpt", true)}}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	_, err = svc.ImportConfig(context.Background(), ConfigImportInput{Content: `{"server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"}}`})
	if err == nil || !strings.Contains(err.Error(), "full ocswitch config") {
		t.Fatalf("ImportConfig(partial) error = %v, want full config error", err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(after) error = %v", err)
	}
	if len(after.Providers) != 1 || len(after.Aliases) != 1 || after.Desktop.Language != "zh-CN" {
		t.Fatalf("config changed after rejected import: providers=%#v aliases=%#v desktop=%#v", after.Providers, after.Aliases, after.Desktop)
	}
}

func TestImportConfigPreservesExplicitEmptyServerAPIKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	content := `{
  "server": { "host": "127.0.0.1", "port": 9982, "api_key": "" },
  "providers": [],
  "aliases": []
}`
	if _, err := svc.ImportConfig(context.Background(), ConfigImportInput{Content: content}); err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Server.APIKey != "" {
		t.Fatalf("server api_key = %q, want explicit empty", cfg.Server.APIKey)
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
		ID:      "relay",
		BaseURL: "https://old.example.com/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-old",
			Models: []string{"gpt-4.1"}, ModelsSource: "discovered",
		}},
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
	// Shared connection change keeps catalogs untrusted (no discovery on shared upsert).
	shared, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "relay",
		BaseURL: upstream.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if shared.Provider.BaseURL != upstream.URL+"/v1" {
		t.Fatalf("saved baseUrl = %q, want %q", shared.Provider.BaseURL, upstream.URL+"/v1")
	}
	if !containsWarning(shared.Warnings, "keeping existing model catalog as untrusted") {
		t.Fatalf("warnings %#v do not mention untrusted catalog", shared.Warnings)
	}

	// Key is group-owned; discovery is explicit via group refresh.
	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "relay",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Name:           config.DefaultGroupName,
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-new"},
		},
	}); err != nil {
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	result, err := svc.RefreshProviderGroupModels(context.Background(), ProviderGroupRefreshModelsInput{
		ProviderID: "relay",
		GroupID:    config.DefaultGroupID,
	})
	if err != nil {
		t.Fatalf("RefreshProviderGroupModels() error = %v", err)
	}
	if !containsWarning(result.Warnings, "model discovery failed") && !containsWarning(result.Warnings, "could not discover provider models") && !containsWarning(result.Warnings, "untrusted") {
		t.Fatalf("warnings %#v do not mention discovery failure / untrusted catalog", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("relay")
	if provider == nil {
		t.Fatal("provider relay not found after save")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing after save")
	}
	if group.ModelsSource != "" {
		t.Fatalf("group.ModelsSource = %q, want empty", group.ModelsSource)
	}
	if len(group.Models) != 1 || group.Models[0] != "gpt-4.1" {
		t.Fatalf("group.Models = %#v, want existing catalog kept", group.Models)
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
		ID:           "relay",
		BaseURL:      first.URL + "/v1",
		BaseURLs:     []string{first.URL + "/v1", second.URL + "/v1"},
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false),
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if got := strings.Join(result.Warnings, "; "); !strings.Contains(got, "auto-generated 2 alias(es)") {
		t.Fatalf("warnings = %#v, want auto-generated alias summary", result.Warnings)
	}
	if len(result.Provider.Groups) != 1 || !strings.Contains(strings.Join(result.Provider.Groups[0].Models, ","), "gpt-4.1") {
		t.Fatalf("groups = %#v", result.Provider.Groups)
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
	cfg.UpsertProvider(testProviderWithDefaultGroup("keep", "https://existing.example.com/v1"))
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
				"options": {"baseURL": "", "apiKey": "sk-bad"}
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
	if result.Imported != 1 || result.Skipped != 1 {
		t.Fatalf("ImportProviders() = %#v, want imported=1 skipped=1", result)
	}
	if !containsWarning(result.Warnings, `skip "keep"`) {
		t.Fatalf("warnings %#v do not mention duplicate provider", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if provider := reloaded.FindProvider("fresh"); provider == nil {
		t.Fatal("provider fresh not imported")
	}
}

func TestImportProvidersRejectsMaskedAPIKeyPlaceholder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ocswitch.json")
	sourcePath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(sourcePath, []byte(`{
		"provider": {
			"masked": {
				"npm": "@ai-sdk/openai",
				"options": {"baseURL": "https://masked.example.com/v1", "apiKey": "sk-***-masked"}
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	svc := NewService(path)
	if _, err := svc.ImportProviders(context.Background(), ProviderImportInput{SourcePath: sourcePath}); err == nil || !strings.Contains(err.Error(), "masked placeholder") {
		t.Fatalf("ImportProviders() error = %v, want masked placeholder", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.FindProvider("masked") != nil {
		t.Fatal("masked provider was persisted")
	}
}

func TestSetAliasTargetDisabledPersistsState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(testProviderWithDefaultGroup("relay", "https://relay.example.com/v1"))
	cfg.UpsertAlias(config.Alias{
		Alias:   "chat",
		Enabled: true,
		Targets: []config.Target{testDefaultTarget("relay", "gpt-4.1", true)},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	disabled, err := svc.SetAliasTargetDisabled(context.Background(), AliasTargetInput{
		Alias:    "chat",
		Provider: "relay",
		Group:    config.DefaultGroupID,
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
		Group:    config.DefaultGroupID,
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
	cfg.UpsertProvider(testProviderWithDefaultGroup("p1", "https://p1.example.com/v1"))
	cfg.UpsertProvider(testProviderWithDefaultGroup("p2", "https://p2.example.com/v1"))
	cfg.UpsertAlias(config.Alias{
		Alias:   "chat",
		Enabled: true,
		Targets: []config.Target{
			testDefaultTarget("p1", "up-1", true),
			testDefaultTarget("p2", "up-2", false),
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	view, err := svc.ReorderAliasTargets(context.Background(), AliasTargetReorderInput{
		Alias: "chat",
		Targets: []AliasTargetRefInput{
			{Provider: "p2", Group: config.DefaultGroupID, Model: "up-2"},
			{Provider: "p1", Group: config.DefaultGroupID, Model: "up-1"},
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

func TestAutoAliasTargetOverrides(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(testProviderWithDefaultGroup("p1", "https://p1.example.com/v1"))
	cfg.UpsertProvider(testProviderWithDefaultGroup("p2", "https://p2.example.com/v1"))
	cfg.SetProviderPriority([]string{"p2", "p1"})
	cfg.UpsertAlias(config.Alias{
		Alias:         "chat",
		Protocol:      config.ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: true,
		Targets: []config.Target{
			{Provider: "p1", Group: config.DefaultGroupID, Model: "chat", Enabled: true, AutoGenerated: true},
			{Provider: "p2", Group: config.DefaultGroupID, Model: "chat", Enabled: true, AutoGenerated: true},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	ctx := context.Background()
	disabled, err := svc.SetAliasTargetDisabled(ctx, AliasTargetInput{Alias: "chat", Provider: "p1", Group: config.DefaultGroupID, Model: "chat", Disabled: true})
	if err != nil {
		t.Fatalf("SetAliasTargetDisabled(auto) error = %v", err)
	}
	foundDisabledP1 := false
	for _, target := range disabled.Targets {
		if target.Provider == "p1" && !target.Enabled {
			foundDisabledP1 = true
		}
	}
	if !disabled.AutoGenerated || !foundDisabledP1 {
		t.Fatalf("disabled auto alias view = %#v", disabled)
	}

	reordered, err := svc.ReorderAliasTargets(ctx, AliasTargetReorderInput{Alias: "chat", Targets: []AliasTargetRefInput{{Provider: "p1", Group: config.DefaultGroupID, Model: "chat"}, {Provider: "p2", Group: config.DefaultGroupID, Model: "chat"}}})
	if err != nil {
		t.Fatalf("ReorderAliasTargets(auto) error = %v", err)
	}
	if reordered.TargetOrderMode != config.TargetOrderModeCustom || reordered.Targets[0].Provider != "p1" {
		t.Fatalf("reordered auto alias = %#v", reordered)
	}

	if _, err := svc.UnbindAliasTarget(ctx, AliasTargetInput{Alias: "chat", Provider: "p1", Group: config.DefaultGroupID, Model: "chat"}); err == nil {
		t.Fatal("UnbindAliasTarget(system auto target) error = nil, want rejection")
	}

	reset, err := svc.ResetAliasTargetOrder(ctx, AliasLockInput{Name: "chat"})
	if err != nil {
		t.Fatalf("ResetAliasTargetOrder(auto) error = %v", err)
	}
	if reset.TargetOrderMode != config.TargetOrderModeProviderPriority || reset.Targets[0].Provider != "p2" {
		t.Fatalf("reset auto alias = %#v", reset)
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

func TestRequestRewriteRulesUpsertListReorderAndPersist(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)

	first, err := svc.UpsertRequestRewriteRule(context.Background(), RequestRewriteRuleInput{
		Name:    "fast-tier",
		Alias:   "chat-fast",
		Enabled: true,
		Ops: []config.RequestRewriteOperation{
			{Op: config.RequestRewriteOpSet, Path: "$.service_tier", Value: "priority", ValueSet: true},
			{Op: config.RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true},
		},
	})
	if err != nil {
		t.Fatalf("UpsertRequestRewriteRule(first) error = %v", err)
	}
	if first.Name != "fast-tier" || !first.Enabled || requestRewriteOpValue(first.Ops, "$.service_tier") != "priority" {
		t.Fatalf("first rule view = %#v", first)
	}

	second, err := svc.UpsertRequestRewriteRule(context.Background(), RequestRewriteRuleInput{
		Name:  "strip-store",
		Alias: "chat-fast",
		ProviderGroups: []ProviderGroupSelectorInput{
			{Provider: "p1", Group: config.DefaultGroupID},
			{Provider: "p2", Group: config.DefaultGroupID},
		},
		Enabled:  true,
		Override: true,
		Ops:      []config.RequestRewriteOperation{{Op: config.RequestRewriteOpDelete, Path: "$.store"}},
	})
	if err != nil {
		t.Fatalf("UpsertRequestRewriteRule(second) error = %v", err)
	}
	if !second.Override || len(second.Ops) != 1 || second.Ops[0].Path != "$.store" || len(second.ProviderGroups) != 2 {
		t.Fatalf("second rule view = %#v", second)
	}

	rules, err := svc.ListRequestRewriteRules(context.Background())
	if err != nil {
		t.Fatalf("ListRequestRewriteRules() error = %v", err)
	}
	if len(rules) != 2 || rules[0].Name != "fast-tier" || rules[1].Name != "strip-store" {
		t.Fatalf("rules = %#v", rules)
	}

	disabled, err := svc.SetRequestRewriteRuleEnabled(context.Background(), RequestRewriteRuleStateInput{Name: "fast-tier", Enabled: false})
	if err != nil {
		t.Fatalf("SetRequestRewriteRuleEnabled() error = %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("disabled rule = %#v", disabled)
	}

	reordered, err := svc.ReorderRequestRewriteRules(context.Background(), RequestRewriteRuleReorderInput{Names: []string{"strip-store", "fast-tier"}})
	if err != nil {
		t.Fatalf("ReorderRequestRewriteRules() error = %v", err)
	}
	if len(reordered.Rules) != 2 || reordered.Rules[0].Name != "strip-store" || reordered.Rules[1].Enabled {
		t.Fatalf("reordered rules = %#v", reordered.Rules)
	}

	removed, err := svc.RemoveRequestRewriteRule(context.Background(), RequestRewriteRuleRemoveInput{Name: "strip-store"})
	if err != nil {
		t.Fatalf("RemoveRequestRewriteRule() error = %v", err)
	}
	if !removed.OK {
		t.Fatalf("removed = %#v", removed)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	persisted := reloaded.RequestRewriteRulesSnapshot()
	if len(persisted) != 1 || persisted[0].Name != "fast-tier" || persisted[0].Enabled {
		t.Fatalf("persisted rules = %#v", persisted)
	}
	if got := requestRewriteOpValue(persisted[0].Ops, "$.store"); got != false {
		t.Fatalf("persisted op store = %#v, want false", got)
	}
}

func requestRewriteOpValue(ops []config.RequestRewriteOperation, path string) any {
	for _, op := range ops {
		if op.Path == path {
			return op.Value
		}
	}
	return nil
}

func TestRequestRewriteRuleMutationReloadsRunningProxy(t *testing.T) {
	t.Parallel()

	var seenPayload map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenPayload = payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[]}`))
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	port := freePort(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Server.APIKey = config.DefaultLocalAPIKey
	cfg.Providers = []config.Provider{testProviderWithDefaultGroup("p1", upstream.URL+"/v1")}
	cfg.Aliases = []config.Alias{{
		Alias:   "chat",
		Enabled: true,
		Targets: []config.Target{testDefaultTarget("p1", "gpt-5.5", true)},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	if err := svc.StartProxy(context.Background()); err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	t.Cleanup(func() { _ = svc.StopProxy(context.Background()) })

	if _, err := svc.UpsertRequestRewriteRule(context.Background(), RequestRewriteRuleInput{
		Name:    "store-off",
		Alias:   "chat",
		Enabled: true,
		Ops:     []config.RequestRewriteOperation{{Op: config.RequestRewriteOpSet, Path: "$.store", Value: false, ValueSet: true}},
	}); err != nil {
		t.Fatalf("UpsertRequestRewriteRule() error = %v", err)
	}
	postProxyResponse(t, port, `{"model":"chat","stream":false}`)
	if got := seenPayload["store"]; got != false {
		t.Fatalf("store after upsert = %#v, want false", got)
	}

	if _, err := svc.SetRequestRewriteRuleEnabled(context.Background(), RequestRewriteRuleStateInput{Name: "store-off", Enabled: false}); err != nil {
		t.Fatalf("SetRequestRewriteRuleEnabled() error = %v", err)
	}
	postProxyResponse(t, port, `{"model":"chat","stream":false}`)
	if _, ok := seenPayload["store"]; ok {
		t.Fatalf("store after disable still present: %#v", seenPayload)
	}

	if _, err := svc.RemoveRequestRewriteRule(context.Background(), RequestRewriteRuleRemoveInput{Name: "store-off"}); err != nil {
		t.Fatalf("RemoveRequestRewriteRule() error = %v", err)
	}
}

func postProxyResponse(t *testing.T, port int, payload string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+itoa(port)+"/v1/responses", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestReconcileRuntimeSnapshotReportsDriftCategories(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{Host: "127.0.0.1", Port: 9982},
		Aliases: []config.Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []config.Target{testDefaultTarget("p1", "up-1", true)},
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
	assertDoctorIssueCodes(t, issues, "runtime_provider_protocol_mismatch", "opencode_catalog_drift", "opencode_default_model_unroutable", "opencode_small_model_unroutable")
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
	cfg.UpsertProvider(testProviderWithDefaultGroup("p1", "https://example.com/v1"))
	cfg.UpsertAlias(config.Alias{Alias: "gpt-5.4", Enabled: true, Targets: []config.Target{testDefaultTarget("p1", "up-1", true)}})
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

func TestQueryProviderHealthAggregatesAttempts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	primaryProvider := testProviderWithDefaultGroup("primary", "https://primary.example/v1", "sk-primary")
	primaryProvider.Name = "Primary"
	backupProvider := testProviderWithDefaultGroup("backup", "https://backup.example/v1", "sk-backup")
	backupProvider.Name = "Backup"
	cfg.Providers = []config.Provider{primaryProvider, backupProvider}
	cfg.Aliases = []config.Alias{
		{Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{
			testDefaultTarget("primary", "model-a", true),
			testDefaultTarget("backup", "model-b", true),
		}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	svc.traces = proxy.NewTraceStore(10)
	startedAt := time.Now().UTC()
	primaryCacheReadTokens := int64(10)
	backupCacheReadTokens := int64(5)
	traces := []proxy.RequestTrace{
		{
			ID:            1,
			StartedAt:     startedAt,
			Protocol:      config.ProtocolOpenAIResponses,
			Alias:         "chat",
			Success:       true,
			StatusCode:    http.StatusOK,
			FinalProvider: "primary",
			Usage:         proxy.TraceUsage{CacheReadTokens: &primaryCacheReadTokens},
			InputTokens:   10,
			OutputTokens:  20,
			FirstByteMs:   30,
			DurationMs:    100,
			AttemptCount:  1,
			Attempts:      []proxy.TraceAttempt{{Attempt: 1, Provider: "primary", Model: "model-a", Success: true, StatusCode: http.StatusOK, Result: "success", FirstByteMs: 30, DurationMs: 100}},
		},
		{
			ID:            2,
			StartedAt:     startedAt.Add(time.Second),
			Protocol:      config.ProtocolOpenAIResponses,
			Alias:         "chat",
			Success:       true,
			StatusCode:    http.StatusOK,
			FinalProvider: "backup",
			Usage:         proxy.TraceUsage{CacheReadTokens: &backupCacheReadTokens},
			InputTokens:   5,
			OutputTokens:  7,
			FirstByteMs:   40,
			DurationMs:    170,
			Failover:      true,
			AttemptCount:  2,
			Attempts: []proxy.TraceAttempt{
				{Attempt: 1, Provider: "primary", Model: "model-a", Retryable: true, StatusCode: http.StatusInternalServerError, Result: "retryable_failure", DurationMs: 50},
				{Attempt: 2, Provider: "backup", Model: "model-b", Success: true, StatusCode: http.StatusOK, Result: "success", FirstByteMs: 40, DurationMs: 120},
			},
		},
	}
	for _, trace := range traces {
		if err := svc.traces.Add(context.Background(), trace); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", trace.ID, err)
		}
	}

	result, err := svc.QueryProviderHealth(context.Background(), ProviderHealthInput{})
	if err != nil {
		t.Fatalf("QueryProviderHealth() error = %v", err)
	}
	if result.Summary.RequestCount != 2 || result.Summary.AttemptCount != 3 || result.Summary.Failover != 1 || result.Summary.RetryableFailures != 1 || result.Summary.CacheReadTokens != 15 || result.Summary.CacheHitRate != 0.5 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	primary := providerHealthByID(result.Providers, "primary")
	if primary == nil {
		t.Fatal("primary health missing")
	}
	if primary.Role != "primary" || primary.AttemptCount != 2 || primary.Success != 1 || primary.RetryableFailures != 1 || primary.Upstream5xx != 1 || primary.PrimaryAttempts != 2 || primary.CacheReadTokens != 10 || primary.CacheHitRate != 0.5 {
		t.Fatalf("primary = %#v", primary)
	}
	backup := providerHealthByID(result.Providers, "backup")
	if backup == nil {
		t.Fatal("backup health missing")
	}
	if backup.Role != "backup" || backup.AttemptCount != 1 || backup.Success != 1 || backup.FinalSuccess != 1 || backup.BackupAttempts != 1 || backup.TotalTokens != 12 || backup.CacheReadTokens != 5 || backup.CacheHitRate != 0.5 {
		t.Fatalf("backup = %#v", backup)
	}

	filtered, err := svc.QueryProviderHealth(context.Background(), ProviderHealthInput{Providers: []string{"backup"}})
	if err != nil {
		t.Fatalf("QueryProviderHealth(filtered) error = %v", err)
	}
	if len(filtered.Providers) != 1 || filtered.Providers[0].Provider != "backup" || filtered.Summary.RequestCount != 1 || filtered.Summary.AttemptCount != 1 || filtered.Summary.CacheReadTokens != 5 || filtered.Summary.CacheHitRate != 0.5 {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestQueryProviderHealthExcludesClientCanceledAttempts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	primaryProvider := testProviderWithDefaultGroup("primary", "https://primary.example/v1", "sk-primary")
	primaryProvider.Name = "Primary"
	backupProvider := testProviderWithDefaultGroup("backup", "https://backup.example/v1", "sk-backup")
	backupProvider.Name = "Backup"
	cfg.Providers = []config.Provider{primaryProvider, backupProvider}
	cfg.Aliases = []config.Alias{
		{Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{
			testDefaultTarget("primary", "model-a", true),
			testDefaultTarget("backup", "model-b", true),
		}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	svc.traces = proxy.NewTraceStore(10)
	startedAt := time.Now().UTC()
	traces := []proxy.RequestTrace{
		{
			ID:            1,
			StartedAt:     startedAt,
			Protocol:      config.ProtocolOpenAIResponses,
			Alias:         "chat",
			Error:         "client canceled: context canceled",
			FinalProvider: "primary",
			AttemptCount:  1,
			Attempts:      []proxy.TraceAttempt{{Attempt: 1, Provider: "primary", Model: "model-a", Result: proxy.TraceResultClientCanceled, FirstByteMs: 90, DurationMs: 900}},
		},
		{
			ID:            2,
			StartedAt:     startedAt.Add(time.Second),
			Protocol:      config.ProtocolOpenAIResponses,
			Alias:         "chat",
			Error:         "client canceled: broken pipe",
			FinalProvider: "backup",
			AttemptCount:  1,
			Attempts:      []proxy.TraceAttempt{{Attempt: 1, Provider: "backup", Model: "model-b", Result: proxy.TraceResultDownstreamCanceled, FirstByteMs: 80, DurationMs: 800}},
		},
		{
			ID:            3,
			StartedAt:     startedAt.Add(2 * time.Second),
			Protocol:      config.ProtocolOpenAIResponses,
			Alias:         "chat",
			Success:       true,
			StatusCode:    http.StatusOK,
			FinalProvider: "primary",
			FirstByteMs:   20,
			DurationMs:    50,
			AttemptCount:  1,
			Attempts:      []proxy.TraceAttempt{{Attempt: 1, Provider: "primary", Model: "model-a", Success: true, StatusCode: http.StatusOK, Result: "success", FirstByteMs: 20, DurationMs: 50}},
		},
	}
	for _, trace := range traces {
		if err := svc.traces.Add(context.Background(), trace); err != nil {
			t.Fatalf("traces.Add(%d) error = %v", trace.ID, err)
		}
	}

	result, err := svc.QueryProviderHealth(context.Background(), ProviderHealthInput{})
	if err != nil {
		t.Fatalf("QueryProviderHealth() error = %v", err)
	}
	if result.Summary.RequestCount != 1 || result.Summary.AttemptCount != 1 || result.Summary.Success != 1 || result.Summary.Failed != 0 || result.Summary.Failover != 0 || result.Summary.RetryableFailures != 0 || result.Summary.FirstByteP50Ms != 20 || result.Summary.DurationP50Ms != 50 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	primary := providerHealthByID(result.Providers, "primary")
	if primary == nil {
		t.Fatal("primary health missing")
	}
	if primary.RequestCount != 1 || primary.AttemptCount != 1 || primary.Success != 1 || primary.ObservedSuccessRate != 1 || primary.FirstByteP50Ms != 20 || primary.DurationP50Ms != 50 {
		t.Fatalf("primary = %#v", primary)
	}
	backup := providerHealthByID(result.Providers, "backup")
	if backup == nil {
		t.Fatal("backup health missing")
	}
	if backup.RequestCount != 0 || backup.AttemptCount != 0 || backup.TerminalFailures != 0 || backup.StreamErrors != 0 || backup.DurationP50Ms != 0 {
		t.Fatalf("backup = %#v", backup)
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

func providerHealthByID(items []ProviderHealthView, id string) *ProviderHealthView {
	for index := range items {
		if items[index].Provider == id {
			return &items[index]
		}
	}
	return nil
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

func modelsDiscoveryServer(t *testing.T, modelIDs ...string) *httptest.Server {
	t.Helper()
	type modelEntry struct {
		ID string `json:"id"`
	}
	data := make([]modelEntry, 0, len(modelIDs))
	for _, id := range modelIDs {
		data = append(data, modelEntry{ID: id})
	}
	payload, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func findAliasView(aliases []AliasView, name string) *AliasView {
	for i := range aliases {
		if aliases[i].Alias == name {
			return &aliases[i]
		}
	}
	return nil
}

func TestUpsertProvider_AutoGeneratesAliases(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "gpt-auto-a", "gpt-auto-b")
	defer upstream.Close()

	svc := NewService(path)
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:           "p-auto",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-test"),
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if !containsWarning(result.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v do not mention auto-generated aliases", result.Warnings)
	}
	if !result.Provider.AutoAliasEnabled {
		t.Fatal("create with nil AutoAliasEnabled should expose true")
	}
	if len(result.Provider.Groups) != 1 || !reflect.DeepEqual(result.Provider.Groups[0].Models, []string{"gpt-auto-a", "gpt-auto-b"}) {
		t.Fatalf("groups = %#v", result.Provider.Groups)
	}

	aliases, err := svc.ListAliases(context.Background())
	if err != nil {
		t.Fatalf("ListAliases() error = %v", err)
	}
	for _, name := range []string{"gpt-auto-a", "gpt-auto-b"} {
		alias := findAliasView(aliases, name)
		if alias == nil {
			t.Fatalf("alias %q not found in %#v", name, aliases)
		}
		if !alias.AutoGenerated {
			t.Fatalf("alias %q AutoGenerated = false, want true", name)
		}
		if len(alias.Targets) != 1 || alias.Targets[0].Provider != "p-auto" || alias.Targets[0].Model != name || !alias.Targets[0].AutoGenerated {
			t.Fatalf("alias %q targets = %#v", name, alias.Targets)
		}
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.FindAutoAlias("gpt-auto-a") == nil || reloaded.FindAutoAlias("gpt-auto-b") == nil {
		t.Fatalf("persisted auto aliases missing: %#v", reloaded.Aliases)
	}
}

func boolPtr(v bool) *bool { return &v }

// testDefaultGroupInput builds nested defaultGroup for UpsertProvider create tests.
// skipDiscover true stores an empty catalog (no /v1/models probe).
func testDefaultGroupInput(protocol string, skipDiscover bool, keys ...string) *ProviderGroupInput {
	g := &ProviderGroupInput{
		ID:       config.DefaultGroupID,
		Name:     config.DefaultGroupName,
		Protocol: protocol,
	}
	if len(keys) > 0 {
		g.APIKeysChanged = true
		g.APIKeys = append([]string(nil), keys...)
	}
	if skipDiscover {
		g.Models = []string{}
	}
	return g
}

// testDefaultProviderGroup builds a minimal default config.ProviderGroup for fixtures.
// Old single-group tests must materialize an explicit default group (v2 rejects empty groups).
func testDefaultProviderGroup(protocol string, apiKeys ...string) config.ProviderGroup {
	if strings.TrimSpace(protocol) == "" {
		protocol = config.ProtocolOpenAIResponses
	}
	g := config.ProviderGroup{
		ID:       config.DefaultGroupID,
		Name:     config.DefaultGroupName,
		Protocol: protocol,
	}
	if len(apiKeys) > 0 {
		keys := config.NormalizeProviderAPIKeys("", apiKeys)
		if len(keys) > 0 {
			g.APIKey = keys[0]
			if len(keys) > 1 {
				g.APIKeys = append([]string(nil), keys[1:]...)
			}
		}
	}
	return g
}

// testProviderWithDefaultGroup builds a single-group provider fixture.
func testProviderWithDefaultGroup(id, baseURL string, apiKeys ...string) config.Provider {
	return config.Provider{
		ID:      id,
		BaseURL: baseURL,
		Groups:  []config.ProviderGroup{testDefaultProviderGroup(config.ProtocolOpenAIResponses, apiKeys...)},
	}
}

// testDefaultTarget builds an alias target that explicitly references the default group.
// v2 ValidateForPersist rejects empty target groups; load also fails closed on blank group.
func testDefaultTarget(provider, model string, enabled bool) config.Target {
	return config.Target{
		Provider: provider,
		Group:    config.DefaultGroupID,
		Model:    model,
		Enabled:  enabled,
	}
}

func TestUpsertProvider_ProviderAutoAliasSwitch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "switch-model")
	defer upstream.Close()

	svc := NewService(path)
	ctx := context.Background()

	// Create with auto alias disabled: no generation warnings / aliases.
	offResult, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:               "p-switch",
		BaseURL:          upstream.URL + "/v1",
		AutoAliasEnabled: boolPtr(false),
		DefaultGroup:     testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-test"),
	})
	if err != nil {
		t.Fatalf("UpsertProvider(create off) error = %v", err)
	}
	if offResult.Provider.AutoAliasEnabled {
		t.Fatal("Provider.AutoAliasEnabled = true, want false")
	}
	if containsWarning(offResult.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v should not auto-generate when provider switch off", offResult.Warnings)
	}
	if aliases, err := svc.ListAliases(ctx); err != nil {
		t.Fatalf("ListAliases() error = %v", err)
	} else if findAliasView(aliases, "switch-model") != nil {
		t.Fatalf("alias should not exist when provider switch off: %#v", aliases)
	}

	// Update with nil keeps false (shared fields only; groups untouched).
	keepResult, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:      "p-switch",
		BaseURL: upstream.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("UpsertProvider(nil keep) error = %v", err)
	}
	if keepResult.Provider.AutoAliasEnabled {
		t.Fatal("nil update should preserve AutoAliasEnabled=false")
	}
	if containsWarning(keepResult.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v should stay quiet while provider switch off", keepResult.Warnings)
	}

	// Enable provider switch: generation resumes from existing group catalog.
	onResult, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:               "p-switch",
		BaseURL:          upstream.URL + "/v1",
		AutoAliasEnabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("UpsertProvider(enable) error = %v", err)
	}
	if !onResult.Provider.AutoAliasEnabled {
		t.Fatal("Provider.AutoAliasEnabled = false, want true")
	}
	if !containsWarning(onResult.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v should mention auto-generated after enable", onResult.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	p := reloaded.FindProvider("p-switch")
	if p == nil || !p.EffectiveAutoAliasEnabled() {
		t.Fatalf("persisted provider switch = %#v", p)
	}
	if reloaded.FindAutoAlias("switch-model") == nil {
		t.Fatal("expected auto alias after enabling provider switch")
	}
}

func TestGetSetAutoAliasSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	ctx := context.Background()

	got, err := svc.GetAutoAliasSettings(ctx)
	if err != nil {
		t.Fatalf("GetAutoAliasSettings() error = %v", err)
	}
	if !got.Enabled {
		t.Fatal("default global auto alias should be enabled")
	}

	set, err := svc.SetAutoAliasSettings(ctx, AutoAliasSettingsInput{Enabled: false})
	if err != nil {
		t.Fatalf("SetAutoAliasSettings(false) error = %v", err)
	}
	if set.Enabled {
		t.Fatal("SetAutoAliasSettings(false) returned enabled")
	}
	got, err = svc.GetAutoAliasSettings(ctx)
	if err != nil {
		t.Fatalf("GetAutoAliasSettings() after set error = %v", err)
	}
	if got.Enabled {
		t.Fatal("global auto alias still enabled after disable")
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.IsAutoAliasEnabled() {
		t.Fatal("persisted global auto alias still enabled")
	}

	// With global off, UpsertProvider should not auto-generate even if provider on.
	upstream := modelsDiscoveryServer(t, "global-off-model")
	defer upstream.Close()
	result, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:           "p-global-off",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-test"),
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if containsWarning(result.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v should not auto-generate when global off", result.Warnings)
	}
	if reloaded2, err := config.Load(path); err != nil {
		t.Fatalf("config.Load() error = %v", err)
	} else if reloaded2.FindAutoAlias("global-off-model") != nil {
		t.Fatal("auto alias created while global switch off")
	}
}

func TestUpgradeAutoAlias(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "lock-me")
	defer upstream.Close()

	svc := NewService(path)
	ctx := context.Background()
	if _, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:           "p-lock",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-test"),
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}

	view, err := svc.UpgradeAutoAlias(ctx, AliasLockInput{Name: "lock-me"})
	if err != nil {
		t.Fatalf("UpgradeAutoAlias() error = %v", err)
	}
	if !view.AutoGenerated || view.Locked {
		t.Fatalf("view flags = auto=%v locked=%v", view.AutoGenerated, view.Locked)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	alias := reloaded.FindAlias("lock-me")
	if alias == nil || alias.Locked || !alias.AutoGenerated || len(alias.Targets) != 1 {
		t.Fatalf("persisted alias = %#v", alias)
	}

	// Compatibility endpoint is a no-op: the alias remains auto-maintained and accepts future catalog targets.
	if _, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:           "p-lock-2",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-2"),
	}); err != nil {
		t.Fatalf("UpsertProvider(p-lock-2) error = %v", err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	auto := after.FindAlias("lock-me")
	if auto == nil || !auto.AutoGenerated || len(auto.Targets) != 2 {
		t.Fatalf("auto targets = %#v, want two auto-maintained targets", auto)
	}

	if _, err := svc.UpgradeAutoAlias(ctx, AliasLockInput{Name: "missing"}); err == nil {
		t.Fatal("UpgradeAutoAlias(missing) should fail")
	}
}

func TestUpsertProvider_MigratesCatalogManualAlias(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertAlias(config.Alias{
		Alias:         "manual-model",
		Protocol:      config.ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: false,
		Targets:       []config.Target{testDefaultTarget("other", "manual-model", true)},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	upstream := modelsDiscoveryServer(t, "manual-model", "extra-auto")
	defer upstream.Close()

	svc := NewService(path)
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:           "p-manual",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-test"),
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if !containsWarning(result.Warnings, "manual-model") {
		t.Fatalf("warnings %#v should include manual-model auto update", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	manual := reloaded.FindAlias("manual-model")
	if manual == nil {
		t.Fatal("catalog alias missing after UpsertProvider")
	}
	if !manual.AutoGenerated {
		t.Fatal("catalog alias was not absorbed as AutoGenerated")
	}
	if len(manual.Targets) != 2 || manual.Targets[0].Provider != "other" || manual.Targets[0].AutoGenerated || manual.Targets[1].Provider != "p-manual" || !manual.Targets[1].AutoGenerated {
		t.Fatalf("catalog targets = %#v, want custom target preserved plus system target", manual.Targets)
	}
	if reloaded.FindAutoAlias("manual-model") == nil {
		t.Fatal("FindAutoAlias should return absorbed catalog alias")
	}
	if auto := reloaded.FindAutoAlias("extra-auto"); auto == nil {
		t.Fatal("expected auto alias for extra-auto")
	}
}

func TestRemoveProvider_CleansAutoTargets(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "solo-model", "shared-model")
	defer upstream.Close()

	svc := NewService(path)
	ctx := context.Background()

	if _, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:           "p1",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-1"),
	}); err != nil {
		t.Fatalf("UpsertProvider(p1) error = %v", err)
	}
	if _, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
		ID:           "p2",
		BaseURL:      upstream.URL + "/v1",
		DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-2"),
	}); err != nil {
		t.Fatalf("UpsertProvider(p2) error = %v", err)
	}

	before, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if before.FindAutoAlias("solo-model") == nil || before.FindAutoAlias("shared-model") == nil {
		t.Fatalf("expected auto aliases before remove: %#v", before.Aliases)
	}

	if err := svc.RemoveProvider(ctx, "p1"); err != nil {
		t.Fatalf("RemoveProvider(p1) error = %v", err)
	}

	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() after remove error = %v", err)
	}
	if after.FindProvider("p1") != nil {
		t.Fatal("provider p1 still present")
	}
	// solo-model only had p1+p2; after p1 remove both providers still have it via shared discovery.
	// shared-model should keep p2 only.
	shared := after.FindAutoAlias("shared-model")
	if shared == nil {
		t.Fatal("shared-model auto alias missing")
	}
	for _, target := range shared.Targets {
		if target.Provider == "p1" {
			t.Fatalf("shared-model still references p1: %#v", shared.Targets)
		}
	}
	if len(shared.Targets) != 1 || shared.Targets[0].Provider != "p2" {
		t.Fatalf("shared-model targets = %#v, want only p2", shared.Targets)
	}
	solo := after.FindAutoAlias("solo-model")
	if solo == nil {
		t.Fatal("solo-model auto alias missing")
	}
	for _, target := range solo.Targets {
		if target.Provider == "p1" {
			t.Fatalf("solo-model still references p1: %#v", solo.Targets)
		}
	}
}

func TestRemoveProvider_PreservesManualAlias(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "p-manual",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-m", Models: []string{"manual-keep"},
		}},
	})
	cfg.UpsertAlias(config.Alias{
		Alias:         "manual-keep",
		Protocol:      config.ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: false,
		Targets:       []config.Target{testDefaultTarget("p-manual", "manual-keep", true)},
	})
	cfg.UpsertAlias(config.Alias{
		Alias:         "auto-only",
		Protocol:      config.ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: true,
		Targets: []config.Target{{
			Provider:      "p-manual",
			Group:         config.DefaultGroupID,
			Model:         "auto-only",
			Enabled:       true,
			AutoGenerated: true,
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	ctx := context.Background()
	// Convenience RemoveProvider blocks on protected manual targets (lifecycle contract).
	err = svc.RemoveProvider(ctx, "p-manual")
	if err == nil {
		t.Fatal("RemoveProvider() error = nil, want plan_not_executable for protected manual target")
	}
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "plan_not_executable" {
		t.Fatalf("RemoveProvider() error = %v, want OutcomeError plan_not_executable", err)
	}

	rev, snap, err := svc.SnapshotConfigRevision(ctx)
	if err != nil {
		t.Fatalf("SnapshotConfigRevision() error = %v", err)
	}
	planned, err := lifecycle.PlanProviderRemove(snap, string(rev), "p-manual", nil)
	if err != nil {
		t.Fatalf("PlanProviderRemove() error = %v", err)
	}
	var selections []lifecycle.Selection
	for _, choice := range planned.Plan.Choices {
		if choice.Code != lifecycle.ReasonProtectedTarget {
			continue
		}
		selections = append(selections, lifecycle.Selection{ChoiceID: choice.ID, OptionID: lifecycle.OptionRemoveTarget})
	}
	if len(selections) == 0 {
		t.Fatalf("expected protected_target choices, plan=%#v", planned.Plan)
	}
	// Explicit selection: remove protected target(s); system auto targets clean automatically.
	if err := svc.RemoveProviderWithPlan(ctx, "p-manual", selections); err != nil {
		t.Fatalf("RemoveProviderWithPlan() error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.FindProvider("p-manual") != nil {
		t.Fatal("provider p-manual still present")
	}
	manual := reloaded.FindAlias("manual-keep")
	if manual == nil {
		t.Fatal("manual alias was removed")
	}
	if !manual.AutoGenerated {
		t.Fatal("catalog-matching alias should remain AutoGenerated")
	}
	if len(manual.Targets) != 0 {
		t.Fatalf("manual targets = %#v, want empty after explicit remove_target", manual.Targets)
	}
	if reloaded.FindAutoAlias("auto-only") != nil {
		t.Fatal("emptied auto alias auto-only should be removed")
	}
}

func TestRefreshProviderModels_AutoGeneratesAliases(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "refresh-a", "refresh-b")
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "p-refresh",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-refresh",
		}},
		// No models yet — discovery happens on refresh.
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{ID: "p-refresh"})
	if err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}
	if !containsWarning(result.Warnings, "auto-generated") {
		t.Fatalf("warnings %#v do not mention auto-generated aliases", result.Warnings)
	}
	if len(result.Provider.Groups) != 1 || !reflect.DeepEqual(result.Provider.Groups[0].Models, []string{"refresh-a", "refresh-b"}) {
		t.Fatalf("groups = %#v", result.Provider.Groups)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	for _, name := range []string{"refresh-a", "refresh-b"} {
		alias := reloaded.FindAutoAlias(name)
		if alias == nil {
			t.Fatalf("auto alias %q missing", name)
		}
		if len(alias.Targets) != 1 || alias.Targets[0].Provider != "p-refresh" {
			t.Fatalf("alias %q targets = %#v", name, alias.Targets)
		}
	}
}

func TestSetGetProviderPriority(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	upstream := modelsDiscoveryServer(t, "prio-model")
	defer upstream.Close()

	svc := NewService(path)
	ctx := context.Background()

	for _, id := range []string{"p-low", "p-high", "p-mid"} {
		if _, err := svc.UpsertProvider(ctx, ProviderUpsertInput{
			ID:           id,
			BaseURL:      upstream.URL + "/v1",
			DefaultGroup: testDefaultGroupInput(config.ProtocolOpenAIResponses, false, "sk-"+id),
		}); err != nil {
			t.Fatalf("UpsertProvider(%s) error = %v", id, err)
		}
	}

	setResult, err := svc.SetProviderPriority(ctx, ProviderPriorityInput{
		OrderedIDs: []string{"p-high", "p-mid", "p-low"},
	})
	if err != nil {
		t.Fatalf("SetProviderPriority() error = %v", err)
	}
	wantOrder := []string{"p-high", "p-mid", "p-low"}
	if !reflect.DeepEqual(setResult.OrderedIDs, wantOrder) {
		t.Fatalf("SetProviderPriority OrderedIDs = %#v, want %#v", setResult.OrderedIDs, wantOrder)
	}

	getResult, err := svc.GetProviderPriority(ctx)
	if err != nil {
		t.Fatalf("GetProviderPriority() error = %v", err)
	}
	if !reflect.DeepEqual(getResult.OrderedIDs, wantOrder) {
		t.Fatalf("GetProviderPriority OrderedIDs = %#v, want %#v", getResult.OrderedIDs, wantOrder)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !reflect.DeepEqual(reloaded.ProviderPriorityOrder(), wantOrder) {
		t.Fatalf("persisted priority = %#v, want %#v", reloaded.ProviderPriorityOrder(), wantOrder)
	}
	alias := reloaded.FindAutoAlias("prio-model")
	if alias == nil {
		t.Fatal("prio-model auto alias missing")
	}
	gotProviders := make([]string, 0, len(alias.Targets))
	for _, target := range alias.Targets {
		gotProviders = append(gotProviders, target.Provider)
	}
	if !reflect.DeepEqual(gotProviders, wantOrder) {
		t.Fatalf("auto alias target order = %#v, want %#v", gotProviders, wantOrder)
	}
}

func TestRefreshProviderModelsWritesOnlyTargetGroup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer sk-premium" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"wrong key"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"premium-a"},{"id":"premium-b"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Headers: map[string]string{"X-Shared": "1"},
		Groups: []config.ProviderGroup{
			{
				ID:           config.DefaultGroupID,
				Name:         config.DefaultGroupName,
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-default",
				Models:       []string{"default-old"},
				ModelsSource: "discovered",
			},
			{
				ID:           "premium",
				Name:         "Premium",
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-premium",
				Models:       []string{"premium-old"},
				ModelsSource: "imported",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{
		ID:    "vendor-a",
		Group: "premium",
	})
	if err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}
	_ = result

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	premium := provider.FindGroup("premium")
	if defaultGroup == nil || premium == nil {
		t.Fatalf("groups missing: %#v", provider.Groups)
	}
	if !reflect.DeepEqual(defaultGroup.Models, []string{"default-old"}) || defaultGroup.ModelsSource != "discovered" {
		t.Fatalf("default group mutated: %#v", defaultGroup)
	}
	if !reflect.DeepEqual(premium.Models, []string{"premium-a", "premium-b"}) || premium.ModelsSource != "discovered" {
		t.Fatalf("premium group = %#v", premium)
	}
	if len(auths) != 1 || auths[0] != "Bearer sk-premium" {
		t.Fatalf("auths = %#v, want only premium key", auths)
	}
}

func TestRefreshProviderModelsEmptyGroupMapsToDefaultOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"default-new"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{
			{
				ID:       config.DefaultGroupID,
				Protocol: config.ProtocolOpenAIResponses,
				APIKey:   "sk-default",
				Models:   []string{"default-old"},
			},
			{
				ID:           "premium",
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-premium",
				Models:       []string{"premium-keep"},
				ModelsSource: "imported",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	if _, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{ID: "vendor-a"}); err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if got := provider.FindGroup(config.DefaultGroupID); got == nil || !reflect.DeepEqual(got.Models, []string{"default-new"}) {
		t.Fatalf("default group = %#v", got)
	}
	if got := provider.FindGroup("premium"); got == nil || !reflect.DeepEqual(got.Models, []string{"premium-keep"}) {
		t.Fatalf("premium group mutated: %#v", got)
	}
	if len(auths) != 1 || auths[0] != "Bearer sk-default" {
		t.Fatalf("auths = %#v", auths)
	}
}

func TestRefreshProviderModelsMissingGroupDoesNotFallback(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium"},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	_, err = svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{
		ID:    "vendor-a",
		Group: "missing-group",
	})
	if err == nil {
		t.Fatal("expected missing group error")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Fatalf("error = %q, want group not found", err.Error())
	}
}

func TestPingProviderBaseURLUsesExactGroupKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	var auth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if auth != "Bearer sk-premium" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"wrong key"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium"},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.PingProviderBaseURL(context.Background(), ProviderPingInput{
		ID:      "vendor-a",
		Group:   "premium",
		BaseURL: upstream.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("PingProviderBaseURL() error = %v", err)
	}
	if !result.Reachable {
		t.Fatalf("result = %#v", result)
	}
	if auth != "Bearer sk-premium" {
		t.Fatalf("Authorization = %q", auth)
	}
}

func TestPingProviderBaseURLEmptyProtocolKeepsGroupProtocol(t *testing.T) {
	t.Parallel()

	// Empty Protocol must not force DefaultProviderProtocol over the exact group.
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	var sawAnthropicAuth bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// anthropic-messages uses x-api-key; openai-responses uses Authorization Bearer.
		if got := r.Header.Get("x-api-key"); got == "sk-anthropic-premium" {
			sawAnthropicAuth = true
		}
		if r.Header.Get("Authorization") == "Bearer sk-anthropic-premium" {
			t.Fatalf("used openai-style Authorization; group protocol was overridden")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-model"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Protocol: config.ProtocolAnthropicMessages, APIKey: "sk-anthropic-premium"},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	result, err := svc.PingProviderBaseURL(context.Background(), ProviderPingInput{
		ID:      "vendor-a",
		Group:   "premium",
		BaseURL: upstream.URL + "/v1",
		// Protocol intentionally empty — must use premium group's anthropic-messages.
	})
	if err != nil {
		t.Fatalf("PingProviderBaseURL() error = %v", err)
	}
	if !result.Reachable {
		t.Fatalf("result = %#v", result)
	}
	if !sawAnthropicAuth {
		t.Fatal("expected anthropic-messages auth header from exact group protocol")
	}
}

func TestSelectCapabilityProbeTargetUsesExactGroup(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: []config.Provider{{
			ID:      "vendor-a",
			BaseURL: "https://example.com/v1",
			Headers: map[string]string{"X-Shared": "1"},
			Groups: []config.ProviderGroup{
				{
					ID:       config.DefaultGroupID,
					Protocol: config.ProtocolOpenAIResponses,
					APIKey:   "sk-default",
				},
				{
					ID:       "premium",
					Protocol: config.ProtocolAnthropicMessages,
					APIKey:   "sk-premium",
				},
			},
		}},
	}
	target, probe, ok := selectCapabilityProbeTarget(cfg, []config.Target{{
		Provider: "vendor-a",
		Group:    "premium",
		Model:    "claude-model",
		Enabled:  true,
	}})
	if !ok {
		t.Fatal("expected probe target")
	}
	if target.Group != "premium" {
		t.Fatalf("target.Group = %q", target.Group)
	}
	if probe.GroupID != "premium" {
		t.Fatalf("probe.GroupID = %q", probe.GroupID)
	}
	if probe.Protocol != config.ProtocolAnthropicMessages {
		t.Fatalf("probe.Protocol = %q", probe.Protocol)
	}
	if !reflect.DeepEqual(probe.APIKeys, []string{"sk-premium"}) {
		t.Fatalf("probe.APIKeys = %#v", probe.APIKeys)
	}
	if probe.Headers["X-Shared"] != "1" {
		t.Fatalf("probe.Headers = %#v", probe.Headers)
	}

	// Missing group must not fall back to default/first/same-protocol.
	_, _, ok = selectCapabilityProbeTarget(cfg, []config.Target{{
		Provider: "vendor-a",
		Group:    "missing",
		Model:    "m",
		Enabled:  true,
	}})
	if ok {
		t.Fatal("missing group must not select a fallback probe target")
	}
}

func TestProviderBaseURLChangeMarksAllGroupsUntrusted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://old.example.com/v1",
		Groups: []config.ProviderGroup{
			{
				ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
				Models: []string{"d1"}, ModelsSource: "discovered",
			},
			{
				ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium",
				Models: []string{"p1"}, ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer failing.Close()

	svc := NewService(path)
	// Shared connection change marks every group untrusted without touching group auth.
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "vendor-a",
		BaseURL: failing.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if !containsWarning(result.Warnings, "skip models") && !containsWarning(result.Warnings, "untrusted") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	for _, groupID := range []string{config.DefaultGroupID, "premium"} {
		g := provider.FindGroup(groupID)
		if g == nil {
			t.Fatalf("group %q missing", groupID)
		}
		if g.ModelsSource != "" {
			t.Fatalf("group %q ModelsSource = %q, want untrusted empty", groupID, g.ModelsSource)
		}
		if len(g.Models) == 0 {
			t.Fatalf("group %q models cleared, want preserved catalog", groupID)
		}
	}
}

func TestGroupKeyChangeMarksOnlyDefaultGroupUntrusted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{
				ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default-old",
				Models: []string{"d1"}, ModelsSource: "discovered",
			},
			{
				ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium",
				Models: []string{"p1"}, ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	// Key-only change is group-owned — UpdateProviderGroup must not touch siblings.
	svc := NewService(path)
	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-default-new"},
		},
	}); err != nil {
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	premium := provider.FindGroup("premium")
	if defaultGroup == nil || premium == nil {
		t.Fatalf("groups = %#v", provider.Groups)
	}
	if defaultGroup.ModelsSource != "" {
		t.Fatalf("default ModelsSource = %q, want empty after key change", defaultGroup.ModelsSource)
	}
	if !reflect.DeepEqual(defaultGroup.Models, []string{"d1"}) {
		t.Fatalf("default models = %#v", defaultGroup.Models)
	}
	if !reflect.DeepEqual(defaultGroup.EffectiveAPIKeys(), []string{"sk-default-new"}) {
		t.Fatalf("default keys = %#v", defaultGroup.EffectiveAPIKeys())
	}
	if premium.ModelsSource != "discovered" || !reflect.DeepEqual(premium.Models, []string{"p1"}) {
		t.Fatalf("premium group should stay trusted: %#v", premium)
	}
}
