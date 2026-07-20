package opencode

import "testing"

func TestComputeSyncDiffAllFieldsNew(t *testing.T) {
	diff := ComputeSyncDiff("gpt-test", "openai-responses", map[string]any{}, fullSyncDiffConfig("gpt-test"), nil)

	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, New: 17})
	assertAllSyncDiffEntriesHaveStatus(t, diff, SyncDiffStatusNew)
	assertSyncDiffChangedStatusExists(t)
}

func TestComputeSyncDiffAllFieldsUnchanged(t *testing.T) {
	config := fullSyncDiffConfig("gpt-test")
	diff := ComputeSyncDiff("gpt-test", "openai-responses", config, config, nil)

	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, Unchanged: 17})
	assertAllSyncDiffEntriesHaveStatus(t, diff, SyncDiffStatusUnchanged)
}

func TestComputeSyncDiffUserValueConflicts(t *testing.T) {
	diff := ComputeSyncDiff("gpt-test", "openai-responses", conflictingSyncDiffConfig(), fullSyncDiffConfig("gpt-test"), nil)

	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, Conflict: 17})
	assertAllSyncDiffEntriesHaveStatus(t, diff, SyncDiffStatusConflict)
	for _, entry := range diff.Entries {
		if entry.ConflictNote == "" {
			t.Fatalf("entry %s ConflictNote is empty, want conflict note", entry.Path)
		}
	}
}

func TestComputeSyncDiffProbeFailures(t *testing.T) {
	probeErrors := map[string]string{}
	for _, path := range syncDiffFieldPaths {
		probeErrors[path] = "probe failed"
	}

	diff := ComputeSyncDiff("gpt-test", "openai-responses", nil, nil, probeErrors)

	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, Failed: 17})
	assertAllSyncDiffEntriesHaveStatus(t, diff, SyncDiffStatusFailed)
	assertSyncDiffEntry(t, diff, "limit.context", SyncDiffStatusFailed, int64(SafeDefaultContextLimit), true)
	assertSyncDiffEntry(t, diff, "limit.output", SyncDiffStatusFailed, int64(SafeDefaultOutputLimit), true)
}

func TestComputeSyncDiffMixedScenario(t *testing.T) {
	userConfig := map[string]any{
		"name": "gpt-test",
		"limit": map[string]any{
			"context": 64000,
			"output":  4096,
		},
	}
	probeErrors := map[string]string{
		"limit.output": "output probe failed",
		"cost.output":  "cost probe failed",
	}

	diff := ComputeSyncDiff("gpt-test", "openai-responses", userConfig, fullSyncDiffConfig("gpt-test"), probeErrors)

	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, New: 13, Unchanged: 2, Conflict: 1, Failed: 1})
	assertSyncDiffEntry(t, diff, "name", SyncDiffStatusUnchanged, "gpt-test", false)
	assertSyncDiffEntry(t, diff, "limit.context", SyncDiffStatusConflict, int64(128000), false)
	assertSyncDiffEntry(t, diff, "limit.output", SyncDiffStatusUnchanged, int64(4096), false)
	assertSyncDiffEntry(t, diff, "cost.input", SyncDiffStatusNew, 0.001, false)
	assertSyncDiffEntry(t, diff, "cost.output", SyncDiffStatusFailed, 0.002, true)
}

func TestComputeSyncDiffEmptyUserConfig(t *testing.T) {
	diff := ComputeSyncDiff(" gpt-test ", " openai-responses ", nil, fullSyncDiffConfig("gpt-test"), nil)

	if diff.AliasName != "gpt-test" {
		t.Fatalf("AliasName = %q, want gpt-test", diff.AliasName)
	}
	if diff.Protocol != "openai-responses" {
		t.Fatalf("Protocol = %q, want openai-responses", diff.Protocol)
	}
	assertSyncDiffSummary(t, diff.Summary, DiffSummary{Total: 17, New: 17})
	assertAllSyncDiffEntriesHaveStatus(t, diff, SyncDiffStatusNew)
}

