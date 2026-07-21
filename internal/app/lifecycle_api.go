package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

// Lifecycle DTOs mirror artifact 05 for Service/Wails/HTTP transport.

type LifecyclePreviewInput struct {
	Revision          ConfigRevision            `json:"revision"`
	Operation         lifecycle.Operation       `json:"operation"`
	Selections        []lifecycle.Selection     `json:"selections"`
	PreparationToken  string                    `json:"preparationToken,omitempty"`
	ExternalOpenCode  lifecycle.ExternalRefs    `json:"externalOpenCode,omitempty"`
}

type LifecycleExecuteInput struct {
	Revision         ConfigRevision        `json:"revision"`
	PlanToken        string                `json:"planToken"`
	Operation        lifecycle.Operation   `json:"operation"`
	Selections       []lifecycle.Selection `json:"selections"`
	PreparationToken string                `json:"preparationToken,omitempty"`
}

type LifecyclePlanView struct {
	ContractVersion   string                    `json:"contractVersion"`
	PlannerVersion    string                    `json:"plannerVersion"`
	BaseRevision      ConfigRevision            `json:"baseRevision"`
	CandidateRevision ConfigRevision            `json:"candidateRevision,omitempty"`
	OperationKind     string                    `json:"operationKind"`
	Executable        bool                      `json:"executable"`
	NoOp              bool                      `json:"noOp"`
	PlanToken         string                    `json:"planToken,omitempty"`
	ExpiresAt         *time.Time                `json:"expiresAt,omitempty"`
	RequestedChanges  []lifecycle.Change        `json:"requestedChanges"`
	AutomaticChanges  []lifecycle.Change        `json:"automaticChanges"`
	SelectedChanges   []lifecycle.Change        `json:"selectedChanges"`
	Blockers          []lifecycle.Issue         `json:"blockers"`
	Choices           []lifecycle.Choice        `json:"choices"`
	PreservedIssues   []lifecycle.Issue         `json:"preservedIssues"`
	RuntimeImpact     lifecycle.RuntimeImpact   `json:"runtimeImpact"`
}

type LifecycleExecuteResult struct {
	ContractVersion        string                  `json:"contractVersion"`
	BaseRevision           ConfigRevision          `json:"baseRevision"`
	CommittedRevision      ConfigRevision          `json:"committedRevision"`
	RuntimeRevision        *ConfigRevision         `json:"runtimeRevision"`
	Persisted              bool                    `json:"persisted"`
	WritePerformed         bool                    `json:"writePerformed"`
	Changed                bool                    `json:"changed"`
	NoOp                   bool                    `json:"noOp"`
	CandidateAlreadyPresent bool                   `json:"candidateAlreadyPresent"`
	RuntimeApplied         bool                    `json:"runtimeApplied"`
	PendingRestart         bool                    `json:"pendingRestart"`
	RuntimeState           string                  `json:"runtimeState"`
	Issues                 []lifecycle.Issue       `json:"issues"`
	Plan                   LifecyclePlanView       `json:"plan"`
}

// GetConfigRevision returns the current config revision for clients.
func (s *Service) GetConfigRevision(ctx context.Context) (ConfigRevision, error) {
	rev, _, err := s.SnapshotConfigRevision(ctx)
	return rev, err
}

// PreviewLifecycle plans a mutation without side effects.
func (s *Service) PreviewLifecycle(ctx context.Context, in LifecyclePreviewInput) (LifecyclePlanView, error) {
	if strings.TrimSpace(string(in.Revision)) == "" {
		return LifecyclePlanView{}, &OutcomeError{Code: "revision_required"}
	}
	store, err := s.configStore(ctx)
	if err != nil {
		return LifecyclePlanView{}, err
	}
	snap, err := store.Snapshot(ctx)
	if err != nil {
		return LifecyclePlanView{}, err
	}
	if snap.Revision != in.Revision {
		return LifecyclePlanView{}, &OutcomeError{
			Code: "revision_conflict",
			Params: map[string]any{
				"expected": string(in.Revision),
				"current":  string(snap.Revision),
			},
		}
	}
	result, err := planOperation(snap.Value, string(snap.Revision), in.Operation, in.Selections, in.ExternalOpenCode)
	if err != nil {
		return LifecyclePlanView{}, err
	}
	view := planView(result.Plan, snap.Revision)
	if result.Plan.Executable && result.Candidate != nil && !result.Plan.NoOp {
		// Compute candidate revision via dry encode under store codec without writing.
		raw, err := result.Candidate.MarshalPersistent()
		if err != nil {
			return LifecyclePlanView{}, err
		}
		// Use a second snapshot path: candidate revision is derived by temporary mutate no-op check.
		// Encode-only digest: re-open store and hash via Snapshot after hypothetical - instead stamp token.
		view.CandidateRevision = ConfigRevision("candidate:" + shortDigest(raw))
		view.PlanToken = mintPlanToken(view, in.Operation, in.Selections)
		exp := time.Now().Add(10 * time.Minute)
		view.ExpiresAt = &exp
	} else if result.Plan.Executable && result.Plan.NoOp {
		view.CandidateRevision = snap.Revision
		view.PlanToken = mintPlanToken(view, in.Operation, in.Selections)
		exp := time.Now().Add(10 * time.Minute)
		view.ExpiresAt = &exp
	}
	return view, nil
}

