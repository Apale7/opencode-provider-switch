package app

import (
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/diagnostics"
)

func doctorIssueFromDiagnostic(issue diagnostics.Issue) DoctorIssue {
	out := DoctorIssue{
		SchemaVersion:  issue.SchemaVersion,
		Code:           string(issue.Code),
		Severity:       string(issue.Severity),
		Path:           issue.Path,
		Reason:         string(issue.Reason),
		AllowedActions: actionsToStrings(issue.AllowedActions),
		Params:         cloneParams(issue.Params),
		Source: &DiagnosticEntityRef{
			Kind: string(issue.Source.Kind),
			Key:  issue.Source.Key,
			Path: issue.Source.Path,
		},
	}
	if issue.Target != nil {
		out.Target = &DiagnosticTargetRef{
			Kind: string(issue.Target.Kind),
			Key:  issue.Target.Key,
			Path: issue.Target.Path,
		}
	}
	if alias, _ := issue.Params["alias"].(string); alias != "" {
		out.Alias = alias
	}
	if providerID, _ := issue.Params["providerId"].(string); providerID != "" {
		out.ProviderKey = providerID
	}
	if protocol, _ := issue.Params["aliasProtocol"].(string); protocol != "" {
		out.Protocol = protocol
	}
	if protocol, _ := issue.Params["providerProtocol"].(string); out.Protocol == "" && protocol != "" {
		out.Protocol = protocol
	}
	out.ActionHint = actionHintForCode(out.Code)
	out.Message = legacyMessageForDiagnostic(issue)
	return out
}

func doctorIssuesFromDiagnostics(issues []diagnostics.Issue) []DoctorIssue {
	out := make([]DoctorIssue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, doctorIssueFromDiagnostic(issue))
	}
	return out
}

func actionsToStrings(actions []diagnostics.Action) []string {
	if len(actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, string(action))
	}
	return out
}

func cloneParams(params diagnostics.Params) map[string]any {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = value
	}
	return out
}

func actionHintForCode(code string) string {
	switch code {
	case string(diagnostics.CodeAliasTargetProviderMissing), string(diagnostics.CodeAliasTargetProtocolMismatch):
		return "rebind or remove the unavailable alias target"
	case string(diagnostics.CodeAliasDisabled):
		return "enable the alias or keep it disabled intentionally"
	case string(diagnostics.CodeAliasTargetDisabled):
		return "enable the target or remove it from the alias"
	case string(diagnostics.CodeAliasTargetProviderDisabled):
		return "enable the provider or rebind the alias target"
	case string(diagnostics.CodeProviderCatalogStale), string(diagnostics.CodeAliasTargetModelUnconfirmed):
		return "refresh the provider model catalog"
	case string(diagnostics.CodeAliasNoAvailableTarget), string(diagnostics.CodeNoAvailableTarget):
		return "add an available target or fix disabled/missing providers"
	case string(diagnostics.CodeRewriteAliasUnresolved), string(diagnostics.CodeRewriteProviderMissing):
		return "replace the selector, disable, or delete the rewrite rule"
	case string(diagnostics.CodePriorityProviderMissing):
		return "remove the stale provider priority entry"
	case string(diagnostics.CodeRewriteRuleLegacy):
		return "edit the rewrite rule and replace set/delete with ops"
	case string(diagnostics.CodeOpenCodeDefaultUnroutable), string(diagnostics.CodeOpenCodeSmallUnroutable):
		return "choose one of available routable aliases"
	case string(diagnostics.CodeOpenCodeContractMissing), string(diagnostics.CodeOpenCodeContractInvalid), string(diagnostics.CodeOpenCodeContractDrift), string(diagnostics.CodeOpenCodeCatalogDrift):
		return "run ocswitch opencode sync"
	case string(diagnostics.CodeRuntimeUnreachable), string(diagnostics.CodeRuntimeAuthFailed), string(diagnostics.CodeRuntimeBadStatus):
		return "ensure OpenCode runtime is reachable and authenticated"
	case string(diagnostics.CodeRuntimeProviderMissing), string(diagnostics.CodeRuntimeProviderProtocolMismatch):
		return "restart or reload OpenCode after sync"
	case string(diagnostics.CodeFileParseError):
		return "fix OpenCode config JSON/JSONC syntax"
	case string(diagnostics.CodeConfigInvalid):
		return "fix local ocswitch config and rerun doctor"
	default:
		return ""
	}
}

