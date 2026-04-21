package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestDesktopHTTPHandlerServesOverviewAndStaticApp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "demo",
		Name:    "Demo",
		BaseURL: "https://example.com/v1",
		APIKey:  "sk-demo-12345678",
		Models:  []string{"gpt-4.1-mini"},
	})
	cfg.UpsertAlias(config.Alias{
		Alias:       "chat",
		DisplayName: "Chat",
		Enabled:     true,
		Targets: []config.Target{{
			Provider: "demo",
			Model:    "gpt-4.1-mini",
			Enabled:  true,
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	instance := New(path)
	h, err := newHandler(instance, "test", "http://127.0.0.1:9982")
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}

	t.Run("overview api", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		var payload struct {
			Data struct {
				ProviderCount int      `json:"providerCount"`
				AliasCount    int      `json:"aliasCount"`
				Aliases       []string `json:"availableAliases"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.ProviderCount != 1 || payload.Data.AliasCount != 1 {
			t.Fatalf("unexpected counts: %#v", payload.Data)
		}
		if len(payload.Data.Aliases) != 1 || payload.Data.Aliases[0] != "chat" {
			t.Fatalf("unexpected aliases: %#v", payload.Data.Aliases)
		}
	})

	t.Run("request traces api", func(t *testing.T) {
		instance.Service().ListRequestTraces(context.Background(), 10)
		req := httptest.NewRequest(http.MethodGet, "/api/proxy/traces", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		var payload struct {
			Data []app.RequestTrace `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if len(payload.Data) != 0 {
			t.Fatalf("traces = %#v, want empty list", payload.Data)
		}
	})

	t.Run("proxy settings api", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/proxy/settings", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		var payload struct {
			Data app.ProxySettingsView `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.ConnectTimeoutMs != config.DefaultConnectTimeoutMs || payload.Data.FirstByteTimeoutMs != config.DefaultFirstByteTimeoutMs {
			t.Fatalf("proxy settings = %#v", payload.Data)
		}
	})

	t.Run("save proxy settings", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy/settings", strings.NewReader(`{"connectTimeoutMs":12000,"responseHeaderTimeoutMs":21000,"firstByteTimeoutMs":22000,"requestReadTimeoutMs":33000,"streamIdleTimeoutMs":70000}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		if loaded.Server.ConnectTimeoutMs != 12000 || loaded.Server.ResponseHeaderTimeoutMs != 21000 || loaded.Server.FirstByteTimeoutMs != 22000 || loaded.Server.RequestReadTimeoutMs != 33000 || loaded.Server.StreamIdleTimeoutMs != 70000 {
			t.Fatalf("persisted server settings = %#v", loaded.Server)
		}
	})

	t.Run("save proxy settings normalizes non-positive values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/proxy/settings", strings.NewReader(`{"connectTimeoutMs":0,"responseHeaderTimeoutMs":-1,"firstByteTimeoutMs":0,"requestReadTimeoutMs":-50,"streamIdleTimeoutMs":0}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data app.ProxySettingsSaveResult `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.Settings.ConnectTimeoutMs != config.DefaultConnectTimeoutMs ||
			payload.Data.Settings.ResponseHeaderTimeoutMs != config.DefaultResponseHeaderTimeoutMs ||
			payload.Data.Settings.FirstByteTimeoutMs != config.DefaultFirstByteTimeoutMs ||
			payload.Data.Settings.RequestReadTimeoutMs != config.DefaultRequestReadTimeoutMs ||
			payload.Data.Settings.StreamIdleTimeoutMs != config.DefaultStreamIdleTimeoutMs {
			t.Fatalf("proxy settings payload = %#v", payload.Data.Settings)
		}
	})

	t.Run("save desktop prefs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/desktop-prefs", strings.NewReader(`{"launchAtLogin":true,"autoStartProxy":true,"minimizeToTray":true,"notifications":true,"theme":"dark","language":"zh-CN"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		if !loaded.Desktop.LaunchAtLogin || !loaded.Desktop.AutoStartProxy || !loaded.Desktop.MinimizeToTray || !loaded.Desktop.Notifications || loaded.Desktop.Theme != "dark" || loaded.Desktop.Language != "zh-CN" {
			t.Fatalf("persisted desktop prefs = %#v", loaded.Desktop)
		}
	})

	t.Run("startup auto starts proxy when enabled", func(t *testing.T) {
		port := freePort(t)
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		cfg.Server.Host = "127.0.0.1"
		cfg.Server.Port = port
		cfg.Server.APIKey = config.DefaultLocalAPIKey
		cfg.Desktop.AutoStartProxy = true
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save() error = %v", err)
		}

		tray := &spyTray{}
		notify := &spyNotifier{}
		originalTray := instance.tray
		originalNotify := instance.notify
		defer func() {
			instance.tray = originalTray
			instance.notify = originalNotify
			_ = instance.Service().StopProxy(context.Background())
		}()
		instance.tray = tray
		instance.notify = notify

		instance.Startup(context.Background())

		status, err := instance.Service().GetProxyStatus(context.Background())
		if err != nil {
			t.Fatalf("GetProxyStatus() error = %v", err)
		}
		if !status.Running {
			t.Fatalf("status = %#v, want running", status)
		}
		if tray.refreshCalls != 1 {
			t.Fatalf("tray refreshCalls = %d, want 1", tray.refreshCalls)
		}
		if len(notify.sends) != 0 {
			t.Fatalf("startup notifications = %#v, want none", notify.sends)
		}
	})

	t.Run("startup auto start surfaces failure without success toast", func(t *testing.T) {
		port := freePort(t)
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
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
		cfg.Desktop.AutoStartProxy = true
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save() error = %v", err)
		}

		tray := &spyTray{}
		notify := &spyNotifier{}
		originalTray := instance.tray
		originalNotify := instance.notify
		defer func() {
			instance.tray = originalTray
			instance.notify = originalNotify
			_ = instance.Service().StopProxy(context.Background())
		}()
		instance.tray = tray
		instance.notify = notify

		instance.Startup(context.Background())

		assertEventually(t, func() bool {
			return len(notify.sends) == 1
		})

		status, err := instance.Service().GetProxyStatus(context.Background())
		if err != nil {
			t.Fatalf("GetProxyStatus() error = %v", err)
		}
		if status.Running {
			t.Fatalf("status = %#v, want stopped after failure", status)
		}
		if status.LastError == "" {
			t.Fatalf("status = %#v, want last error", status)
		}
		if len(notify.sends) != 1 {
			t.Fatalf("notifications = %#v, want 1 failure notice", notify.sends)
		}
		if notify.sends[0].Title != "Proxy failed to start" {
			t.Fatalf("notification = %#v, want failure title", notify.sends[0])
		}
		if !strings.Contains(strings.ToLower(notify.sends[0].Body), "bind") {
			t.Fatalf("notification = %#v, want bind error", notify.sends[0])
		}
		if tray.refreshCalls < 2 {
			t.Fatalf("tray refreshCalls = %d, want refresh after failure", tray.refreshCalls)
		}
	})

	t.Run("proxy start api surfaces bind failure", func(t *testing.T) {
		port := freePort(t)
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
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
		cfg.Desktop.AutoStartProxy = false
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/proxy/start", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
		}
		if !strings.Contains(strings.ToLower(resp.Body.String()), "bind") {
			t.Fatalf("body = %q, want bind error", resp.Body.String())
		}
	})

	t.Run("save desktop prefs keeps success with integration warning", func(t *testing.T) {
		originalAuto := instance.auto
		instance.auto = failingAutoStart{message: "startup folder unavailable"}
		defer func() {
			instance.auto = originalAuto
		}()

		req := httptest.NewRequest(http.MethodPost, "/api/desktop-prefs", strings.NewReader(`{"launchAtLogin":true,"minimizeToTray":false,"notifications":false}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data struct {
				Prefs struct {
					LaunchAtLogin bool `json:"launchAtLogin"`
				} `json:"prefs"`
				Warnings []string `json:"warnings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if !payload.Data.Prefs.LaunchAtLogin {
			t.Fatalf("prefs payload = %#v", payload.Data.Prefs)
		}
		if len(payload.Data.Warnings) != 1 || !strings.Contains(payload.Data.Warnings[0], "launch-at-login integration") {
			t.Fatalf("warnings = %#v, want integration warning", payload.Data.Warnings)
		}

		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		if !loaded.Desktop.LaunchAtLogin {
			t.Fatalf("persisted desktop prefs = %#v, want saved despite warning", loaded.Desktop)
		}
	})

	t.Run("import config syncs desktop integrations and reports warning", func(t *testing.T) {
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		cfg.UpsertProvider(config.Provider{
			ID:      "demo",
			Name:    "Demo",
			BaseURL: "https://example.com/v1",
			APIKey:  "sk-demo-12345678",
			Models:  []string{"gpt-4.1-mini"},
		})
		cfg.UpsertAlias(config.Alias{
			Alias:       "chat",
			DisplayName: "Chat",
			Enabled:     true,
			Targets: []config.Target{{
				Provider: "demo",
				Model:    "gpt-4.1-mini",
				Enabled:  true,
			}},
		})
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save() error = %v", err)
		}

		originalAuto := instance.auto
		originalTray := instance.tray
		originalNotify := instance.notify
		auto := failingAutoStart{message: "startup folder unavailable"}
		tray := &spyTray{}
		notify := &spyNotifier{}
		instance.auto = auto
		instance.tray = tray
		instance.notify = notify
		defer func() {
			instance.auto = originalAuto
			instance.tray = originalTray
			instance.notify = originalNotify
		}()

		req := httptest.NewRequest(http.MethodPost, "/api/config/import", strings.NewReader(`{
			"content":"{\"server\":{\"host\":\"127.0.0.1\",\"port\":9982,\"api_key\":\"ocswitch-local\"},\"desktop\":{\"launch_at_login\":true,\"minimize_to_tray\":true,\"notifications\":true,\"theme\":\"dark\",\"language\":\"zh-CN\"},\"providers\":[{\"id\":\"demo\",\"name\":\"Demo\",\"base_url\":\"https://example.com/v1\",\"api_key\":\"sk-demo-12345678\",\"models\":[\"gpt-4.1-mini\"]}],\"aliases\":[{\"alias\":\"chat\",\"display_name\":\"Chat\",\"enabled\":true,\"targets\":[{\"provider\":\"demo\",\"model\":\"gpt-4.1-mini\",\"enabled\":true}]}]}"
		}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data struct {
				ConfigPath string   `json:"configPath"`
				Warnings   []string `json:"warnings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.ConfigPath == "" {
			t.Fatalf("configPath is empty: %#v", payload.Data)
		}
		if len(payload.Data.Warnings) != 1 || !strings.Contains(payload.Data.Warnings[0], "sync desktop integrations") {
			t.Fatalf("warnings = %#v, want desktop sync warning", payload.Data.Warnings)
		}
		if tray.syncCalls != 1 {
			t.Fatalf("tray syncCalls = %d, want 1", tray.syncCalls)
		}
		if tray.refreshCalls != 1 {
			t.Fatalf("tray refreshCalls = %d, want 1", tray.refreshCalls)
		}
		if !notify.lastPrefs.Notifications {
			t.Fatalf("notify prefs = %#v, want notifications enabled", notify.lastPrefs)
		}

		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		if !loaded.Desktop.LaunchAtLogin || !loaded.Desktop.MinimizeToTray || !loaded.Desktop.Notifications {
			t.Fatalf("persisted desktop prefs = %#v", loaded.Desktop)
		}
	})

	t.Run("provider save exposes warnings", func(t *testing.T) {
		providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"bad upstream"}`))
		}))
		defer providerServer.Close()

		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		cfg.UpsertProvider(config.Provider{
			ID:           "warnme",
			BaseURL:      "https://prior.example.com/v1",
			APIKey:       "sk-prior",
			Models:       []string{"gpt-4.1"},
			ModelsSource: "discovered",
		})
		if err := cfg.Save(); err != nil {
			t.Fatalf("cfg.Save() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(`{"id":"warnme","baseUrl":"`+providerServer.URL+`/v1","apiKey":"sk-new"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data struct {
				Provider struct {
					ID string `json:"id"`
				} `json:"provider"`
				Warnings []string `json:"warnings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.Provider.ID != "warnme" {
			t.Fatalf("saved provider = %#v", payload.Data.Provider)
		}
		if len(payload.Data.Warnings) == 0 {
			t.Fatalf("warnings = %#v, want non-empty", payload.Data.Warnings)
		}
	})

	t.Run("alias target state route toggles enabled flag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/aliases/state", strings.NewReader(`{"alias":"chat","provider":"demo","model":"gpt-4.1-mini","disabled":true}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("disable status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data struct {
				AvailableTargetCount int `json:"availableTargetCount"`
				Targets              []struct {
					Enabled bool `json:"enabled"`
				} `json:"targets"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.AvailableTargetCount != 0 || len(payload.Data.Targets) != 1 || payload.Data.Targets[0].Enabled {
			t.Fatalf("disabled payload = %#v", payload.Data)
		}

		req = httptest.NewRequest(http.MethodPost, "/api/aliases/state", strings.NewReader(`{"alias":"chat","provider":"demo","model":"gpt-4.1-mini","disabled":false}`))
		req.Header.Set("Content-Type", "application/json")
		resp = httptest.NewRecorder()
		h.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("enable status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}

		loaded, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		alias := loaded.FindAlias("chat")
		if alias == nil || len(alias.Targets) != 1 || !alias.Targets[0].Enabled {
			t.Fatalf("persisted alias = %#v", alias)
		}
	})

	t.Run("provider import exposes warnings", func(t *testing.T) {
		sourcePath := filepath.Join(t.TempDir(), "opencode.json")
		if err := os.WriteFile(sourcePath, []byte(`{
			"provider": {
				"demo": {
					"npm": "@ai-sdk/openai",
					"options": {"baseURL": "https://duplicate.example.com/v1", "apiKey": "sk-dup"}
				},
				"broken": {
					"npm": "@ai-sdk/openai",
					"options": {"baseURL": "https://broken.example.com", "apiKey": "sk-bad"}
				}
			}
		}`), 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/providers/import", strings.NewReader(`{"sourcePath":"`+strings.ReplaceAll(sourcePath, "\\", "\\\\")+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
		}
		var payload struct {
			Data struct {
				Imported int      `json:"imported"`
				Skipped  int      `json:"skipped"`
				Warnings []string `json:"warnings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.Data.Imported != 0 || payload.Data.Skipped != 2 {
			t.Fatalf("import payload = %#v", payload.Data)
		}
		if len(payload.Data.Warnings) != 2 {
			t.Fatalf("warnings = %#v, want 2 entries", payload.Data.Warnings)
		}
	})

	t.Run("serves app shell", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		if !strings.Contains(resp.Body.String(), "ocswitch desktop") {
			t.Fatalf("unexpected body = %q", resp.Body.String())
		}
	})
}

type failingAutoStart struct {
	message string
}

func (f failingAutoStart) Attach(_ context.Context) {}

func (f failingAutoStart) Detach() {}

func (f failingAutoStart) Sync(_ context.Context, _ app.DesktopPrefsView) error {
	return errors.New(f.message)
}

type spyTray struct {
	syncCalls    int
	refreshCalls int
	lastPrefs    app.DesktopPrefsView
}

func (s *spyTray) Attach(_ context.Context) {}

func (s *spyTray) Detach() {}

func (s *spyTray) Sync(_ context.Context, prefs app.DesktopPrefsView) {
	s.syncCalls++
	s.lastPrefs = prefs
}

func (s *spyTray) RefreshProxyStatus(_ context.Context) {
	s.refreshCalls++
}

func (s *spyTray) BeforeClose(_ context.Context) (bool, error) {
	return false, nil
}

type spyNotifier struct {
	syncCalls int
	lastPrefs app.DesktopPrefsView
	sends     []notifyCall
}

func (s *spyNotifier) Attach(_ context.Context) {}

func (s *spyNotifier) Detach() {}

func (s *spyNotifier) Sync(_ context.Context, prefs app.DesktopPrefsView) {
	s.syncCalls++
	s.lastPrefs = prefs
}

func (s *spyNotifier) Send(_ context.Context, title string, body string) error {
	s.sends = append(s.sends, notifyCall{Title: title, Body: body})
	return nil
}

type notifyCall struct {
	Title string
	Body  string
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