// ExecuteLifecycle commits an executable plan under ConfigStore CAS.
func (s *Service) ExecuteLifecycle(ctx context.Context, in LifecycleExecuteInput) (LifecycleExecuteResult, error) {
	if strings.TrimSpace(string(in.Revision)) == "" {
		return LifecycleExecuteResult{}, &OutcomeError{Code: "revision_required"}
	}
	if strings.TrimSpace(in.PlanToken) == "" {
		return LifecycleExecuteResult{}, &OutcomeError{Code: "plan_not_executable", Params: map[string]any{"reason": "missing_plan_token"}}
	}

	// Preview again with same inputs to rebuild candidate (same planner).
	preview, err := s.PreviewLifecycle(ctx, LifecyclePreviewInput{
		Revision:   in.Revision,
		Operation:  in.Operation,
		Selections: in.Selections,
	})
	if err != nil {
		return LifecycleExecuteResult{}, err
	}
	if !preview.Executable {
		return LifecycleExecuteResult{}, &OutcomeError{
			Code:   "plan_not_executable",
			Params: map[string]any{"blockerCount": len(preview.Blockers)},
		}
	}
	if preview.PlanToken != in.PlanToken {
		return LifecycleExecuteResult{}, &OutcomeError{Code: "plan_mismatch"}
	}
	if preview.ExpiresAt != nil && time.Now().After(*preview.ExpiresAt) {
		return LifecycleExecuteResult{}, &OutcomeError{Code: "plan_expired"}
	}

	store, err := s.configStore(ctx)
	if err != nil {
		return LifecycleExecuteResult{}, err
	}
	snap, err := store.Snapshot(ctx)
	if err != nil {
		return LifecycleExecuteResult{}, err
	}
	if snap.Revision != in.Revision {
		return LifecycleExecuteResult{}, &OutcomeError{
			Code: "revision_conflict",
			Params: map[string]any{
				"expected": string(in.Revision),
				"current":  string(snap.Revision),
			},
		}
	}

	planned, err := planOperation(snap.Value, string(snap.Revision), in.Operation, in.Selections, lifecycle.ExternalRefs{})
	if err != nil {
		return LifecycleExecuteResult{}, err
	}
	if !planned.Plan.Executable {
		return LifecycleExecuteResult{}, &OutcomeError{Code: "plan_not_executable"}
	}

	var commitRev ConfigRevision
	var writePerformed, changed, noOp, persisted bool
	runtimeApplied := false
	runtimeState := "not_running"

	if planned.Plan.NoOp || planned.Candidate == nil {
		noOp = true
		persisted = true
		commitRev = snap.Revision
		changed = false
		writePerformed = false
		if err := s.reloadRunningProxyConfig(snap.Value); err != nil {
			runtimeState = "apply_failed"
			return LifecycleExecuteResult{
				ContractVersion:   lifecycle.ContractVersion,
				BaseRevision:      snap.Revision,
				CommittedRevision: commitRev,
				Persisted:         true,
				NoOp:              true,
				RuntimeState:      runtimeState,
				Plan:              preview,
			}, &OutcomeError{Code: "runtime_apply_failed", Err: err}
		}
		if s.isProxyRunning() {
			runtimeApplied = true
			runtimeState = "already_applied"
		}
	} else {
		result, err := s.commitConfigReplace(ctx, in.Revision, planned.Candidate)
		if err != nil {
			return LifecycleExecuteResult{Plan: preview}, err
		}
		commitRev = result.CommittedRevision
		persisted = result.Persisted
		writePerformed = result.WritePerformed
		changed = result.Changed
		noOp = result.NoOp
		if s.isProxyRunning() {
			runtimeApplied = true
			runtimeState = "applied"
		}
	}

	out := LifecycleExecuteResult{
		ContractVersion:   lifecycle.ContractVersion,
		BaseRevision:      in.Revision,
		CommittedRevision: commitRev,
		Persisted:         persisted,
		WritePerformed:    writePerformed,
		Changed:           changed,
		NoOp:              noOp,
		RuntimeApplied:    runtimeApplied,
		RuntimeState:      runtimeState,
		Plan:              preview,
	}
	if runtimeApplied {
		rev := commitRev
		out.RuntimeRevision = &rev
	}
	return out, nil
}

