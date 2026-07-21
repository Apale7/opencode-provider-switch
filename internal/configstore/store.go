// Package configstore coordinates revisioned configuration mutations.
package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Apale7/opencode-provider-switch/internal/fileutil"
)

var (
	ErrRevisionRequired = errors.New("config revision is required")
	ErrRevisionConflict = errors.New("config revision conflict")
	ErrPersistFailed    = errors.New("config persist failed")
	ErrApplyFailed      = errors.New("committed config apply failed")
)

type ConflictError struct {
	Expected Revision
	Current  Revision
}

type CommitState string

const (
	CommitStateNotCommitted CommitState = "not_committed"
	CommitStateCommitted    CommitState = "committed"
	CommitStateUnknown      CommitState = "unknown"
)

func (e *ConflictError) Error() string { return ErrRevisionConflict.Error() }
func (e *ConflictError) Unwrap() error { return ErrRevisionConflict }

type PersistError[T any] struct {
	Result      Result[T]
	Candidate   Revision
	Committed   bool
	CommitState CommitState
	Err         error
}

func (e *PersistError[T]) Error() string        { return ErrPersistFailed.Error() }
func (e *PersistError[T]) Unwrap() error        { return e.Err }
func (e *PersistError[T]) Is(target error) bool { return target == ErrPersistFailed }

type ApplyError[T any] struct {
	Result Result[T]
	Err    error
}

func (e *ApplyError[T]) Error() string        { return ErrApplyFailed.Error() }
func (e *ApplyError[T]) Unwrap() error        { return e.Err }
func (e *ApplyError[T]) Is(target error) bool { return target == ErrApplyFailed }

type Codec[T any] struct {
	Decode func(path string, raw []byte, exists bool) (T, error)
	Clone  func(T) (T, error)
	Encode func(base Snapshot[T], candidate T) ([]byte, error)
}

type Snapshot[T any] struct {
	Path     string
	Revision Revision
	Value    T
	Exists   bool
	raw      []byte
}

func (s Snapshot[T]) RawBytes() []byte { return bytes.Clone(s.raw) }

type Candidate[T any] struct {
	BaseRevision      Revision
	CandidateRevision Revision
	Base              T
	Value             T
	BaseExists        bool
	Exists            bool
	Changed           bool
	baseRaw           []byte
	candidateRaw      []byte
}

func (c Candidate[T]) BaseBytes() []byte      { return bytes.Clone(c.baseRaw) }
func (c Candidate[T]) CandidateBytes() []byte { return bytes.Clone(c.candidateRaw) }

type Hooks[T any] struct {
	Validate func(context.Context, Candidate[T]) error
	Build    func(context.Context, Candidate[T]) (any, error)
	Apply    func(context.Context, Result[T], any) error
}

type Mutation[T any] struct {
	Value   T
	Changed bool
}

type Result[T any] struct {
	BaseRevision      Revision
	CommittedRevision Revision
	Snapshot          Snapshot[T]
	Persisted         bool
	WritePerformed    bool
	Changed           bool
	NoOp              bool
}

type atomicWriteFunc func(string, []byte, os.FileMode) error

type Store[T any] struct {
	path, identity, keyPath string
	codec                   Codec[T]
	hooks                   Hooks[T]
	writeFile               atomicWriteFunc
}

func New[T any](path string, codec Codec[T], hooks Hooks[T]) (*Store[T], error) {
	if codec.Decode == nil || codec.Clone == nil || codec.Encode == nil {
		return nil, errors.New("configstore: complete codec is required")
	}
	canonical, identity, err := canonicalPath(path)
	if err != nil {
		return nil, err
	}
	return &Store[T]{path: canonical, identity: identity, keyPath: canonical + revisionKeySuffix, codec: codec, hooks: hooks, writeFile: fileutil.AtomicWriteFile}, nil
}

func (s *Store[T]) Snapshot(ctx context.Context) (out Snapshot[T], err error) {
	err = s.locked(ctx, func(key []byte) error {
		raw, exists, err := readRaw(s.path)
		if err != nil {
			return err
		}
		out, err = s.decode(revisionFor(key, s.identity, exists, raw), raw, exists)
		return err
	})
	return out, err
}

