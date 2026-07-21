package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

// jsonOutput is set from root persistent --json.
var jsonOutput bool

// exitError carries a process exit code for classified CLI failures.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string {
	if e == nil {
		return ""
	}
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit %d", e.code)
}

func exitCodeOf(err error) int {
	var ee *exitError
	if errors.As(err, &ee) && ee != nil {
		return ee.code
	}
	if err == nil {
		return 0
	}
	_, env := app.ClassifyOutcome(err, nil)
	return app.CLIExitCodeForOutcome(env.Outcome.Code)
}

func writeJSONEnvelope(w io.Writer, env app.APIEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		return &exitError{code: 1, msg: "json_encode_failed"}
	}
	return nil
}

func humanOutcomeSummary(code string) string {
	switch code {
	case "ok":
		return "ok"
	case "restart_pending":
		return "config saved; restart required for full runtime apply"
	case "revision_conflict":
		return "config changed elsewhere; re-preview and retry"
	case "revision_required":
		return "revision is required"
	case "plan_not_executable":
		return "plan has blockers or unresolved choices"
	case "plan_mismatch":
		return "plan token does not match current preview"
	case "plan_expired":
		return "plan token expired; preview again"
	case "runtime_apply_failed":
		return "config saved, but runtime was not applied"
	case "not_found":
		return "resource not found"
	case "invalid_request":
		return "invalid request"
	case "validation_failed":
		return "validation failed"
	case "config_store_busy":
		return "config store busy; retry"
	case "persist_failed", "config_io_failed":
		return "config I/O failed"
	default:
		if code == "" {
			return "internal error"
		}
		return code
	}
}

func writeHumanError(w io.Writer, code string, detail string) {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "internal_error"
	}
	summary := humanOutcomeSummary(code)
	detail = sanitizeCLIText(detail)
	if detail != "" && detail != code && detail != summary {
		fmt.Fprintf(w, "error[%s]: %s (%s)\n", code, summary, detail)
		return
	}
	fmt.Fprintf(w, "error[%s]: %s\n", code, summary)
}

// finishOutcome prints classified result and returns exitError when failed.
func finishOutcome(cmd *cobra.Command, err error, data any) error {
	_, env := app.ClassifyOutcome(err, data)
	code := env.Outcome.Code
	if jsonOutput {
		if werr := writeJSONEnvelope(cmd.OutOrStdout(), env); werr != nil {
			return werr
		}
		if env.OK {
			return nil
		}
		return &exitError{code: app.CLIExitCodeForOutcome(code)}
	}
	if env.OK {
		return nil
	}
	detail := ""
	if env.Error != "" && env.Error != code {
		detail = env.Error
	}
	writeHumanError(cmd.ErrOrStderr(), code, detail)
	if code == "runtime_apply_failed" {
		fmt.Fprintln(cmd.ErrOrStderr(), "note: configuration was persisted; runtime still needs convergence")
	}
	return &exitError{code: app.CLIExitCodeForOutcome(code)}
}

func printPlanHuman(w io.Writer, plan app.LifecyclePlanView) {
	fmt.Fprintf(w, "plan operation=%s executable=%v noOp=%v base=%s\n",
		sanitizeCLIText(plan.OperationKind), plan.Executable, plan.NoOp, sanitizeCLIText(string(plan.BaseRevision)))
	printChangeGroup(w, "requested", plan.RequestedChanges)
	printChangeGroup(w, "automatic", plan.AutomaticChanges)
	printChangeGroup(w, "selected", plan.SelectedChanges)
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(w, "blockers:")
		for _, b := range plan.Blockers {
			fmt.Fprintf(w, "  - %s path=%s disposition=%s\n",
				sanitizeCLIText(b.Code), sanitizeCLIText(b.Path), sanitizeCLIText(b.Disposition))
		}
	}
	if len(plan.Choices) > 0 {
		fmt.Fprintln(w, "choices:")
		for _, ch := range plan.Choices {
			opts := make([]string, 0, len(ch.Options))
			for _, o := range ch.Options {
				opts = append(opts, sanitizeCLIText(o.ID))
			}
			fmt.Fprintf(w, "  - %s id=%s options=[%s]\n",
				sanitizeCLIText(ch.Code), sanitizeCLIText(ch.ID), strings.Join(opts, ", "))
		}
	}
	if len(plan.PreservedIssues) > 0 {
		fmt.Fprintln(w, "preserved:")
		for _, p := range plan.PreservedIssues {
			fmt.Fprintf(w, "  - %s path=%s\n", sanitizeCLIText(p.Code), sanitizeCLIText(p.Path))
		}
	}
	if plan.RuntimeImpact.ProviderRemoved || plan.RuntimeImpact.AliasRemoved || plan.RuntimeImpact.RoutingChanged {
		fmt.Fprintf(w, "runtimeImpact providerRemoved=%v aliasRemoved=%v routingChanged=%v\n",
			plan.RuntimeImpact.ProviderRemoved, plan.RuntimeImpact.AliasRemoved, plan.RuntimeImpact.RoutingChanged)
	}
}