func (s *Service) isProxyRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxyStatus.Running && s.proxyServer != nil
}

func planOperation(cfg *config.Config, baseRevision string, op lifecycle.Operation, selections []lifecycle.Selection, external lifecycle.ExternalRefs) (lifecycle.Result, error) {
	kind := strings.TrimSpace(op.Kind)
	switch kind {
	case lifecycle.OpProviderRemove:
		var payload lifecycle.ProviderRemovePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanProviderRemove(cfg, baseRevision, payload.ProviderID, selections)
	case lifecycle.OpAliasRemove:
		var payload lifecycle.AliasRemovePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanAliasRemove(cfg, baseRevision, payload.Alias, selections, external)
	case lifecycle.OpAliasUpgrade:
		var payload lifecycle.AliasUpgradePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanAliasUpgrade(cfg, baseRevision, payload.Alias)
	case lifecycle.OpAliasMutate:
		var payload lifecycle.AliasMutatePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanAliasMutateGate(cfg, baseRevision, payload.Alias, payload.Intent)
	case lifecycle.OpDiscoverySync:
		var payload lifecycle.DiscoveryReconcilePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanDiscoveryReconcile(cfg, baseRevision, payload.ProviderID, payload.Observation)
	case "provider.state":
		var payload struct {
			ProviderID string `json:"providerId"`
			Disabled   bool   `json:"disabled"`
		}
		if err := decodePayload(op.Payload, &payload); err != nil {
			return lifecycle.Result{}, err
		}
		return lifecycle.PlanProviderDisabled(cfg, baseRevision, payload.ProviderID, payload.Disabled)
	default:
		return lifecycle.Result{}, &OutcomeError{
			Code:   "invalid_request",
			Params: map[string]any{"operationKind": kind},
		}
	}
}

func decodePayload(raw json.RawMessage, dest any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return &OutcomeError{Code: "invalid_request", Params: map[string]any{"reason": "missing_payload"}}
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return &OutcomeError{Code: "invalid_request", Err: err}
	}
	return nil
}

func planView(plan lifecycle.Plan, base ConfigRevision) LifecyclePlanView {
	return LifecyclePlanView{
		ContractVersion:  plan.ContractVersion,
		PlannerVersion:   plan.PlannerVersion,
		BaseRevision:     base,
		OperationKind:    plan.OperationKind,
		Executable:       plan.Executable,
		NoOp:             plan.NoOp,
		RequestedChanges: plan.RequestedChanges,
		AutomaticChanges: plan.AutomaticChanges,
		SelectedChanges:  plan.SelectedChanges,
		Blockers:         plan.Blockers,
		Choices:          plan.Choices,
		PreservedIssues:  plan.PreservedIssues,
		RuntimeImpact:    plan.RuntimeImpact,
	}
}

func mintPlanToken(view LifecyclePlanView, op lifecycle.Operation, selections []lifecycle.Selection) string {
	// Non-secret integrity token for same-process preview/execute pairing.
	// Step 5 transport layer; hardened HMAC can reuse configstore key later.
	type body struct {
		Base      string                `json:"base"`
		Candidate string                `json:"candidate"`
		Kind      string                `json:"kind"`
		Payload   json.RawMessage       `json:"payload"`
		Select    []lifecycle.Selection `json:"selections"`
		Planner   string                `json:"planner"`
	}
	raw, _ := json.Marshal(body{
		Base:      string(view.BaseRevision),
		Candidate: string(view.CandidateRevision),
		Kind:      op.Kind,
		Payload:   op.Payload,
		Select:    selections,
		Planner:   view.PlannerVersion,
	})
	return "v1." + shortDigest(raw)
}

func shortDigest(raw []byte) string {
	// FNV-1a 64-bit hex for compact non-crypto plan binding.
	var h uint64 = 14695981039346656037
	for _, b := range raw {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}