func (s *Store[T]) Mutate(ctx context.Context, expected Revision, mutate func(context.Context, T) (Mutation[T], error)) (result Result[T], err error) {
	if expected == "" {
		return result, ErrRevisionRequired
	}
	if mutate == nil {
		return result, errors.New("configstore: mutate callback is required")
	}
	err = s.locked(ctx, func(key []byte) error {
		raw, exists, err := readRaw(s.path)
		if err != nil {
			return err
		}
		currentRevision := revisionFor(key, s.identity, exists, raw)
		if currentRevision != expected {
			return &ConflictError{Expected: expected, Current: currentRevision}
		}
		base, err := s.decode(currentRevision, raw, exists)
		if err != nil {
			return err
		}
		working, err := s.codec.Clone(base.Value)
		if err != nil {
			return fmt.Errorf("clone config: %w", err)
		}
		mutation, err := mutate(ctx, working)
		if err != nil {
			return err
		}
		candidate, err := s.candidate(key, base, mutation)
		if err != nil {
			return err
		}
		prepared, err := s.precommit(ctx, candidate)
		if err != nil {
			return err
		}
		// Build the caller-visible result before crossing the commit point.
		result, err = s.result(candidate, candidate.Changed)
		if err != nil {
			return err
		}
		if candidate.Changed {
			if writeErr := s.writeFile(s.path, candidate.candidateRaw, 0o600); writeErr != nil {
				state, verifyErr := candidateCommitState(s.path, candidate)
				cause := writeErr
				if verifyErr != nil {
					cause = errors.Join(writeErr, verifyErr)
					state = CommitStateUnknown
				}
				committed := state == CommitStateCommitted
				persistErr := &PersistError[T]{Candidate: candidate.CandidateRevision, Committed: committed, CommitState: state, Err: cause}
				if !committed {
					result = Result[T]{}
					return persistErr
				}
				persistErr.Result = result
				if applyErr := s.apply(ctx, result, prepared); applyErr != nil {
					return errors.Join(persistErr, applyErr)
				}
				return persistErr
			}
		}
		return s.apply(ctx, result, prepared)
	})
	return result, err
}

func (s *Store[T]) precommit(ctx context.Context, candidate Candidate[T]) (any, error) {
	if s.hooks.Validate != nil {
		view, err := s.cloneCandidate(candidate)
		if err != nil {
			return nil, fmt.Errorf("clone validation candidate: %w", err)
		}
		if err := s.hooks.Validate(ctx, view); err != nil {
			return nil, fmt.Errorf("validate candidate: %w", err)
		}
	}
	if s.hooks.Build == nil {
		return nil, nil
	}
	view, err := s.cloneCandidate(candidate)
	if err != nil {
		return nil, fmt.Errorf("clone build candidate: %w", err)
	}
	prepared, err := s.hooks.Build(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("build candidate: %w", err)
	}
	return prepared, nil
}

func (s *Store[T]) apply(ctx context.Context, result Result[T], prepared any) error {
	if s.hooks.Apply == nil {
		return nil
	}
	view, err := s.cloneResult(result)
	if err != nil {
		return &ApplyError[T]{Result: result, Err: fmt.Errorf("clone apply result: %w", err)}
	}
	if err := s.hooks.Apply(ctx, view, prepared); err != nil {
		return &ApplyError[T]{Result: result, Err: err}
	}
	return nil
}

func (s *Store[T]) decode(revision Revision, raw []byte, exists bool) (Snapshot[T], error) {
	value, err := s.codec.Decode(s.path, bytes.Clone(raw), exists)
	if err != nil {
		return Snapshot[T]{}, fmt.Errorf("decode config: %w", err)
	}
	return Snapshot[T]{Path: s.path, Revision: revision, Value: value, Exists: exists, raw: bytes.Clone(raw)}, nil
}

