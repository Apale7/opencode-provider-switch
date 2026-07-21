package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

// Presenter models adapt lifecycle/transport contracts for TUI screens.
// Screen interaction code must not parse err.Error() for business classification.

const (
	// Max displayed length for entity IDs / path fragments (control-char stripped).
	presenterIDMaxRunes = 64
)

// TransportMessage is a pure display model for one classified transport outcome.
type TransportMessage struct {
	Code      string
	Params    map[string]any
	Retryable bool
	// OK mirrors envelope.ok (restart_pending is OK).
	OK bool
	// Data holds optional payload (e.g. execute result on runtime_apply_failed).
	Data any
	// Kind guides screen routing without localizing here.
	// One of: success, validation, conflict, blocked, not_found, io, apply_failed, restart, auth, internal.
	Kind string
}

// LifecyclePlanPresentation groups plan fields for impact / confirm views.
type LifecyclePlanPresentation struct {
	BaseRevision      string
	CandidateRevision string
	OperationKind     string
	Executable        bool
	NoOp              bool
	PlanToken         string
	Requested         []LifecycleChangeLine
	Automatic         []LifecycleChangeLine
	Selected          []LifecycleChangeLine
	Blockers          []LifecycleIssueLine
	Choices           []LifecycleChoiceLine
	Preserved         []LifecycleIssueLine
	RuntimeImpact     LifecycleRuntimeImpactLine
}

// LifecycleChangeLine is one planned change row.
type LifecycleChangeLine struct {
	ID         string
	Kind       string
	Source     string
	Entity     string
	ReasonCode string
	Path       string
	Params     map[string]any
}

// LifecycleIssueLine is one blocker / preserved issue row.
type LifecycleIssueLine struct {
	ID          string
	Code        string
	Disposition string
	Path        string
	Params      map[string]any
}

// LifecycleChoiceOptionLine is one concrete option under a required choice.
type LifecycleChoiceOptionLine struct {
	ID     string
	Params map[string]any
}

// LifecycleChoiceLine is one required user choice.
type LifecycleChoiceLine struct {
	ID      string
	Code    string
	Path    string
	Params  map[string]any
	Options []LifecycleChoiceOptionLine
}

// LifecycleRuntimeImpactLine summarizes runtime-facing effects.
type LifecycleRuntimeImpactLine struct {
	ProviderRemoved bool
	AliasRemoved    bool
	RoutingChanged  bool
}

// LifecycleExecutePresentation is the post-execute summary model.
type LifecycleExecutePresentation struct {
	BaseRevision            string
	CommittedRevision       string
	RuntimeRevision         string
	Persisted               bool
	WritePerformed          bool
	Changed                 bool
	NoOp                    bool
	CandidateAlreadyPresent bool
	RuntimeApplied          bool
	PendingRestart          bool
	RuntimeState            string
	Issues                  []LifecycleIssueLine
	Plan                    LifecyclePlanPresentation
}

// TransportMessageFromEnvelope maps APIEnvelope to a display message.
func TransportMessageFromEnvelope(env app.APIEnvelope) TransportMessage {
	code := strings.TrimSpace(env.Outcome.Code)
	if code == "" {
		code = strings.TrimSpace(env.Error)
	}
	if code == "" {
		if env.OK {
			code = "ok"
		} else {
			code = "internal_error"
		}
	}
	params := env.Outcome.Params
	if params == nil {
		params = map[string]any{}
	}
	return TransportMessage{
		Code:      code,
		Params:    params,
		Retryable: env.Outcome.Retryable,
		OK:        env.OK,
		Data:      env.Data,
		Kind:      transportKind(code, env.OK),
	}
}

// TransportMessageFromError classifies a Go error via the shared outcome mapper.
// Framework/bridge faults without OutcomeError become Kind=internal.
func TransportMessageFromError(err error, data any) TransportMessage {
	_, env := app.ClassifyOutcome(err, data)
	return TransportMessageFromEnvelope(env)
}

func transportKind(code string, ok bool) string {
	switch code {
	case "ok":
		return "success"
	case "restart_pending":
		return "restart"
	case "revision_conflict", "preparation_stale":
		return "conflict"
	case "plan_not_executable", "plan_mismatch":
		return "blocked"
	case "plan_expired":
		return "blocked"
	case "not_found":
		return "not_found"
	case "validation_failed", "invalid_request", "revision_required",
		"candidate_invalid", "runtime_candidate_build_failed", "method_not_allowed":
		return "validation"
	case "runtime_apply_failed":
		return "apply_failed"
	case "config_io_failed", "persist_failed", "config_store_busy":
		return "io"
	case "unauthenticated", "forbidden":
		return "auth"
	default:
		if ok {
			return "success"
		}
		return "internal"
	}
}

