package app

import (
	"errors"
	"net/http"
)

// TransportOutcome is the stable multi-client outcome envelope field (artifact 06).
type TransportOutcome struct {
	Code      string         `json:"code"`
	Params    map[string]any `json:"params"`
	Retryable bool           `json:"retryable"`
}

// APIEnvelope is the canonical HTTP/Wails transport envelope.
// Legacy clients keep reading data/error; ok/outcome are additive.
type APIEnvelope struct {
	OK      bool             `json:"ok"`
	Data    any              `json:"data,omitempty"`
	Error   string           `json:"error,omitempty"`
	Outcome TransportOutcome `json:"outcome"`
}

// ClassifyOutcome maps domain/transport errors to stable outcome + HTTP status.
// data may still be present on classified failures (e.g. runtime_apply_failed).
func ClassifyOutcome(err error, data any) (status int, env APIEnvelope) {
	if err == nil {
		code := "ok"
		if pendingRestartFromData(data) {
			code = "restart_pending"
		}
		return http.StatusOK, APIEnvelope{
			OK:   true,
			Data: data,
			Outcome: TransportOutcome{
				Code:      code,
				Params:    map[string]any{},
				Retryable: false,
			},
		}
	}

	var outcome *OutcomeError
	if errors.As(err, &outcome) && outcome != nil && outcome.Code != "" {
		code := outcome.Code
		params := outcome.Params
		if params == nil {
			params = map[string]any{}
		}
		status = HTTPStatusForOutcome(code)
		env = APIEnvelope{
			OK:    false,
			Error: code,
			Outcome: TransportOutcome{
				Code:      code,
				Params:    params,
				Retryable: code == "config_store_busy",
			},
		}
		// Preserve execute/result payloads on post-commit apply failure.
		if code == "runtime_apply_failed" && data != nil {
			env.Data = data
		}
		// Some execute paths return partial result alongside OutcomeError.
		if env.Data == nil && data != nil && (code == "runtime_apply_failed" || code == "persist_failed") {
			env.Data = data
		}
		return status, env
	}

	// Legacy plain errors keep HTTP 400 + message for existing clients.
	// outcome.code stays a stable machine value; message is only in error for back-compat.
	msg := err.Error()
	if msg == "" {
		msg = "internal_error"
	}
	return http.StatusBadRequest, APIEnvelope{
		OK:    false,
		Error: msg,
		Outcome: TransportOutcome{
			Code:      "internal_error",
			Params:    map[string]any{},
			Retryable: false,
		},
	}
}

// HTTPStatusForOutcome implements artifact 06 status mapping.
func HTTPStatusForOutcome(code string) int {
	switch code {
	case "ok", "restart_pending":
		return http.StatusOK
	case "invalid_request":
		return http.StatusBadRequest
	case "unauthenticated":
		return http.StatusUnauthorized
	case "forbidden":
		return http.StatusForbidden
	case "not_found":
		return http.StatusNotFound
	case "method_not_allowed":
		return http.StatusMethodNotAllowed
	case "revision_conflict", "preparation_stale":
		return http.StatusConflict
	case "plan_expired":
		return http.StatusGone
	case "revision_required":
		return http.StatusPreconditionRequired
	case "validation_failed", "plan_not_executable", "plan_mismatch",
		"candidate_invalid", "runtime_candidate_build_failed":
		return http.StatusUnprocessableEntity
	case "config_store_busy":
		return http.StatusServiceUnavailable
	case "config_io_failed", "persist_failed", "runtime_apply_failed", "internal_error":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// CLIExitCodeForOutcome implements artifact 06 CLI exit mapping.
func CLIExitCodeForOutcome(code string) int {
	switch code {
	case "ok", "restart_pending":
		return 0
	case "unauthenticated", "forbidden", "internal_error":
		return 1
	case "validation_failed", "invalid_request", "method_not_allowed",
		"revision_required", "plan_mismatch", "plan_expired", "candidate_invalid":
		return 2
	case "plan_not_executable":
		return 3
	case "revision_conflict", "preparation_stale":
		return 4
	case "not_found":
		return 5
	case "config_io_failed", "persist_failed", "config_store_busy":
		return 6
	case "runtime_candidate_build_failed":
		return 7
	case "runtime_apply_failed":
		return 8
	default:
		return 1
	}
}

func pendingRestartFromData(data any) bool {
	switch v := data.(type) {
	case LifecycleExecuteResult:
		return v.PendingRestart
	case *LifecycleExecuteResult:
		return v != nil && v.PendingRestart
	default:
		return false
	}
}
