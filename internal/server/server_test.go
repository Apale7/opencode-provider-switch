package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
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

func writeBootstrapLegacyConfig(t *testing.T, withAdminKey bool) (path string, legacy []byte) {
	t.Helper()
	if withAdminKey {
		legacy = []byte(`{
  "server": {"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "admin": {"host":"127.0.0.1","port":9983,"api_key":"existing-admin-key"},
  "providers": [{
    "id":"vendor-a",
    "protocol":"openai-responses",
    "base_url":"https://api.example.com/v1",
    "api_key":"sk-primary",
    "models":["model-a"]
  }],
  "aliases": []
}`)
	} else {
		legacy = []byte(`{
  "server": {"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "providers": [{
    "id":"vendor-a",
    "protocol":"openai-responses",
    "base_url":"https://api.example.com/v1",
    "api_key":"sk-primary",
    "models":["model-a"]
  }],
  "aliases": []
}`)
	}
	path = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	return path, legacy
}

func TestEnsureAdminConfigBacksUpLegacyBeforeSchemaV2Persist(t *testing.T) {
	path, legacy := writeBootstrapLegacyConfig(t, false)

	cfg, generated, err := ensureAdminConfig(RunOptions{ConfigPath: path, Host: "0.0.0.0", Port: 19001})
	if err != nil {
		t.Fatalf("ensureAdminConfig() error = %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true for empty admin key")
	}
	if strings.TrimSpace(cfg.Admin.APIKey) == "" {
		t.Fatal("admin api key empty after bootstrap")
	}
	if cfg.Admin.Host != "0.0.0.0" || cfg.Admin.Port != 19001 {
		t.Fatalf("admin bind = %#v, want host=0.0.0.0 port=19001", cfg.Admin)
	}

	bak, err := os.ReadFile(config.BackupPathForConfig(path))
	if err != nil {
		t.Fatalf("read legacy backup: %v", err)
	}
	if !bytes.Equal(bak, legacy) {
		t.Fatal("legacy backup does not match original config bytes")
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !bytes.Contains(saved, []byte(`"schema_version": 2`)) {
		t.Fatalf("saved config is not schema v2: %s", saved)
	}
	if !bytes.Contains(saved, []byte(cfg.Admin.APIKey)) {
		t.Fatal("saved config missing generated admin api key")
	}
	if !bytes.Contains(saved, []byte(`"host": "0.0.0.0"`)) || !bytes.Contains(saved, []byte(`"port": 19001`)) {
		t.Fatalf("saved config missing host/port overrides: %s", saved)
	}
}

func TestEnsureAdminConfigBackupFailurePreservesLegacyFile(t *testing.T) {
	path, legacy := writeBootstrapLegacyConfig(t, false)
	bakPath := config.BackupPathForConfig(path)
	if err := os.Mkdir(bakPath, 0o700); err != nil {
		t.Fatalf("create blocking bak directory: %v", err)
	}

	_, _, err := ensureAdminConfig(RunOptions{ConfigPath: path})
	if err == nil {
		t.Fatal("ensureAdminConfig() error = nil, want backup failure")
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read config after failed bootstrap: %v", readErr)
	}
	if !bytes.Equal(after, legacy) {
		t.Fatal("legacy config changed after backup failure")
	}
}

func TestEnsureAdminConfigPreservesExistingBak(t *testing.T) {
	path, _ := writeBootstrapLegacyConfig(t, false)
	bakPath := config.BackupPathForConfig(path)
	first := []byte("bootstrap-first-backup-must-be-kept")
	if err := os.WriteFile(bakPath, first, 0o600); err != nil {
		t.Fatalf("seed existing bak: %v", err)
	}

	cfg, generated, err := ensureAdminConfig(RunOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("ensureAdminConfig() error = %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true")
	}
	if strings.TrimSpace(cfg.Admin.APIKey) == "" {
		t.Fatal("admin api key empty")
	}

	got, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("read bak: %v", err)
	}
	if !bytes.Equal(got, first) {
		t.Fatalf("existing .bak was overwritten: got %q", got)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !bytes.Contains(saved, []byte(`"schema_version": 2`)) {
		t.Fatalf("saved config is not schema v2: %s", saved)
	}
}

func TestEnsureAdminConfigHostPortOverrideBacksUpLegacyWithExistingKey(t *testing.T) {
	path, legacy := writeBootstrapLegacyConfig(t, true)

	cfg, generated, err := ensureAdminConfig(RunOptions{ConfigPath: path, Host: "127.0.0.1", Port: 19002})
	if err != nil {
		t.Fatalf("ensureAdminConfig() error = %v", err)
	}
	if generated {
		t.Fatal("generated = true, want false when admin key already exists")
	}
	if cfg.Admin.APIKey != "existing-admin-key" {
		t.Fatalf("admin key = %q, want existing-admin-key", cfg.Admin.APIKey)
	}
	if cfg.Admin.Port != 19002 {
		t.Fatalf("admin port = %d, want 19002", cfg.Admin.Port)
	}

	bak, err := os.ReadFile(config.BackupPathForConfig(path))
	if err != nil {
		t.Fatalf("read legacy backup: %v", err)
	}
	if !bytes.Equal(bak, legacy) {
		t.Fatal("legacy backup does not match original config bytes")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !bytes.Contains(saved, []byte(`"schema_version": 2`)) {
		t.Fatalf("saved config is not schema v2: %s", saved)
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
