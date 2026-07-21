package configstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type document struct {
	Value int `json:"value"`
}

func testStore(t *testing.T, path string, hooks Hooks[*document]) *Store[*document] {
	t.Helper()
	codec := Codec[*document]{
		Decode: func(_ string, raw []byte, exists bool) (*document, error) {
			v := &document{}
			if exists && len(raw) > 0 {
				if err := json.Unmarshal(raw, v); err != nil {
					return nil, err
				}
			}
			return v, nil
		},
		Clone: func(v *document) (*document, error) { c := *v; return &c, nil },
		Encode: func(_ Snapshot[*document], v *document) ([]byte, error) {
			raw, err := json.Marshal(v)
			return append(raw, '\n'), err
		},
	}
	store, err := New(path, codec, hooks)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRevisionUsesPathAndExactBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	_ = os.WriteFile(a, []byte(`{"value":1}`), 0o600)
	_ = os.WriteFile(b, []byte(`{"value":1}`), 0o600)
	sa, sb := testStore(t, a, Hooks[*document]{}), testStore(t, b, Hooks[*document]{})
	ra, _ := sa.Snapshot(context.Background())
	rb, _ := sb.Snapshot(context.Background())
	if ra.Revision == rb.Revision {
		t.Fatal("same bytes at different paths share revision")
	}
	_ = os.WriteFile(a, []byte(`{ "value":1 }`), 0o600)
	rc, _ := sa.Snapshot(context.Background())
	if rc.Revision == ra.Revision {
		t.Fatal("format change did not alter revision")
	}
	missing := testStore(t, filepath.Join(dir, "missing.json"), Hooks[*document]{})
	rm, _ := missing.Snapshot(context.Background())
	_ = os.WriteFile(filepath.Join(dir, "missing.json"), nil, 0o600)
	re, _ := missing.Snapshot(context.Background())
	if rm.Revision == re.Revision {
		t.Fatal("missing equals empty")
	}
}

func TestCASAndHooksBeforePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600)
	boom := errors.New("invalid candidate")
	store := testStore(t, path, Hooks[*document]{Validate: func(context.Context, Candidate[*document]) error { return boom }})
	base, _ := store.Snapshot(context.Background())
	_, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, v *document) (Mutation[*document], error) {
		v.Value = 2
		return Mutation[*document]{Value: v, Changed: true}, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error=%v", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{\"value\":1}\n" {
		t.Fatal("validator failure persisted")
	}
	_ = os.WriteFile(path, []byte("{\"value\":3}\n"), 0o600)
	_, err = store.Mutate(context.Background(), base.Revision, func(_ context.Context, v *document) (Mutation[*document], error) {
		return Mutation[*document]{Value: v}, nil
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestCommitNoOpAndApplyFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600)
	store := testStore(t, path, Hooks[*document]{})
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, v *document) (Mutation[*document], error) {
		return Mutation[*document]{Value: v}, nil
	})
	if err != nil || !result.NoOp || result.WritePerformed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	boom := errors.New("apply")
	store.hooks.Apply = func(context.Context, Result[*document], any) error { return boom }
	result, err = store.Mutate(context.Background(), base.Revision, func(_ context.Context, v *document) (Mutation[*document], error) {
		v.Value = 2
		return Mutation[*document]{Value: v, Changed: true}, nil
	})
	if !errors.Is(err, ErrApplyFailed) || !result.Persisted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var applyErr *ApplyError[*document]
	if !errors.As(err, &applyErr) || applyErr.Result.CommittedRevision != result.CommittedRevision {
		t.Fatalf("apply error lost committed result: %#v", applyErr)
	}
}

func TestStaleMalformedSnapshotReturnsConflictBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, path, Hooks[*document]{})
	base, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		called = true
		return Mutation[*document]{Value: value}, nil
	})
	if !errors.Is(err, ErrRevisionConflict) || called {
		t.Fatalf("error=%v mutationCalled=%v", err, called)
	}
}

