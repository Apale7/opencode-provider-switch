package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	t.Run("save desktop prefs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/desktop-prefs", strings.NewReader(`{"launchAtLogin":true,"minimizeToTray":true,"notifications":true,"theme":"dark","language":"zh-CN"}`))
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
		if !loaded.Desktop.LaunchAtLogin || !loaded.Desktop.MinimizeToTray || !loaded.Desktop.Notifications || loaded.Desktop.Theme != "dark" || loaded.Desktop.Language != "zh-CN" {
			t.Fatalf("persisted desktop prefs = %#v", loaded.Desktop)
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
	return fmt.Errorf(f.message)
}
