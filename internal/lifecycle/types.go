// Package lifecycle implements pure configuration lifecycle planners.
// It has no I/O: callers supply a config snapshot and revision, then persist via ConfigStore.
package lifecycle

import (
	"encoding/json"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

const (
	ContractVersion = "lifecycle/v1"
	PlannerVersion  = "lifecycle-planner/v1"

	OpProviderRemove = "provider.remove"
	OpGroupRemove    = "group.remove"
	OpGroupIDChange  = "group.id_change"
	OpAliasRemove    = "alias.remove"
	OpAliasUpgrade   = "alias.upgrade"
	OpAliasMutate    = "alias.mutate"
	OpDiscoverySync  = "provider.discovery_reconcile"

	ChangeAdd     = "add"
	ChangeRemove  = "remove"
	ChangeUpdate  = "update"
	ChangeReorder = "reorder"

	SourceRequested = "requested"
	SourceAutomatic = "automatic"
	SourceSelection = "selection"

	DispositionBlocker        = "blocker"
	DispositionRequiredChoice = "required_choice"
	DispositionPreserved      = "preserved"

	EntityProvider      = "provider"
	EntityProviderGroup = "provider_group"
	EntityAlias         = "alias"
	EntityAliasTarget   = "alias_target"
	EntityRewriteRule   = "rewrite_rule"
	EntityPriority      = "priority_entry"

	// Stable choice option IDs (no force/ignore).
	OptionRebindTarget          = "rebind_target"
	OptionRemoveTarget          = "remove_target"
	OptionDeleteAlias           = "delete_alias"
	OptionKeepDormant           = "keep_dormant"
	OptionDisableRule           = "disable_rule"
	OptionDeleteRule            = "delete_rule"
	OptionReplaceProviders      = "replace_providers"
	OptionReplaceProviderGroups = "replace_provider_groups"
	OptionKeepRule              = "keep_rule"

	ReasonProviderRemove         = "provider_remove"
	ReasonGroupRemove            = "group_remove"
	ReasonGroupIDChange          = "group_id_change"
	ReasonSystemTargetCleanup    = "system_target_cleanup"
	ReasonEmptyAutoAliasCleanup  = "empty_auto_alias_cleanup"
	ReasonPriorityCleanup        = "priority_cleanup"
	ReasonRewriteSelectorNarrow  = "rewrite_selector_narrow"
	ReasonProtectedTarget        = "protected_target"
	ReasonSingletonRewrite       = "singleton_rewrite_selector"
	ReasonAliasRemove            = "alias_remove"
	ReasonRewriteSelectorImpact  = "rewrite_selector_impact"
	ReasonDirectFallbackPossible = "direct_fallback_possible"
	ReasonOpenCodeWeakRef        = "opencode_weak_ref"
	ReasonUpgradeRequired        = "upgrade_required"
	ReasonUpgradeOwnership       = "upgrade_ownership"
	ReasonDiscoveryUntrusted     = "discovery_untrusted"
	ReasonSystemTargetReconcile  = "system_target_reconcile"
	ReasonProviderMissing        = "provider_missing"
	ReasonGroupMissing           = "group_missing"
	ReasonLastGroup              = "last_group"
	ReasonGroupIDConflict        = "group_id_conflict"
	ReasonAliasMissing           = "alias_missing"
	ReasonAliasAmbiguous         = "alias_identity_ambiguous"
	ReasonSelectionRequired      = "selection_required"
	ReasonInvalidSelection       = "invalid_selection"
)

// Selection resolves one required choice.
type Selection struct {
	ChoiceID string         `json:"choiceId"`
	OptionID string         `json:"optionId"`
	Params   map[string]any `json:"params,omitempty"`
}

// Change is one planned mutation.
type Change struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Source     string         `json:"source"`
	Entity     string         `json:"entity"`
	ReasonCode string         `json:"reasonCode"`
	Path       string         `json:"path,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
}

// Issue is a planner-policy issue (disposition is not derived from diagnostic severity).
type Issue struct {
	ID          string         `json:"id"`
	Code        string         `json:"code"`
	Disposition string         `json:"disposition"`
	Path        string         `json:"path,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
}

