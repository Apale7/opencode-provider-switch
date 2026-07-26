package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeLegacyConfigForBackup(t *testing.T) (path string, legacy []byte) {
	t.Helper()
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
	path = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	return path, legacy
}

func TestBackupLegacyConfigFileCreatesFirstBak(t *testing.T) {
	t.Parallel()

	path, legacy := writeLegacyConfigForBackup(t)
	if err := BackupLegacyConfigFile(path); err != nil {
		t.Fatalf("BackupLegacyConfigFile() error = %v", err)
	}
	bak := BackupPathForConfig(path)
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(got, legacy) {
		t.Fatalf("backup content mismatch")
	}
}

func TestBackupLegacyConfigFilePreservesExistingBak(t *testing.T) {
	t.Parallel()

	path, legacy := writeLegacyConfigForBackup(t)
	bak := BackupPathForConfig(path)
	first := []byte("first-legacy-backup-must-be-kept")
	if err := os.WriteFile(bak, first, 0o600); err != nil {
		t.Fatalf("seed existing bak: %v", err)
	}

	if err := BackupLegacyConfigFile(path); err != nil {
		t.Fatalf("BackupLegacyConfigFile() error = %v", err)
	}
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(got, first) {
		t.Fatalf("existing .bak was overwritten: got %q", got)
	}

	// Original config file must remain untouched by backup itself.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(after, legacy) {
		t.Fatal("config file changed during backup")
	}
}

func TestBackupLegacyConfigFileFailsWhenBakIsDirectory(t *testing.T) {
	t.Parallel()

	path, legacy := writeLegacyConfigForBackup(t)
	bak := BackupPathForConfig(path)
	if err := os.Mkdir(bak, 0o700); err != nil {
		t.Fatalf("create blocking bak directory: %v", err)
	}

	if err := BackupLegacyConfigFile(path); err == nil {
		t.Fatal("BackupLegacyConfigFile() error = nil, want directory error")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after failed backup: %v", err)
	}
	if !bytes.Equal(after, legacy) {
		t.Fatal("config file changed after backup failure")
	}
}

func TestBackupLegacyConfigFileSkipsSchemaV2(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	v2 := []byte(`{"schema_version":2,"server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},"providers":[],"aliases":[]}`)
	if err := os.WriteFile(path, v2, 0o600); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if err := BackupLegacyConfigFile(path); err != nil {
		t.Fatalf("BackupLegacyConfigFile() error = %v", err)
	}
	if _, err := os.Stat(BackupPathForConfig(path)); !os.IsNotExist(err) {
		t.Fatalf("schema v2 must not create .bak, stat err = %v", err)
	}
}