// PresentLifecyclePlan adapts LifecyclePlanView for TUI grouping.
func PresentLifecyclePlan(plan app.LifecyclePlanView) LifecyclePlanPresentation {
	return LifecyclePlanPresentation{
		BaseRevision:      sanitizeDisplayID(string(plan.BaseRevision)),
		CandidateRevision: sanitizeDisplayID(string(plan.CandidateRevision)),
		OperationKind:     sanitizeDisplayID(plan.OperationKind),
		Executable:        plan.Executable,
		NoOp:              plan.NoOp,
		PlanToken:         sanitizeDisplayID(plan.PlanToken),
		Requested:         presentChanges(plan.RequestedChanges),
		Automatic:         presentChanges(plan.AutomaticChanges),
		Selected:          presentChanges(plan.SelectedChanges),
		Blockers:          presentIssues(plan.Blockers),
		Choices:           presentChoices(plan.Choices),
		Preserved:         presentIssues(plan.PreservedIssues),
		RuntimeImpact: LifecycleRuntimeImpactLine{
			ProviderRemoved: plan.RuntimeImpact.ProviderRemoved,
			AliasRemoved:    plan.RuntimeImpact.AliasRemoved,
			RoutingChanged:  plan.RuntimeImpact.RoutingChanged,
		},
	}
}

// PresentLifecycleExecute adapts LifecycleExecuteResult for TUI summary.
func PresentLifecycleExecute(result app.LifecycleExecuteResult) LifecycleExecutePresentation {
	runtimeRev := ""
	if result.RuntimeRevision != nil {
		runtimeRev = string(*result.RuntimeRevision)
	}
	return LifecycleExecutePresentation{
		BaseRevision:            sanitizeDisplayID(string(result.BaseRevision)),
		CommittedRevision:       sanitizeDisplayID(string(result.CommittedRevision)),
		RuntimeRevision:         sanitizeDisplayID(runtimeRev),
		Persisted:               result.Persisted,
		WritePerformed:          result.WritePerformed,
		Changed:                 result.Changed,
		NoOp:                    result.NoOp,
		CandidateAlreadyPresent: result.CandidateAlreadyPresent,
		RuntimeApplied:          result.RuntimeApplied,
		PendingRestart:          result.PendingRestart,
		RuntimeState:            sanitizeDisplayID(result.RuntimeState),
		Issues:                  presentIssues(result.Issues),
		Plan:                    PresentLifecyclePlan(result.Plan),
	}
}

func presentChanges(items []lifecycle.Change) []LifecycleChangeLine {
	if len(items) == 0 {
		return nil
	}
	out := make([]LifecycleChangeLine, 0, len(items))
	for _, c := range items {
		out = append(out, LifecycleChangeLine{
			ID:         sanitizeDisplayID(c.ID),
			Kind:       sanitizeDisplayID(c.Kind),
			Source:     sanitizeDisplayID(c.Source),
			Entity:     sanitizeDisplayID(c.Entity),
			ReasonCode: sanitizeDisplayID(c.ReasonCode),
			Path:       sanitizeDisplayID(c.Path),
			Params:     sanitizeParams(c.Params),
		})
	}
	return out
}

func presentIssues(items []lifecycle.Issue) []LifecycleIssueLine {
	if len(items) == 0 {
		return nil
	}
	out := make([]LifecycleIssueLine, 0, len(items))
	for _, issue := range items {
		out = append(out, LifecycleIssueLine{
			ID:          sanitizeDisplayID(issue.ID),
			Code:        sanitizeDisplayID(issue.Code),
			Disposition: sanitizeDisplayID(issue.Disposition),
			Path:        sanitizeDisplayID(issue.Path),
			Params:      sanitizeParams(issue.Params),
		})
	}
	return out
}

func presentChoices(items []lifecycle.Choice) []LifecycleChoiceLine {
	if len(items) == 0 {
		return nil
	}
	out := make([]LifecycleChoiceLine, 0, len(items))
	for _, ch := range items {
		opts := make([]LifecycleChoiceOptionLine, 0, len(ch.Options))
		for _, opt := range ch.Options {
			opts = append(opts, LifecycleChoiceOptionLine{
				ID:     sanitizeDisplayID(opt.ID),
				Params: sanitizeParams(opt.Params),
			})
		}
		out = append(out, LifecycleChoiceLine{
			ID:      sanitizeDisplayID(ch.ID),
			Code:    sanitizeDisplayID(ch.Code),
			Path:    sanitizeDisplayID(ch.Path),
			Params:  sanitizeParams(ch.Params),
			Options: opts,
		})
	}
	return out
}

// sanitizeDisplayID strips control characters / ANSI-like escapes and truncates.
// Used for entity IDs and path fragments shown in human TUI/CLI lines.
func sanitizeDisplayID(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	runes := 0
	for _, r := range raw {
		if r == '\u001b' || r == '\u009b' {
			// Drop ESC and CSI introducer; skip following CSI parameter bytes best-effort.
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if runes >= presenterIDMaxRunes {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		runes++
	}
	return b.String()
}

func sanitizeParams(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		key := sanitizeDisplayID(k)
		switch val := v.(type) {
		case string:
			out[key] = sanitizeDisplayID(val)
		case fmt.Stringer:
			out[key] = sanitizeDisplayID(val.String())
		default:
			out[key] = v
		}
	}
	return out
}

// OutcomeI18nKey returns the stable i18n key for a transport outcome code.
func OutcomeI18nKey(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "transport.outcome.internal_error"
	}
	return "transport.outcome." + code
}

// Ensure utf8 import used when validating display width helpers later.
var _ = utf8.RuneCountInString