func printChangeGroup(w io.Writer, name string, items []lifecycle.Change) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "%s changes (%d):\n", name, len(items))
	for _, c := range items {
		fmt.Fprintf(w, "  - %s %s entity=%s reason=%s\n",
			sanitizeCLIText(c.Kind), sanitizeCLIText(c.Source), sanitizeCLIText(c.Entity), sanitizeCLIText(c.ReasonCode))
	}
}

func printExecuteHuman(w io.Writer, result app.LifecycleExecuteResult) {
	fmt.Fprintf(w, "executed persisted=%v writePerformed=%v changed=%v noOp=%v runtimeState=%s\n",
		result.Persisted, result.WritePerformed, result.Changed, result.NoOp, sanitizeCLIText(result.RuntimeState))
	fmt.Fprintf(w, "revisions base=%s committed=%s\n",
		sanitizeCLIText(string(result.BaseRevision)), sanitizeCLIText(string(result.CommittedRevision)))
	if result.PendingRestart {
		fmt.Fprintln(w, "warning: restart pending for full runtime apply")
	}
	printPlanHuman(w, result.Plan)
}

func sanitizeCLIText(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	n := 0
	for _, r := range raw {
		if r == '\u001b' || r == '\u009b' || unicode.IsControl(r) {
			continue
		}
		if n >= 96 {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func marshalPayload(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func previewRemoveProvider(cmd *cobra.Command, id string) (app.ConfigRevision, app.LifecyclePlanView, error) {
	svc := appService()
	rev, err := svc.GetConfigRevision(cmd.Context())
	if err != nil {
		return "", app.LifecyclePlanView{}, err
	}
	payload, err := marshalPayload(lifecycle.ProviderRemovePayload{ProviderID: id})
	if err != nil {
		return "", app.LifecyclePlanView{}, err
	}
	plan, err := svc.PreviewLifecycle(cmd.Context(), app.LifecyclePreviewInput{
		Revision:  rev,
		Operation: lifecycle.Operation{Kind: lifecycle.OpProviderRemove, Payload: payload},
	})
	return rev, plan, err
}

func previewRemoveAliasWithSelections(cmd *cobra.Command, name string) (app.ConfigRevision, app.LifecyclePlanView, []lifecycle.Selection, error) {
	svc := appService()
	rev, err := svc.GetConfigRevision(cmd.Context())
	if err != nil {
		return "", app.LifecyclePlanView{}, nil, err
	}
	payload, err := marshalPayload(lifecycle.AliasRemovePayload{Alias: name})
	if err != nil {
		return "", app.LifecyclePlanView{}, nil, err
	}
	op := lifecycle.Operation{Kind: lifecycle.OpAliasRemove, Payload: payload}
	// First pass discovers rewrite impacts; convenience path keeps rules.
	basePlan, err := svc.PreviewLifecycle(cmd.Context(), app.LifecyclePreviewInput{
		Revision:  rev,
		Operation: op,
	})
	if err != nil {
		return rev, basePlan, nil, err
	}
	selections := make([]lifecycle.Selection, 0, len(basePlan.Choices))
	for _, ch := range basePlan.Choices {
		if ch.Code == lifecycle.ReasonRewriteSelectorImpact {
			selections = append(selections, lifecycle.Selection{ChoiceID: ch.ID, OptionID: lifecycle.OptionKeepRule})
		}
	}
	if len(selections) == 0 {
		return rev, basePlan, nil, nil
	}
	plan, err := svc.PreviewLifecycle(cmd.Context(), app.LifecyclePreviewInput{
		Revision:   rev,
		Operation:  op,
		Selections: selections,
	})
	return rev, plan, selections, err
}

func executeLifecyclePlan(cmd *cobra.Command, rev app.ConfigRevision, plan app.LifecyclePlanView, op lifecycle.Operation, selections []lifecycle.Selection) (app.LifecycleExecuteResult, error) {
	return appService().ExecuteLifecycle(cmd.Context(), app.LifecycleExecuteInput{
		Revision:   rev,
		PlanToken:  plan.PlanToken,
		Operation:  op,
		Selections: selections,
	})
}

func confirmExecute(cmd *cobra.Command, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !isInteractiveTerminal() {
		return false, &app.OutcomeError{
			Code:   "invalid_request",
			Params: map[string]any{"reason": "confirmation_required", "hint": "pass --yes"},
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), "Execute plan? [y/N]: ")
	var line string
	_, _ = fmt.Fscanln(cmd.InOrStdin(), &line)
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// ExitCode returns the process exit code for a CLI error (0 if nil).
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	return exitCodeOf(err)
}
