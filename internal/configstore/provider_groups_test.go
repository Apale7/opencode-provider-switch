package configstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func configStoreForTest(t *testing.T, path string, hooks Hooks[*config.Config]) *Store[*config.Config] {
	t.Helper()
	codec := Codec[*config.Config]{
		Decode: func(p string, raw []byte, exists bool) (*config.Config, error) {
			if !exists || len(raw) == 0 {
				return config.LoadFromBytes(p, nil)
			}
			return config.LoadFromBytes(p, raw)
		},
		Clone: func(cfg *config.Config) (*config.Config, error) {
			if cfg == nil {
				return config.Default(), nil
			}
			return cfg.CloneDeep()
		},
		Encode: func(_ Snapshot[*config.Config], candidate *config.Config) ([]byte, error) {
			return candidate.MarshalPersistent()
		},
	}
	store, err := New(path, codec, hooks)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestConfigStoreLegacyLoadDoesNotChangeRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	input, err := os.ReadFile(filepath.Join("..", "config", "testdata", "provider_groups", "v1_single_upstream_key.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	store := configStoreForTest(t, path, Hooks[*config.Config]{})
	first, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	// Reload without write — revision must stay identical (load does not rewrite).
	second, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if first.Revision != second.Revision {
		t.Fatalf("revision changed on read-only load: %q → %q", first.Revision, second.Revision)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, input) {
		t.Fatal("disk bytes changed after load-only snapshots")
	}
	if first.Value.SchemaVersion != config.CurrentSchemaVersion {
		t.Fatalf("memory schema = %d", first.Value.SchemaVersion)
	}
	if len(first.Value.Providers) != 1 || len(first.Value.Providers[0].Groups) != 1 {
		t.Fatalf("providers/groups = %#v", first.Value.Providers)
	}
}

func TestConfigStoreFirstV2SaveBackupAndFailureGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	input, err := os.ReadFile(filepath.Join("..", "config", "testdata", "provider_groups", "v1_single_upstream_key.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}

	var runtimeApplied *config.Config
	store := configStoreForTest(t, path, Hooks[*config.Config]{
		BeforePersist: func(_ context.Context, candidate Candidate[*config.Config]) error {
			if !config.NeedsLegacyConfigBackup(candidate.BaseBytes()) {
				return nil
			}
			backupPath := config.BackupPathForConfig(path)
			if err := os.WriteFile(backupPath, candidate.BaseBytes(), 0o600); err != nil {
				return err
			}
			return nil
		},
		Apply: func(_ context.Context, result Result[*config.Config], _ any) error {
			runtimeApplied = result.Snapshot.Value
			return nil
		},
	})

	base, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, cfg *config.Config) (Mutation[*config.Config], error) {
		// Touch a field so encode produces canonical v2.
		cfg.Server.Port = 9982
		return Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if !result.Changed || !result.WritePerformed {
		t.Fatalf("result = %#v", result)
	}
	bakRaw, err := os.ReadFile(config.BackupPathForConfig(path))
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !bytes.Equal(bakRaw, input) {
		t.Fatal("backup bytes != original legacy file")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte(`"schema_version": 2`)) {
		t.Fatalf("saved config not v2: %s", saved)
	}
	if runtimeApplied == nil || runtimeApplied.SchemaVersion != config.CurrentSchemaVersion {
		t.Fatalf("runtime apply missing: %#v", runtimeApplied)
	}

	// Backup failure must not overwrite original or apply runtime snapshot.
	path2 := filepath.Join(dir, "config2.json")
	if err := os.WriteFile(path2, input, 0o600); err != nil {
		t.Fatal(err)
	}
	var applied2 *config.Config
	boom := errors.New("backup failed")
	store2 := configStoreForTest(t, path2, Hooks[*config.Config]{
		BeforePersist: func(context.Context, Candidate[*config.Config]) error {
			return boom
		},
		Apply: func(_ context.Context, result Result[*config.Config], _ any) error {
			applied2 = result.Snapshot.Value
			return nil
		},
	})
	base2, err := store2.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store2.Mutate(context.Background(), base2.Revision, func(_ context.Context, cfg *config.Config) (Mutation[*config.Config], error) {
		cfg.Server.Port = 9999
		return Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if !errors.Is(err, ErrPersistFailed) {
		t.Fatalf("error = %v, want ErrPersistFailed", err)
	}
	raw2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw2, input) {
		t.Fatal("original file overwritten after backup failure")
	}
	if applied2 != nil {
		t.Fatal("runtime snapshot applied after backup failure")
	}
}