func TestHooksAndCallerResultAreDetachedFromFrozenCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, path, Hooks[*document]{
		Validate: func(_ context.Context, candidate Candidate[*document]) error {
			candidate.Base.Value = 80
			candidate.Value.Value = 90
			return nil
		},
		Build: func(_ context.Context, candidate Candidate[*document]) (any, error) {
			if candidate.Base.Value != 1 || candidate.Value.Value != 2 {
				t.Fatalf("validate mutation leaked into build: base=%d value=%d", candidate.Base.Value, candidate.Value.Value)
			}
			candidate.Value.Value = 99
			return "prepared", nil
		},
		Apply: func(_ context.Context, result Result[*document], prepared any) error {
			if prepared != "prepared" || result.Snapshot.Value.Value != 2 {
				t.Fatalf("unexpected apply view: prepared=%v result=%+v", prepared, result)
			}
			result.Snapshot.Value.Value = 77
			return nil
		},
	})
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		value.Value = 2
		return Mutation[*document]{Value: value, Changed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Value.Value != 2 {
		t.Fatalf("apply mutated caller result: %+v", result)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{\"value\":2}\n" {
		t.Fatalf("persisted bytes=%q", raw)
	}
}

func TestMissingNoOpPreservesMissingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	store := testStore(t, path, Hooks[*document]{})
	base, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		return Mutation[*document]{Value: value}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Exists || result.WritePerformed {
		t.Fatalf("missing no-op became present: %+v", result)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing no-op wrote config: %v", err)
	}
}

func TestRevisionKeyRejectsUnsafeExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := testStore(t, path, Hooks[*document]{})
	if err := os.WriteFile(store.keyPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(context.Background()); !errors.Is(err, ErrRevisionKeyInvalid) {
		t.Fatalf("error=%v", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.WriteFile(store.keyPath, bytes.Repeat([]byte{'k'}, revisionKeySize), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(store.keyPath, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Snapshot(context.Background()); !errors.Is(err, ErrRevisionKeyInvalid) {
			t.Fatalf("weak mode error=%v", err)
		}
	}
}

func TestWriteErrorAfterVisibleCommitCarriesResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, path, Hooks[*document]{})
	realWrite := store.writeFile
	boom := errors.New("directory sync failed")
	store.writeFile = func(path string, data []byte, mode os.FileMode) error {
		if err := realWrite(path, data, mode); err != nil {
			return err
		}
		return boom
	}
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		value.Value = 2
		return Mutation[*document]{Value: value, Changed: true}, nil
	})
	var persistErr *PersistError[*document]
	if !errors.As(err, &persistErr) || !persistErr.Committed || persistErr.CommitState != CommitStateCommitted || !result.Persisted {
		t.Fatalf("result=%+v err=%#v", result, persistErr)
	}
	if persistErr.Result.CommittedRevision != result.CommittedRevision {
		t.Fatal("persist error lost committed result")
	}
}

func TestCanonicalPathResolvesParentSymlink(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath, realIdentity, err := canonicalPath(filepath.Join(realDir, "nested", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	linkPath, linkIdentity, err := canonicalPath(filepath.Join(linkDir, "nested", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if realPath != linkPath || realIdentity != linkIdentity {
		t.Fatalf("canonical aliases differ: %q/%q vs %q/%q", realPath, realIdentity, linkPath, linkIdentity)
	}
}

func TestStoreRejectsChangedParentIdentity(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.Mkdir(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, filepath.Join(configDir, "config.json"), Hooks[*document]{})
	moved := filepath.Join(dir, "moved")
	if err := os.Rename(configDir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, configDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("changed parent identity was accepted: %v", err)
	}
}

func TestPersistErrorReportsUnknownCommitWhenVerificationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, path, Hooks[*document]{})
	store.writeFile = func(path string, _ []byte, _ os.FileMode) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		return errors.New("commit outcome unavailable")
	}
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		value.Value = 2
		return Mutation[*document]{Value: value, Changed: true}, nil
	})
	var persistErr *PersistError[*document]
	if !errors.As(err, &persistErr) || persistErr.CommitState != CommitStateUnknown || persistErr.Committed || result.Persisted {
		t.Fatalf("result=%+v error=%#v", result, persistErr)
	}
}

func TestPersistErrorReportsUnknownCommitForThirdState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testStore(t, path, Hooks[*document]{})
	store.writeFile = func(path string, _ []byte, _ os.FileMode) error {
		if err := os.WriteFile(path, []byte("{\"value\":3}\n"), 0o600); err != nil {
			return err
		}
		return errors.New("write outcome unavailable")
	}
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		value.Value = 2
		return Mutation[*document]{Value: value, Changed: true}, nil
	})
	var persistErr *PersistError[*document]
	if !errors.As(err, &persistErr) || persistErr.CommitState != CommitStateUnknown || result.Persisted {
		t.Fatalf("result=%+v error=%#v", result, persistErr)
	}
}

func TestWriteErrorAfterVisibleCommitWithApplyFailureJoinsErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBoom := errors.New("directory sync failed")
	applyBoom := errors.New("apply failed after commit")
	store := testStore(t, path, Hooks[*document]{
		Apply: func(context.Context, Result[*document], any) error { return applyBoom },
	})
	realWrite := store.writeFile
	store.writeFile = func(path string, data []byte, mode os.FileMode) error {
		if err := realWrite(path, data, mode); err != nil {
			return err
		}
		return writeBoom
	}
	base, _ := store.Snapshot(context.Background())
	result, err := store.Mutate(context.Background(), base.Revision, func(_ context.Context, value *document) (Mutation[*document], error) {
		value.Value = 2
		return Mutation[*document]{Value: value, Changed: true}, nil
	})
	var persistErr *PersistError[*document]
	var applyErr *ApplyError[*document]
	if !errors.As(err, &persistErr) || !errors.As(err, &applyErr) {
		t.Fatalf("joined errors not extractable: %v", err)
	}
	if !persistErr.Committed || persistErr.CommitState != CommitStateCommitted || !result.Persisted {
		t.Fatalf("result=%+v persist=%#v", result, persistErr)
	}
	if persistErr.Result.CommittedRevision != result.CommittedRevision {
		t.Fatalf("persist error lost committed result: %#v", persistErr.Result)
	}
	if applyErr.Result.CommittedRevision != result.CommittedRevision {
		t.Fatalf("apply error lost committed result: %#v", applyErr.Result)
	}
	if !errors.Is(err, ErrPersistFailed) || !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("joined error lost typed sentinels: %v", err)
	}
}
