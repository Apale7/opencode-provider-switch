package app

import (
	"errors"
	"net/http"
	"testing"
)

func TestClassifyOutcomeOKAndRestartPending(t *testing.T) {
	t.Parallel()

	status, env := ClassifyOutcome(nil, map[string]bool{"ok": true})
	if status != http.StatusOK || !env.OK || env.Outcome.Code != "ok" {
		t.Fatalf("ok classify = status=%d env=%#v", status, env)
	}

	status, env = ClassifyOutcome(nil, LifecycleExecuteResult{PendingRestart: true})
	if status != http.StatusOK || !env.OK || env.Outcome.Code != "restart_pending" {
		t.Fatalf("restart_pending classify = status=%d env=%#v", status, env)
	}
}

func TestClassifyOutcomeRevisionConflict(t *testing.T) {
	t.Parallel()

	err := &OutcomeError{
		Code: "revision_conflict",
		Params: map[string]any{
			"expected": "a",
			"current":  "b",
		},
	}
	status, env := ClassifyOutcome(err, nil)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}
	if env.OK || env.Error != "revision_conflict" || env.Outcome.Code != "revision_conflict" {
		t.Fatalf("env = %#v", env)
	}
	if env.Outcome.Params["expected"] != "a" || env.Outcome.Retryable {
		t.Fatalf("params/retryable = %#v", env.Outcome)
	}
}

func TestClassifyOutcomeRuntimeApplyFailedKeepsData(t *testing.T) {
	t.Parallel()

	result := LifecycleExecuteResult{Persisted: true, RuntimeState: "apply_failed"}
	err := &OutcomeError{Code: "runtime_apply_failed", Err: errors.New("reload boom")}
	status, env := ClassifyOutcome(err, result)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	got, ok := env.Data.(LifecycleExecuteResult)
	if !ok || !got.Persisted || got.RuntimeState != "apply_failed" {
		t.Fatalf("data = %#v", env.Data)
	}
	if env.Error != "runtime_apply_failed" {
		t.Fatalf("error = %q", env.Error)
	}
}

func TestOutcomeErrorErrorIsStableCode(t *testing.T) {
	t.Parallel()

	err := &OutcomeError{Code: "plan_not_executable", Err: errors.New("detail")}
	if err.Error() != "plan_not_executable" {
		t.Fatalf("Error() = %q, want stable code only", err.Error())
	}
}

func TestHTTPStatusAndCLIExitMapping(t *testing.T) {
	t.Parallel()

	if HTTPStatusForOutcome("revision_required") != http.StatusPreconditionRequired {
		t.Fatalf("revision_required status = %d", HTTPStatusForOutcome("revision_required"))
	}
	if HTTPStatusForOutcome("plan_expired") != http.StatusGone {
		t.Fatalf("plan_expired status = %d", HTTPStatusForOutcome("plan_expired"))
	}
	if CLIExitCodeForOutcome("plan_not_executable") != 3 {
		t.Fatalf("plan_not_executable exit = %d", CLIExitCodeForOutcome("plan_not_executable"))
	}
	if CLIExitCodeForOutcome("runtime_apply_failed") != 8 {
		t.Fatalf("runtime_apply_failed exit = %d", CLIExitCodeForOutcome("runtime_apply_failed"))
	}
}
