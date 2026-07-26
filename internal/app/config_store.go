package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/configstore"
)

// ConfigRevision is the opaque path-scoped config digest used by lifecycle APIs.
type ConfigRevision = configstore.Revision

func configCodec() configstore.Codec[*config.Config] {
	return configstore.Codec[*config.Config]{
		Decode: func(path string, raw []byte, exists bool) (*config.Config, error) {
			if !exists || len(raw) == 0 {
				return config.LoadFromBytes(path, nil)
			}
			return config.LoadFromBytes(path, raw)
		},
		Clone: func(cfg *config.Config) (*config.Config, error) {
			if cfg == nil {
				return config.Default(), nil
			}
			return cfg.CloneDeep()
		},
		Encode: func(_ configstore.Snapshot[*config.Config], candidate *config.Config) ([]byte, error) {
			if candidate == nil {
				return nil, fmt.Errorf("encode config: nil candidate")
			}
			return candidate.MarshalPersistent()
		},
	}
}

func (s *Service) configStore(ctx context.Context) (*configstore.Store[*config.Config], error) {
	_ = ctx
	hooks := configstore.Hooks[*config.Config]{
		Validate: func(_ context.Context, candidate configstore.Candidate[*config.Config]) error {
			if candidate.Value == nil {
				return fmt.Errorf("candidate config is nil")
			}
			if errs := candidate.Value.ValidateForPersist(); len(errs) > 0 {
				return errs[0]
			}
			return nil
		},
		Build: func(_ context.Context, candidate configstore.Candidate[*config.Config]) (any, error) {
			// Candidate is already a validated *config.Config; proxy reload uses it directly.
			return candidate.Value, nil
		},
		Apply: func(_ context.Context, result configstore.Result[*config.Config], _ any) error {
			if result.Snapshot.Value == nil {
				return nil
			}
			return s.reloadRunningProxyConfig(result.Snapshot.Value)
		},
		BeforePersist: func(_ context.Context, candidate configstore.Candidate[*config.Config]) error {
			if !config.NeedsLegacyConfigBackup(candidate.BaseBytes()) {
				return nil
			}
			return config.BackupLegacyConfigFile(s.ConfigPath())
		},
	}
	return configstore.New(s.ConfigPath(), configCodec(), hooks)
}

// SnapshotConfigRevision returns the current revision and config snapshot.
func (s *Service) SnapshotConfigRevision(ctx context.Context) (ConfigRevision, *config.Config, error) {
	store, err := s.configStore(ctx)
	if err != nil {
		return "", nil, err
	}
	snap, err := store.Snapshot(ctx)
	if err != nil {
		return "", nil, err
	}
	return snap.Revision, snap.Value, nil
}

// commitConfig applies mutate under ConfigStore CAS.
// When expected is empty, the latest revision is used for a single-shot CAS (legacy callers).
func (s *Service) commitConfig(ctx context.Context, expected ConfigRevision, mutate func(context.Context, *config.Config) (configstore.Mutation[*config.Config], error)) (configstore.Result[*config.Config], error) {
	store, err := s.configStore(ctx)
	if err != nil {
		return configstore.Result[*config.Config]{}, err
	}
	if expected == "" {
		snap, err := store.Snapshot(ctx)
		if err != nil {
			return configstore.Result[*config.Config]{}, err
		}
		expected = snap.Revision
	}
	result, err := store.Mutate(ctx, expected, mutate)
	if err != nil {
		return result, mapConfigStoreError(err)
	}
	return result, nil
}

// commitConfigReplace replaces the full config value under CAS.
func (s *Service) commitConfigReplace(ctx context.Context, expected ConfigRevision, next *config.Config) (configstore.Result[*config.Config], error) {
	return s.commitConfig(ctx, expected, func(_ context.Context, _ *config.Config) (configstore.Mutation[*config.Config], error) {
		if next == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("nil config candidate")
		}
		return configstore.Mutation[*config.Config]{Value: next, Changed: true}, nil
	})
}

func mapConfigStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, configstore.ErrRevisionRequired):
		return &OutcomeError{Code: "revision_required", Err: err}
	case errors.Is(err, configstore.ErrRevisionConflict):
		var conflict *configstore.ConflictError
		if errors.As(err, &conflict) {
			return &OutcomeError{
				Code: "revision_conflict",
				Params: map[string]any{
					"expected": string(conflict.Expected),
					"current":  string(conflict.Current),
				},
				Err: err,
			}
		}
		return &OutcomeError{Code: "revision_conflict", Err: err}
	case errors.Is(err, configstore.ErrPersistFailed):
		return &OutcomeError{Code: "persist_failed", Err: err}
	case errors.Is(err, configstore.ErrApplyFailed):
		return &OutcomeError{Code: "runtime_apply_failed", Err: err}
	default:
		return err
	}
}

// OutcomeError is a stable transport-facing business error.
type OutcomeError struct {
	Code   string
	Params map[string]any
	Err    error
}

func (e *OutcomeError) Error() string {
	if e == nil {
		return "outcome error"
	}
	// Wire/compat clients match the stable code exactly (artifact 06).
	if code := strings.TrimSpace(e.Code); code != "" {
		return code
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "outcome error"
}

func (e *OutcomeError) Unwrap() error { return e.Err }

func (e *OutcomeError) Is(target error) bool {
	other, ok := target.(*OutcomeError)
	return ok && other != nil && other.Code == e.Code
}