func fullSyncDiffConfig(modelName string) map[string]any {
	return map[string]any{
		"name": modelName,
		"limit": map[string]any{
			"context": int64(128000),
			"output":  int64(4096),
		},
		"cost": map[string]any{
			"input":      0.001,
			"output":     0.002,
			"cacheRead":  0.0005,
			"cacheWrite": 0.00075,
		},
		"inputModalities":  []any{"text", "image"},
		"outputModalities": []any{"text"},
		"reasoning":        true,
		"toolCall":         true,
		"attachment":       true,
		"temperature":      true,
		"experimental":     false,
		"variants":         []any{"fast", "quality"},
		"status":           "stable",
		"releaseDate":      "2026-01-01",
	}
}

func conflictingSyncDiffConfig() map[string]any {
	return map[string]any{
		"name": "user-model",
		"limit": map[string]any{
			"context": 32000,
			"output":  2048,
		},
		"cost": map[string]any{
			"input":      0.01,
			"output":     0.02,
			"cacheRead":  0.005,
			"cacheWrite": 0.0075,
		},
		"inputModalities":  []any{"text"},
		"outputModalities": []any{"audio"},
		"reasoning":        false,
		"toolCall":         false,
		"attachment":       false,
		"temperature":      false,
		"experimental":     true,
		"variants":         []any{"legacy"},
		"status":           "beta",
		"releaseDate":      "2025-01-01",
	}
}

func assertSyncDiffSummary(t *testing.T, got DiffSummary, want DiffSummary) {
	t.Helper()
	if got.Total != want.Total {
		t.Fatalf("Summary.Total = %d, want %d", got.Total, want.Total)
	}
	if got.New != want.New {
		t.Fatalf("Summary.New = %d, want %d", got.New, want.New)
	}
	if got.Changed != 0 {
		t.Fatalf("Summary.Changed = %d, want 0", got.Changed)
	}
	if got.Unchanged != want.Unchanged {
		t.Fatalf("Summary.Unchanged = %d, want %d", got.Unchanged, want.Unchanged)
	}
	if got.Conflict != want.Conflict {
		t.Fatalf("Summary.Conflict = %d, want %d", got.Conflict, want.Conflict)
	}
	if got.Failed != want.Failed {
		t.Fatalf("Summary.Failed = %d, want %d", got.Failed, want.Failed)
	}
}

func assertAllSyncDiffEntriesHaveStatus(t *testing.T, diff AliasSyncDiff, wantStatus string) {
	t.Helper()
	if len(diff.Entries) != 17 {
		t.Fatalf("len(Entries) = %d, want 17", len(diff.Entries))
	}
	for _, entry := range diff.Entries {
		if entry.Status != wantStatus {
			t.Fatalf("entry %s Status = %q, want %q", entry.Path, entry.Status, wantStatus)
		}
	}
}

func assertSyncDiffEntry(t *testing.T, diff AliasSyncDiff, path string, wantStatus string, wantProposedValue any, wantConflictNote bool) {
	t.Helper()
	for _, entry := range diff.Entries {
		if entry.Path != path {
			continue
		}
		if entry.Status != wantStatus {
			t.Fatalf("entry %s Status = %q, want %q", path, entry.Status, wantStatus)
		}
		if !syncDiffValuesEqual(entry.ProposedValue, wantProposedValue) {
			t.Fatalf("entry %s ProposedValue = %#v, want %#v", path, entry.ProposedValue, wantProposedValue)
		}
		if wantConflictNote && entry.ConflictNote == "" {
			t.Fatalf("entry %s ConflictNote is empty, want note", path)
		}
		if !wantConflictNote && entry.ConflictNote != "" && wantStatus != SyncDiffStatusConflict {
			t.Fatalf("entry %s ConflictNote = %q, want empty", path, entry.ConflictNote)
		}
		return
	}
	t.Fatalf("entry %s not found", path)
}

func assertSyncDiffChangedStatusExists(t *testing.T) {
	t.Helper()
	if SyncDiffStatusChanged != "changed" {
		t.Fatalf("SyncDiffStatusChanged = %q, want changed", SyncDiffStatusChanged)
	}
	var summary DiffSummary
	summary.Changed = 0
}
