package opencode

import (
	"fmt"
	"math"
	"reflect"
	"strings"
)

const (
	SyncDiffStatusNew       = "new"
	SyncDiffStatusChanged   = "changed"
	SyncDiffStatusUnchanged = "unchanged"
	SyncDiffStatusConflict  = "conflict"
	SyncDiffStatusFailed    = "failed"
)

var syncDiffFieldPaths = []string{
	"name",
	"limit.context",
	"limit.output",
	"cost.input",
	"cost.output",
	"cost.cacheRead",
	"cost.cacheWrite",
	"inputModalities",
	"outputModalities",
	"reasoning",
	"toolCall",
	"attachment",
	"temperature",
	"experimental",
	"variants",
	"status",
	"releaseDate",
}

// SyncDiffEntry describes the user-vs-proposed sync result for one model config field.
type SyncDiffEntry struct {
	Path          string
	UserValue     any
	ProposedValue any
	Status        string
	ConflictNote  string
	AutoDetected  bool
}

// AliasSyncDiff contains all sync diff entries for one OpenCode model alias.
type AliasSyncDiff struct {
	AliasName   string
	Protocol    string
	ProviderKey string
	Entries     []SyncDiffEntry
	Summary     DiffSummary
}

// DiffSummary counts sync diff entry statuses.
type DiffSummary struct {
	Total     int
	New       int
	Changed   int
	Unchanged int
	Conflict  int
	Failed    int
}

// ComputeSyncDiff compares a user's existing model config against a proposed auto-detected config.
func ComputeSyncDiff(aliasName, protocol string, userModelConfig map[string]any, proposedConfig map[string]any, probeErrors map[string]string) AliasSyncDiff {
	diff := AliasSyncDiff{
		AliasName: strings.TrimSpace(aliasName),
		Protocol:  strings.TrimSpace(protocol),
		Entries:   make([]SyncDiffEntry, 0, len(syncDiffFieldPaths)),
	}

	safeDefaults := syncDiffSafeDefaults(diff.Protocol, diff.AliasName)
	for _, path := range syncDiffFieldPaths {
		entry := computeSyncDiffEntry(path, userModelConfig, proposedConfig, probeErrors, safeDefaults)
		diff.Entries = append(diff.Entries, entry)
		addSyncDiffSummary(&diff.Summary, entry.Status)
	}

	return diff
}

func computeSyncDiffEntry(path string, userModelConfig map[string]any, proposedConfig map[string]any, probeErrors map[string]string, safeDefaults map[string]any) SyncDiffEntry {
	userValue, hasUserValue := nestedSyncDiffValue(userModelConfig, path)
	proposedValue, hasProposedValue := nestedSyncDiffValue(proposedConfig, path)
	probeError := strings.TrimSpace(probeErrors[path])

	entry := SyncDiffEntry{
		Path:         path,
		UserValue:    userValue,
		AutoDetected: hasProposedValue,
	}

	if probeError != "" {
		entry.ProposedValue = syncDiffFailedProposedValue(path, hasProposedValue, proposedValue, safeDefaults)
		if hasUserValue {
			entry.Status = SyncDiffStatusUnchanged
			return entry
		}
		entry.Status = SyncDiffStatusFailed
		entry.ConflictNote = probeError
		return entry
	}

	if !hasProposedValue {
		if hasUserValue {
			entry.ProposedValue = userValue
			entry.Status = SyncDiffStatusUnchanged
			return entry
		}
		entry.ProposedValue = syncDiffDefaultValue(path, safeDefaults)
		entry.Status = SyncDiffStatusFailed
		entry.ConflictNote = "missing proposed value"
		return entry
	}

	entry.ProposedValue = proposedValue
	if !hasUserValue {
		entry.Status = SyncDiffStatusNew
		return entry
	}
	if syncDiffValuesEqual(userValue, proposedValue) {
		entry.Status = SyncDiffStatusUnchanged
		return entry
	}
	entry.Status = SyncDiffStatusConflict
	entry.ConflictNote = fmt.Sprintf("user value differs from proposed value for %s; keeping user value", path)
	return entry
}

func nestedSyncDiffValue(values map[string]any, path string) (any, bool) {
	if values == nil {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = values
	for _, part := range parts {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func syncDiffSafeDefaults(protocol, aliasName string) map[string]any {
	defaults := flattenSyncDiffValues(SafeDefaultModelConfig(protocol, aliasName))
	defaults["cost.input"] = float64(0)
	defaults["cost.output"] = float64(0)
	defaults["cost.cacheRead"] = float64(0)
	defaults["cost.cacheWrite"] = float64(0)
	defaults["temperature"] = true
	defaults["experimental"] = false
	defaults["variants"] = []any{}
	defaults["status"] = ""
	defaults["releaseDate"] = ""
	return defaults
}

func flattenSyncDiffValues(values map[string]any) map[string]any {
	out := map[string]any{}
	for _, path := range syncDiffFieldPaths {
		if value, ok := nestedSyncDiffValue(values, path); ok {
			out[path] = value
		}
	}
	return out
}

func syncDiffDefaultValue(path string, safeDefaults map[string]any) any {
	if value, ok := safeDefaults[path]; ok {
		return value
	}
	return nil
}

func syncDiffFailedProposedValue(path string, hasProposedValue bool, proposedValue any, safeDefaults map[string]any) any {
	if hasProposedValue {
		return proposedValue
	}
	return syncDiffDefaultValue(path, safeDefaults)
}

func syncDiffValuesEqual(left any, right any) bool {
	if leftFloat, leftOK := syncDiffNumber(left); leftOK {
		if rightFloat, rightOK := syncDiffNumber(right); rightOK {
			return math.Abs(leftFloat-rightFloat) < 1e-9
		}
	}
	return reflect.DeepEqual(left, right)
}

func syncDiffNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func addSyncDiffSummary(summary *DiffSummary, status string) {
	summary.Total++
	switch status {
	case SyncDiffStatusNew:
		summary.New++
	case SyncDiffStatusChanged:
		summary.Changed++
	case SyncDiffStatusUnchanged:
		summary.Unchanged++
	case SyncDiffStatusConflict:
		summary.Conflict++
	case SyncDiffStatusFailed:
		summary.Failed++
	}
}