func legacyMessageForDiagnostic(issue diagnostics.Issue) string {
	params := issue.Params
	switch issue.Code {
	case diagnostics.CodeAliasTargetProviderMissing:
		return fmt.Sprintf("alias %q target references missing provider %q", paramString(params, "alias"), paramString(params, "providerId"))
	case diagnostics.CodeAliasDisabled:
		return fmt.Sprintf("alias %q is disabled", paramString(params, "alias"))
	case diagnostics.CodeAliasTargetDisabled:
		return fmt.Sprintf("alias %q target %s/%s is disabled", paramString(params, "alias"), paramString(params, "providerId"), paramString(params, "model"))
	case diagnostics.CodeAliasTargetProviderDisabled:
		return fmt.Sprintf("alias %q target provider %q is disabled", paramString(params, "alias"), paramString(params, "providerId"))
	case diagnostics.CodeAliasTargetProtocolMismatch:
		return fmt.Sprintf("alias %q target provider %q protocol mismatch", paramString(params, "alias"), paramString(params, "providerId"))
	case diagnostics.CodeProviderCatalogStale:
		return fmt.Sprintf("provider %q model catalog is stale (%s)", paramString(params, "providerId"), paramString(params, "catalogState"))
	case diagnostics.CodeAliasTargetModelUnconfirmed:
		return fmt.Sprintf("alias %q target model %q is unconfirmed", paramString(params, "alias"), paramString(params, "model"))
	case diagnostics.CodeAliasNoAvailableTarget:
		return fmt.Sprintf("alias %q has no available targets", paramString(params, "alias"))
	case diagnostics.CodeRewriteAliasUnresolved:
		return fmt.Sprintf("rewrite rule %q alias selector %q is unresolved", paramString(params, "ruleName"), paramString(params, "alias"))
	case diagnostics.CodeRewriteProviderMissing:
		return fmt.Sprintf("rewrite rule %q provider selector %q is missing", paramString(params, "ruleName"), paramString(params, "providerId"))
	case diagnostics.CodePriorityProviderMissing:
		return fmt.Sprintf("provider priority entry references missing provider %q", paramString(params, "providerId"))
	case diagnostics.CodeProviderIdentityAmbiguous, diagnostics.CodeAliasIdentityAmbiguous, diagnostics.CodeAliasTargetIdentityAmbiguous, diagnostics.CodeRewriteIdentityAmbiguous:
		return fmt.Sprintf("%s has ambiguous identity", issue.Code)
	case diagnostics.CodeRewriteRuleLegacy:
		return "rewrite rule uses legacy set/delete syntax"
	default:
		if issue.Path != "" {
			return fmt.Sprintf("%s at %s", issue.Code, issue.Path)
		}
		return string(issue.Code)
	}
}

func paramString(params diagnostics.Params, key string) string {
	if params == nil {
		return ""
	}
	value, _ := params[key].(string)
	return value
}

func aliasTargetView(cfg *config.Config, alias config.Alias, target config.Target) AliasTargetView {
	view := AliasTargetView{
		Provider:      target.Provider,
		Model:         target.Model,
		Enabled:       target.Enabled,
		AutoGenerated: target.AutoGenerated,
	}
	if !target.Enabled {
		view.Reason = string(diagnostics.ReasonDisabled)
		view.Code = string(diagnostics.CodeAliasTargetDisabled)
		view.AllowedActions = targetActionsForAlias(alias)
		if !alias.AutoGenerated && !alias.Locked {
			view.AllowedActions = appendUniqueAction(view.AllowedActions, string(diagnostics.ActionEnableTarget))
		}
		return view
	}
	provider := cfg.FindProvider(target.Provider)
	if provider == nil {
		view.Reason = string(diagnostics.ReasonMissing)
		view.Code = string(diagnostics.CodeAliasTargetProviderMissing)
		view.AllowedActions = targetActionsForAlias(alias)
		return view
	}
	if !config.ProtocolsMatch(alias.Protocol, provider.Protocol) {
		view.Reason = string(diagnostics.ReasonProtocolMismatch)
		view.Code = string(diagnostics.CodeAliasTargetProtocolMismatch)
		view.AllowedActions = targetActionsForAlias(alias)
		return view
	}
	if provider.Disabled {
		view.Reason = string(diagnostics.ReasonDisabled)
		view.Code = string(diagnostics.CodeAliasTargetProviderDisabled)
		view.AllowedActions = appendUniqueAction(targetActionsForAlias(alias), string(diagnostics.ActionEnableProvider), string(diagnostics.ActionKeep))
		return view
	}
	view.Available = true
	return view
}

func targetActionsForAlias(alias config.Alias) []string {
	if alias.AutoGenerated || alias.Locked {
		return []string{string(diagnostics.ActionUpgradeAlias), string(diagnostics.ActionDeleteAlias)}
	}
	return []string{string(diagnostics.ActionRebindTarget), string(diagnostics.ActionRemoveTarget), string(diagnostics.ActionDeleteAlias)}
}

func appendUniqueAction(actions []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(actions)+len(extra))
	for _, action := range actions {
		if action == "" || seen[action] {
			continue
		}
		seen[action] = true
		out = append(out, action)
	}
	for _, action := range extra {
		if action == "" || seen[action] {
			continue
		}
		seen[action] = true
		out = append(out, action)
	}
	return out
}

func structuralDoctorIssues(errs []error) []DoctorIssue {
	issues := make([]DoctorIssue, 0, len(errs))
	for _, err := range errs {
		msg := err.Error()
		// Reference/availability problems are covered by diagnostics.ScanConfig.
		if isReferenceAvailabilityValidateError(msg) {
			continue
		}
		issues = append(issues, DoctorIssue{
			SchemaVersion: diagnostics.SchemaVersion,
			Code:          string(diagnostics.CodeConfigInvalid),
			Severity:      string(diagnostics.SeverityError),
			Reason:        string(diagnostics.ReasonInvalid),
			Message:       msg,
			ActionHint:    actionHintForCode(string(diagnostics.CodeConfigInvalid)),
			Params:        map[string]any{},
		})
	}
	return issues
}

func isReferenceAvailabilityValidateError(msg string) bool {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "references unknown provider"):
		return true
	case strings.Contains(lower, "has no available targets"):
		return true
	case strings.Contains(lower, "protocol") && strings.Contains(lower, "does not match provider protocol"):
		return true
	default:
		return false
	}
}
