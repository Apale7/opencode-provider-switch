package lifecycle

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// PlanOperation parses an Operation envelope and dispatches to the matching planner.
// It is the single lifecycle entrypoint for preview/execute candidate construction.
func PlanOperation(base *config.Config, baseRevision string, op Operation, selections []Selection, external ExternalRefs) (Result, error) {
	kind := strings.TrimSpace(op.Kind)
	switch kind {
	case OpProviderRemove:
		var payload ProviderRemovePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanProviderRemove(base, baseRevision, payload.ProviderID, selections)
	case OpGroupRemove:
		var payload GroupRemovePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanGroupRemove(base, baseRevision, payload.ProviderID, payload.GroupID, selections)
	case OpGroupIDChange:
		var payload GroupIDChangePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanGroupIDChange(base, baseRevision, payload.ProviderID, payload.OldGroupID, payload.NewGroupID, selections)
	case OpAliasRemove:
		var payload AliasRemovePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanAliasRemove(base, baseRevision, payload.Alias, selections, external)
	case OpAliasUpgrade:
		var payload AliasUpgradePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanAliasUpgrade(base, baseRevision, payload.Alias)
	case OpAliasMutate:
		var payload AliasMutatePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanAliasMutateGate(base, baseRevision, payload.Alias, payload.Intent)
	case OpDiscoverySync:
		var payload DiscoveryReconcilePayload
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanDiscoveryReconcile(base, baseRevision, payload.ProviderID, payload.Observation)
	case "provider.state":
		var payload struct {
			ProviderID string `json:"providerId"`
			Disabled   bool   `json:"disabled"`
		}
		if err := decodePayload(op.Payload, &payload); err != nil {
			return Result{}, err
		}
		return PlanProviderDisabled(base, baseRevision, payload.ProviderID, payload.Disabled)
	default:
		return Result{}, fmt.Errorf("lifecycle: unsupported operation kind %q", kind)
	}
}

func decodePayload(raw json.RawMessage, dest any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("lifecycle: missing payload")
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("lifecycle: invalid payload: %w", err)
	}
	return nil
}