func (s *Store[T]) candidate(key []byte, base Snapshot[T], mutation Mutation[T]) (Candidate[T], error) {
	if !mutation.Changed {
		value, err := s.codec.Clone(base.Value)
		if err != nil {
			return Candidate[T]{}, err
		}
		return Candidate[T]{BaseRevision: base.Revision, CandidateRevision: base.Revision, Base: base.Value, Value: value, BaseExists: base.Exists, Exists: base.Exists, baseRaw: base.RawBytes(), candidateRaw: base.RawBytes()}, nil
	}
	baseForEncode, err := s.cloneSnapshot(base)
	if err != nil {
		return Candidate[T]{}, fmt.Errorf("clone encode base: %w", err)
	}
	valueForEncode, err := s.codec.Clone(mutation.Value)
	if err != nil {
		return Candidate[T]{}, fmt.Errorf("clone mutation: %w", err)
	}
	raw, err := s.codec.Encode(baseForEncode, valueForEncode)
	if err != nil {
		return Candidate[T]{}, fmt.Errorf("encode candidate: %w", err)
	}
	raw = bytes.Clone(raw)
	value, err := s.codec.Decode(s.path, bytes.Clone(raw), true)
	if err != nil {
		return Candidate[T]{}, fmt.Errorf("decode candidate: %w", err)
	}
	changed := !base.Exists || !bytes.Equal(base.raw, raw)
	revision := base.Revision
	if changed {
		revision = revisionFor(key, s.identity, true, raw)
	}
	return Candidate[T]{BaseRevision: base.Revision, CandidateRevision: revision, Base: base.Value, Value: value, BaseExists: base.Exists, Exists: true, Changed: changed, baseRaw: base.RawBytes(), candidateRaw: raw}, nil
}

func (s *Store[T]) result(candidate Candidate[T], write bool) (Result[T], error) {
	snapshot, err := s.decode(candidate.CandidateRevision, candidate.candidateRaw, candidate.Exists)
	if err != nil {
		return Result[T]{}, fmt.Errorf("construct committed result: %w", err)
	}
	return Result[T]{BaseRevision: candidate.BaseRevision, CommittedRevision: candidate.CandidateRevision, Snapshot: snapshot, Persisted: true, WritePerformed: write, Changed: candidate.Changed, NoOp: !candidate.Changed}, nil
}

func (s *Store[T]) cloneSnapshot(snapshot Snapshot[T]) (Snapshot[T], error) {
	value, err := s.codec.Clone(snapshot.Value)
	if err != nil {
		return Snapshot[T]{}, err
	}
	snapshot.Value, snapshot.raw = value, bytes.Clone(snapshot.raw)
	return snapshot, nil
}

func (s *Store[T]) cloneCandidate(candidate Candidate[T]) (Candidate[T], error) {
	base, err := s.codec.Clone(candidate.Base)
	if err != nil {
		return Candidate[T]{}, err
	}
	value, err := s.codec.Clone(candidate.Value)
	if err != nil {
		return Candidate[T]{}, err
	}
	candidate.Base, candidate.Value = base, value
	candidate.baseRaw, candidate.candidateRaw = bytes.Clone(candidate.baseRaw), bytes.Clone(candidate.candidateRaw)
	return candidate, nil
}

func (s *Store[T]) cloneResult(result Result[T]) (Result[T], error) {
	snapshot, err := s.cloneSnapshot(result.Snapshot)
	if err != nil {
		return Result[T]{}, err
	}
	result.Snapshot = snapshot
	return result, nil
}

func (s *Store[T]) locked(ctx context.Context, fn func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return fileutil.WithLockedFile(s.path, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.ensureIdentity(); err != nil {
			return err
		}
		key, err := loadOrCreateKey(s.keyPath)
		if err != nil {
			return err
		}
		return fn(key)
	})
}

func (s *Store[T]) ensureIdentity() error {
	_, identity, err := canonicalPath(s.path)
	if err != nil {
		return err
	}
	if identity != s.identity {
		return errors.New("configstore: config path identity changed")
	}
	return nil
}

func readRaw(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, false, errors.New("configstore: config path became a symbolic link")
			} else if lerr != nil && !errors.Is(lerr, os.ErrNotExist) {
				return nil, false, fmt.Errorf("inspect config: %w", lerr)
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect open config: %w", err)
	}
	named, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, errors.New("configstore: config path disappeared during read")
		}
		return nil, false, fmt.Errorf("inspect config: %w", err)
	}
	if named.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(info, named) {
		return nil, false, errors.New("configstore: config path is not a regular file")
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	return raw, true, nil
}

// candidateCommitState classifies post-write disk bytes relative to the mutation:
// candidate bytes => committed, base bytes => not_committed, anything else => unknown.
func candidateCommitState[T any](path string, candidate Candidate[T]) (CommitState, error) {
	raw, exists, err := readRaw(path)
	if err != nil {
		return CommitStateUnknown, err
	}
	if exists == candidate.Exists && bytes.Equal(raw, candidate.candidateRaw) {
		return CommitStateCommitted, nil
	}
	if exists == candidate.BaseExists && bytes.Equal(raw, candidate.baseRaw) {
		return CommitStateNotCommitted, nil
	}
	return CommitStateUnknown, nil
}
