package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestEnsureAdminConfigGeneratesPlaintextToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, generated, err := ensureAdminConfig(RunOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("ensureAdminConfig() error = %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true")
	}
	if strings.TrimSpace(cfg.Admin.APIKey) == "" {
		t.Fatalf("admin api key empty: %#v", cfg.Admin)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if loaded.Admin.APIKey != cfg.Admin.APIKey {
		t.Fatalf("persisted admin key mismatch")
	}

	_, generated, err = ensureAdminConfig(RunOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("ensureAdminConfig() second error = %v", err)
	}
	if generated {
		t.Fatal("generated = true on existing key")
	}
}

func TestAdminAuthRequiresBearerToken(t *testing.T) {
	auth := adminAuth("secret-token")

	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	resp := httptest.NewRecorder()
	if auth(resp, req) {
		t.Fatal("auth accepted missing token")
	}
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp = httptest.NewRecorder()
	if !auth(resp, req) {
		t.Fatal("auth rejected valid bearer token")
	}
}
