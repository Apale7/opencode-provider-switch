package tui

import (
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func TestTransportMessageFromEnvelopeKinds(t *testing.T) {
	tests := []struct {
		code string
		ok   bool
		kind string
	}{
		{"ok", true, "success"},
		{"restart_pending", true, "restart"},
		{"revision_conflict", false, "conflict"},
		{"plan_not_executable", false, "blocked"},
		{"not_found", false, "not_found"},
		{"runtime_apply_failed", false, "apply_failed"},
		{"validation_failed", false, "validation"},
		{"config_io_failed", false, "io"},
		{"forbidden", false, "auth"},
		{"internal_error", false, "internal"},
	}
	for _, tt := range tests {
		msg := TransportMessageFromEnvelope(app.APIEnvelope{
			OK: tt.ok,
			Error: func() string {
				if tt.ok {
					return ""
				}
				return tt.code
			}(),
			Outcome: app.TransportOutcome{Code: tt.code, Params: map[string]any{}},
		})
		if msg.Kind != tt.kind {
			t.Fatalf("code %q kind = %q, want %q", tt.code, msg.Kind, tt.kind)
		}
		if msg.Code != tt.code {
			t.Fatalf("code = %q, want %q", msg.Code, tt.code)
		}
	}
}

func TestTransportMessageFromErrorRevisionConflict(t *testing.T) {
	err := &app.OutcomeError{
		Code: "revision_conflict",
		Params: map[string]any{
			"expected": "a",
			"current":  "b",
		},
	}
	msg := TransportMessageFromError(err, nil)
	if msg.Kind != "conflict" {
		t.Fatalf("kind = %q", msg.Kind)
	}
	if msg.Code != "revision_conflict" {
		t.Fatalf("code = %q", msg.Code)
	}
	if msg.OK {
		t.Fatalf("expected not ok")
	}
}

func TestPresentLifecyclePlanGroups(t *testing.T) {
	plan := app.LifecyclePlanView{
		BaseRevision:      "base-1",
		CandidateRevision: "cand-1",
		OperationKind:     lifecycle.OpProviderRemove,
		Executable:        false,
		RequestedChanges: []lifecycle.Change{
			{ID: "c1", Kind: "remove", Source: "requested", Entity: "provider:p1", ReasonCode: "user_request"},
		},
		AutomaticChanges: []lifecycle.Change{
			{ID: "c2", Kind: "remove", Source: "automatic", Entity: "target:a/p1", ReasonCode: "auto_target"},
		},
		Blockers: []lifecycle.Issue{
			{ID: "b1", Code: "protected_target", Disposition: "blocker", Path: "aliases.a"},
		},
		Choices: []lifecycle.Choice{
			{
				ID:   "ch1",
				Code: "resolve_protected_target",
				Options: []lifecycle.ChoiceOption{
					{ID: "remove_target"},
					{ID: "rebind"},
				},
			},
		},
		PreservedIssues: []lifecycle.Issue{
			{ID: "p1", Code: "catalog_stale", Disposition: "preserved"},
		},
		RuntimeImpact: lifecycle.RuntimeImpact{ProviderRemoved: true, RoutingChanged: true},
	}
	view := PresentLifecyclePlan(plan)
	if !view.RuntimeImpact.ProviderRemoved || !view.RuntimeImpact.RoutingChanged {
		t.Fatalf("runtime impact not mapped")
	}
	if len(view.Requested) != 1 || view.Requested[0].Entity != "provider:p1" {
		t.Fatalf("requested = %+v", view.Requested)
	}
	if len(view.Automatic) != 1 {
		t.Fatalf("automatic len = %d", len(view.Automatic))
	}
	if len(view.Blockers) != 1 || view.Blockers[0].Code != "protected_target" {
		t.Fatalf("blockers = %+v", view.Blockers)
	}
	if len(view.Choices) != 1 || len(view.Choices[0].Options) != 2 {
		t.Fatalf("choices = %+v", view.Choices)
	}
	if len(view.Preserved) != 1 {
		t.Fatalf("preserved len = %d", len(view.Preserved))
	}
}

func TestSanitizeDisplayIDStripsControlAndANSI(t *testing.T) {
	raw := "prov\x1b[31mid\x00evil"
	got := sanitizeDisplayID(raw)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\x00") {
		t.Fatalf("control chars remain: %q", got)
	}
	if !strings.Contains(got, "provid") && !strings.Contains(got, "evil") {
		// After stripping CSI-ish bytes we still keep printable runes.
		t.Fatalf("unexpected sanitize result %q", got)
	}
	long := strings.Repeat("a", 80)
	if got := sanitizeDisplayID(long); utf8SafeLen(got) > presenterIDMaxRunes+1 {
		t.Fatalf("truncation failed: len=%d got=%q", utf8SafeLen(got), got)
	}
}

func utf8SafeLen(s string) int {
	return len([]rune(s))
}

func TestOutcomeI18nKey(t *testing.T) {
	if got := OutcomeI18nKey("revision_conflict"); got != "transport.outcome.revision_conflict" {
		t.Fatalf("key = %q", got)
	}
	if got := OutcomeI18nKey(""); got != "transport.outcome.internal_error" {
		t.Fatalf("empty key = %q", got)
	}
}

func TestPresentLifecycleExecute(t *testing.T) {
	rev := app.ConfigRevision("rt-1")
	result := app.LifecycleExecuteResult{
		BaseRevision:      "base",
		CommittedRevision: "commit",
		RuntimeRevision:   &rev,
		Persisted:         true,
		WritePerformed:    true,
		Changed:           true,
		RuntimeApplied:    false,
		PendingRestart:    false,
		RuntimeState:      "apply_failed",
		Plan: app.LifecyclePlanView{
			BaseRevision:  "base",
			OperationKind: lifecycle.OpProviderRemove,
			Executable:    true,
		},
	}
	view := PresentLifecycleExecute(result)
	if view.RuntimeState != "apply_failed" {
		t.Fatalf("runtimeState = %q", view.RuntimeState)
	}
	if view.RuntimeRevision != "rt-1" {
		t.Fatalf("runtimeRevision = %q", view.RuntimeRevision)
	}
	if !view.Persisted || view.RuntimeApplied {
		t.Fatalf("flags persisted=%v applied=%v", view.Persisted, view.RuntimeApplied)
	}
}
