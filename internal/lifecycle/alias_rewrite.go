package lifecycle

import (
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// PlanAliasRemove plans deletion of one alias without cascading Rewrite deletion.
// Rewrite selector impacts become preserved issues / required choices.
func PlanAliasRemove(base *config.Config, baseRevision, aliasName string, selections []Selection, external ExternalRefs) (Result, error) {
	if base == nil {
		return Result{}, fmt.Errorf("lifecycle: config is required")
	}
	aliasName = strings.TrimSpace(aliasName)
	if aliasName == "" {
		return Result{}, fmt.Errorf("lifecycle: alias is required")
	}

	plan := emptyPlan(OpAliasRemove, baseRevision)
	selected := selectionMap(selections)

	idx, err := RequireUniqueAlias(base, aliasName)
	if err != nil {
		code := ReasonAliasMissing
		if strings.Contains(err.Error(), ReasonAliasAmbiguous) {
			code = ReasonAliasAmbiguous
		}
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:alias",
			Code:        code,
			Disposition: DispositionBlocker,
			Path:        "/config/aliases",
			Params:      params("alias", aliasName),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}

	alias := base.Aliases[idx]
	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:remove_alias:" + aliasName,
		Kind:       ChangeRemove,
		Source:     SourceRequested,
		Entity:     EntityAlias,
		ReasonCode: ReasonAliasRemove,
		Path:       fmt.Sprintf("/config/aliases/%d", idx),
		Params:     params("alias", aliasName, "ownership", string(ClassifyAlias(alias))),
	})
	plan.RuntimeImpact.AliasRemoved = true
	plan.RuntimeImpact.RoutingChanged = true

	// Rewrite selector impact: never auto-delete; report direct fallback possibility.
	for ri, rule := range base.RequestRewriteRules {
		if rule.Alias != aliasName {
			continue
		}
		rulePath := fmt.Sprintf("/config/request_rewrite_rules/%d", ri)
		choiceID := fmt.Sprintf("rewrite_on_alias_remove:%d", ri)
		plan.PreservedIssues = append(plan.PreservedIssues, Issue{
			ID:          "preserved:rewrite_selector:" + rule.Name,
			Code:        ReasonDirectFallbackPossible,
			Disposition: DispositionPreserved,
			Path:        rulePath + "/alias",
			Params: params(
				"ruleName", rule.Name,
				"alias", aliasName,
				"directFallbackPossible", true,
			),
		})
		choice := Choice{
			ID:   choiceID,
			Code: ReasonRewriteSelectorImpact,
			Path: rulePath,
			Params: params(
				"ruleName", rule.Name,
				"ruleIndex", ri,
				"alias", aliasName,
			),
			Options: []ChoiceOption{
				{ID: OptionKeepRule},
				{ID: OptionDisableRule},
				{ID: OptionDeleteRule},
			},
		}
		plan.Choices = append(plan.Choices, choice)
		if sel, ok := selected[choiceID]; ok {
			change, err := selectedRewriteOnAliasRemove(sel, rule, ri, rulePath)
			if err != nil {
				plan.Blockers = append(plan.Blockers, Issue{
					ID:          "blocker:invalid:" + choiceID,
					Code:        ReasonInvalidSelection,
					Disposition: DispositionBlocker,
					Path:        rulePath,
					Params:      params("choiceId", choiceID, "error", err.Error()),
				})
				continue
			}
			plan.SelectedChanges = append(plan.SelectedChanges, change)
		}
	}

	// OpenCode weak refs — report only, never hard-fail as FK.
	for _, ref := range []struct {
		field string
		value string
	}{
		{"model", external.OpenCodeModel},
		{"small_model", external.OpenCodeSmallModel},
	} {
		if strings.TrimSpace(ref.value) == "" {
			continue
		}
		if ref.value != aliasName && !strings.HasSuffix(ref.value, "/"+aliasName) {
			// Accept bare alias or provider/alias style.
			continue
		}
		plan.PreservedIssues = append(plan.PreservedIssues, Issue{
			ID:          "preserved:opencode:" + ref.field,
			Code:        ReasonOpenCodeWeakRef,
			Disposition: DispositionPreserved,
			Path:        "/external/opencode/" + ref.field,
			Params:      params("field", ref.field, "value", ref.value, "alias", aliasName),
		})
	}

	finalizePlan(&plan)
	if !plan.Executable {
		return Result{Plan: plan}, nil
	}
	candidate := applyAliasRemove(base, aliasName, plan)
	return Result{Plan: plan, Candidate: candidate}, nil
}

