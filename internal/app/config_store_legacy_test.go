package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/configstore"
)

func TestCommitConfigBacksUpLegacyFileBeforeFirstPersist(t *testing.T) {
	path, legacy := writeLegacyConfigFixture(t)
	svc := NewService(path)

	_, err := svc.commitConfig(context.Background(), "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.Server.Port = 9983
		cfg.Providers[0].Groups[0].ModelsSource = ""
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		t.Fatalf("commit config: %v", err)
	}

	backup, err := os.ReadFile(config.BackupPathForConfig(path))
	if err != nil {
		t.Fatalf("read legacy backup: %v", err)
	}
	if !bytes.Equal(backup, legacy) {
		t.Fatal("legacy backup does not match the original config")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !bytes.Contains(saved, []byte(`"schema_version": 2`)) {
		t.Fatalf("saved config is not schema v2: %s", saved)
	}
}

func TestCommitConfigBackupFailurePreservesLegacyFile(t *testing.T) {
	path, legacy := writeLegacyConfigFixture(t)
	backupPath := config.BackupPathForConfig(path)
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		t.Fatalf("create blocking backup directory: %v", err)
	}
	svc := NewService(path)

	_, err := svc.commitConfig(context.Background(), "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.Server.Port = 9983
		cfg.Providers[0].Groups[0].ModelsSource = ""
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err == nil {
		t.Fatal("commit config succeeded despite backup failure")
	}
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "persist_failed" {
		t.Fatalf("error = %v, want persist_failed outcome", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read config after failed commit: %v", readErr)
	}
	if !bytes.Equal(after, legacy) {
		t.Fatal("legacy config changed after backup failure")
	}
}

func writeLegacyConfigFixture(t *testing.T) (string, []byte) {
	t.Helper()
	legacy, err := os.ReadFile(filepath.Join("..", "config", "testdata", "provider_groups", "v1_single_upstream_key.input.json"))
	if err != nil {
		t.Fatalf("read legacy fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy fixture: %v", err)
	}
	return path, legacy
}