// ChoiceOption is one concrete allowed action for a required choice.
type ChoiceOption struct {
	ID     string         `json:"id"`
	Params map[string]any `json:"params,omitempty"`
}

// Choice requires an explicit user selection before execute.
type Choice struct {
	ID      string         `json:"id"`
	Code    string         `json:"code"`
	Path    string         `json:"path,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Options []ChoiceOption `json:"options"`
}

// RuntimeImpact summarizes runtime-facing effects without embedding secrets.
type RuntimeImpact struct {
	ProviderRemoved bool `json:"providerRemoved,omitempty"`
	GroupRemoved    bool `json:"groupRemoved,omitempty"`
	GroupIDChanged  bool `json:"groupIdChanged,omitempty"`
	AliasRemoved    bool `json:"aliasRemoved,omitempty"`
	RoutingChanged  bool `json:"routingChanged,omitempty"`
}

// Plan is the immutable preview/execute planning result.
type Plan struct {
	ContractVersion   string        `json:"contractVersion"`
	PlannerVersion    string        `json:"plannerVersion"`
	BaseRevision      string        `json:"baseRevision"`
	CandidateRevision string        `json:"candidateRevision,omitempty"`
	OperationKind     string        `json:"operationKind"`
	Executable        bool          `json:"executable"`
	NoOp              bool          `json:"noOp"`
	PlanToken         string        `json:"planToken,omitempty"`
	ExpiresAt         *time.Time    `json:"expiresAt,omitempty"`
	RequestedChanges  []Change      `json:"requestedChanges"`
	AutomaticChanges  []Change      `json:"automaticChanges"`
	SelectedChanges   []Change      `json:"selectedChanges"`
	Blockers          []Issue       `json:"blockers"`
	Choices           []Choice      `json:"choices"`
	PreservedIssues   []Issue       `json:"preservedIssues"`
	RuntimeImpact     RuntimeImpact `json:"runtimeImpact"`
}

// Result pairs a plan with an optional candidate config snapshot.
// Candidate is non-nil only when Executable is true (including no-op).
type Result struct {
	Plan      Plan
	Candidate *config.Config
}

// ExternalRefs carries optional weak external selectors (not local FKs).
type ExternalRefs struct {
	OpenCodeModel      string
	OpenCodeSmallModel string
}

// DiscoveryObservation describes one model-discovery outcome for reconcile planning.
type DiscoveryObservation struct {
	// Status is one of: skip, error, empty, incomplete, trusted_complete.
	Status string
	Models []string
}

// Operation is a typed lifecycle operation envelope.
type Operation struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// ProviderRemovePayload is the payload for OpProviderRemove.
type ProviderRemovePayload struct {
	ProviderID string `json:"providerId"`
}

// GroupRemovePayload is the payload for OpGroupRemove.
type GroupRemovePayload struct {
	ProviderID string `json:"providerId"`
	GroupID    string `json:"groupId"`
}

// GroupIDChangePayload is the payload for OpGroupIDChange.
// Identity rename only: never silently rebinds to default or a sibling group.
type GroupIDChangePayload struct {
	ProviderID string `json:"providerId"`
	OldGroupID string `json:"oldGroupId"`
	NewGroupID string `json:"newGroupId"`
}

// AliasRemovePayload is the payload for OpAliasRemove.
type AliasRemovePayload struct {
	Alias string `json:"alias"`
}

// AliasUpgradePayload is the payload for OpAliasUpgrade.
type AliasUpgradePayload struct {
	Alias string `json:"alias"`
}

// AliasMutatePayload gates manual-only mutations (bind/edit/toggle/unbind/reorder).
type AliasMutatePayload struct {
	Alias string `json:"alias"`
	// Intent is a stable label for the attempted mutation (bind/edit/...).
	Intent string `json:"intent"`
}

// DiscoveryReconcilePayload is the payload for OpDiscoverySync.
type DiscoveryReconcilePayload struct {
	ProviderID  string               `json:"providerId"`
	Observation DiscoveryObservation `json:"observation"`
}