func selectedRewriteOnAliasRemove(sel Selection, rule config.RequestRewriteRule, ri int, path string) (Change, error) {
	base := Change{
		ID:     fmt.Sprintf("selected:rewrite_on_alias_remove:%d", ri),
		Source: SourceSelection,
		Entity: EntityRewriteRule,
		Path:   path,
		Params: params(
			"choiceId", sel.ChoiceID,
			"optionId", sel.OptionID,
			"ruleName", rule.Name,
			"ruleIndex", ri,
		),
	}
	switch sel.OptionID {
	case OptionKeepRule:
		base.Kind = ChangeUpdate
		base.ReasonCode = ReasonRewriteSelectorImpact
		base.Params["action"] = OptionKeepRule
		return base, nil
	case OptionDisableRule:
		base.Kind = ChangeUpdate
		base.ReasonCode = ReasonRewriteSelectorImpact
		base.Params["action"] = OptionDisableRule
		return base, nil
	case OptionDeleteRule:
		base.Kind = ChangeRemove
		base.ReasonCode = ReasonRewriteSelectorImpact
		base.Params["action"] = OptionDeleteRule
		return base, nil
	default:
		return Change{}, fmt.Errorf("unsupported option %q", sel.OptionID)
	}
}

func applyAliasRemove(base *config.Config, aliasName string, plan Plan) *config.Config {
	cfg := CloneConfig(base)
	nextAliases := make([]config.Alias, 0, len(cfg.Aliases))
	for _, a := range cfg.Aliases {
		if a.Alias == aliasName {
			continue
		}
		nextAliases = append(nextAliases, a)
	}
	cfg.Aliases = nextAliases

	rewriteAction := map[int]string{}
	for _, ch := range plan.SelectedChanges {
		if ch.Entity != EntityRewriteRule {
			continue
		}
		idx, ok := asInt(ch.Params["ruleIndex"])
		if !ok {
			continue
		}
		action, _ := ch.Params["action"].(string)
		rewriteAction[idx] = action
	}

	nextRules := make([]config.RequestRewriteRule, 0, len(cfg.RequestRewriteRules))
	for ri, rule := range cfg.RequestRewriteRules {
		if rule.Alias != aliasName {
			nextRules = append(nextRules, rule)
			continue
		}
		action := rewriteAction[ri]
		if action == "" {
			action = OptionKeepRule
		}
		switch action {
		case OptionKeepRule:
			nextRules = append(nextRules, rule)
		case OptionDisableRule:
			rule.Enabled = false
			nextRules = append(nextRules, rule)
		case OptionDeleteRule:
			// drop
		default:
			nextRules = append(nextRules, rule)
		}
	}
	cfg.RequestRewriteRules = nextRules
	return cfg
}

// PlanAliasUpgrade is retained for API compatibility. Automatic model aliases no
// longer need to be upgraded before user overrides; the plan is an executable no-op.
func PlanAliasUpgrade(base *config.Config, baseRevision, aliasName string) (Result, error) {
	if base == nil {
		return Result{}, fmt.Errorf("lifecycle: config is required")
	}
	aliasName = strings.TrimSpace(aliasName)
	plan := emptyPlan(OpAliasUpgrade, baseRevision)

	idx, err := RequireUniqueAlias(base, aliasName)
	if err != nil {
		code := ReasonAliasMissing
		if strings.Contains(err.Error(), ReasonAliasAmbiguous) {
			code = ReasonAliasAmbiguous
		}
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:alias",
			Code:        code,
			Disposition: DispositionBlocker,
			Path:        "/config/aliases",
			Params:      params("alias", aliasName),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}
	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:edit_auto_alias:" + aliasName,
		Kind:       ChangeUpdate,
		Source:     SourceRequested,
		Entity:     EntityAlias,
		ReasonCode: ReasonUpgradeOwnership,
		Path:       fmt.Sprintf("/config/aliases/%d", idx),
		Params:     params("alias", aliasName, "deprecated", true),
	})
	finalizePlan(&plan)
	plan.Executable = true
	plan.NoOp = true
	return Result{Plan: plan, Candidate: CloneConfig(base)}, nil
}

// PlanAliasMutateGate validates that the alias exists before a caller-owned
// metadata/target override. Automatic model aliases are editable overlays and do
// not require an ownership upgrade.
func PlanAliasMutateGate(base *config.Config, baseRevision, aliasName, intent string) (Result, error) {
	plan := emptyPlan(OpAliasMutate, baseRevision)
	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:mutate_alias:" + aliasName,
		Kind:       ChangeUpdate,
		Source:     SourceRequested,
		Entity:     EntityAlias,
		ReasonCode: intent,
		Params:     params("alias", aliasName, "intent", intent),
	})
	if _, err := RequireUniqueAlias(base, aliasName); err != nil {
		code := ReasonAliasMissing
		if strings.Contains(err.Error(), ReasonAliasMissing) {
			code = ReasonAliasMissing
		} else if strings.Contains(err.Error(), ReasonAliasAmbiguous) {
			code = ReasonAliasAmbiguous
		}
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:ownership",
			Code:        code,
			Disposition: DispositionBlocker,
			Path:        "/config/aliases",
			Params:      params("alias", aliasName, "intent", intent),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}
	finalizePlan(&plan)
	// Gate-only plan: executable with no structural changes (caller applies own mutation after gate).
	plan.NoOp = true
	plan.Executable = true
	return Result{Plan: plan, Candidate: CloneConfig(base)}, nil
}
