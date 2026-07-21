package lifecycle

import (
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// PlanProviderRemove builds a pure preview/execute plan for deleting one provider.
// Automatic L0 actions never expand Rewrite scope and never delete protected targets.
func PlanProviderRemove(base *config.Config, baseRevision, providerID string, selections []Selection) (Result, error) {
	if base == nil {
		return Result{}, fmt.Errorf("lifecycle: config is required")
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return Result{}, fmt.Errorf("lifecycle: provider id is required")
	}

	plan := emptyPlan(OpProviderRemove, baseRevision)
	selected := selectionMap(selections)

	found := false
	providerIndex := -1
	for i, p := range base.Providers {
		if p.ID == providerID {
			found = true
			providerIndex = i
			break
		}
	}
	if !found {
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:provider_missing",
			Code:        ReasonProviderMissing,
			Disposition: DispositionBlocker,
			Path:        "/config/providers",
			Params:      params("providerId", providerID),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}

	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:remove_provider:" + providerID,
		Kind:       ChangeRemove,
		Source:     SourceRequested,
		Entity:     EntityProvider,
		ReasonCode: ReasonProviderRemove,
		Path:       fmt.Sprintf("/config/providers/%d", providerIndex),
		Params:     params("providerId", providerID),
	})
	plan.RuntimeImpact.ProviderRemoved = true
	plan.RuntimeImpact.RoutingChanged = true

	// Alias targets.
	for ai, alias := range base.Aliases {
		aliasPath := fmt.Sprintf("/config/aliases/%d", ai)
		for ti, target := range alias.Targets {
			if target.Provider != providerID {
				continue
			}
			targetPath := fmt.Sprintf("%s/targets/%d", aliasPath, ti)
			if TargetSystemOwned(alias, target) {
				plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
					ID:         fmt.Sprintf("auto:remove_target:%s:%s:%s", alias.Alias, target.Provider, target.Model),
					Kind:       ChangeRemove,
					Source:     SourceAutomatic,
					Entity:     EntityAliasTarget,
					ReasonCode: ReasonSystemTargetCleanup,
					Path:       targetPath,
					Params: params(
						"alias", alias.Alias,
						"providerId", target.Provider,
						"model", target.Model,
						"aliasIndex", ai,
						"targetIndex", ti,
					),
				})
				continue
			}
			// Protected target: required choice.
			choiceID := fmt.Sprintf("protected_target:%d:%d", ai, ti)
			choice := Choice{
				ID:   choiceID,
				Code: ReasonProtectedTarget,
				Path: targetPath,
				Params: params(
					"alias", alias.Alias,
					"providerId", target.Provider,
					"model", target.Model,
					"ownership", string(ClassifyAlias(alias)),
				),
				Options: []ChoiceOption{
					{ID: OptionRebindTarget},
					{ID: OptionRemoveTarget},
					{ID: OptionDeleteAlias},
				},
			}
			plan.Choices = append(plan.Choices, choice)
			if sel, ok := selected[choiceID]; ok {
				change, err := selectedProtectedTargetChange(sel, alias, target, ai, ti, targetPath)
				if err != nil {
					plan.Blockers = append(plan.Blockers, Issue{
						ID:          "blocker:invalid:" + choiceID,
						Code:        ReasonInvalidSelection,
						Disposition: DispositionBlocker,
						Path:        targetPath,
						Params:      params("choiceId", choiceID, "error", err.Error()),
					})
					continue
				}
				plan.SelectedChanges = append(plan.SelectedChanges, change)
			}
		}
	}

	// Empty pure auto alias cleanup is computed after automatic+selected target removals.
	// We encode the intent as conditional automatic changes evaluated during apply.
	// For planning visibility, mark aliases that would become empty if only system targets remain for this provider.
	for ai, alias := range base.Aliases {
		if !PureAutoAlias(alias) {
			continue
		}
		remaining := 0
		for _, t := range alias.Targets {
			if t.Provider == providerID && TargetSystemOwned(alias, t) {
				continue
			}
			// protected targets on pure auto shouldn't exist; if they do, PureAutoAlias is false
			if t.Provider == providerID {
				continue
			}
			remaining++
		}
		if remaining == 0 && len(alias.Targets) > 0 {
			// Only if all targets were for this provider and system-owned.
			allSystemForProvider := true
			for _, t := range alias.Targets {
				if t.Provider != providerID || !TargetSystemOwned(alias, t) {
					allSystemForProvider = false
					break
				}
			}
			if allSystemForProvider {
				plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
					ID:         "auto:remove_empty_alias:" + alias.Alias,
					Kind:       ChangeRemove,
					Source:     SourceAutomatic,
					Entity:     EntityAlias,
					ReasonCode: ReasonEmptyAutoAliasCleanup,
					Path:       fmt.Sprintf("/config/aliases/%d", ai),
					Params:     params("alias", alias.Alias, "aliasIndex", ai, "providerId", providerID),
				})
			}
		}
	}

	// Priority cleanup.
	for pi, id := range base.ProviderPriority {
		if id == providerID {
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:remove_priority:%d", pi),
				Kind:       ChangeRemove,
				Source:     SourceAutomatic,
				Entity:     EntityPriority,
				ReasonCode: ReasonPriorityCleanup,
				Path:       fmt.Sprintf("/config/provider_priority/%d", pi),
				Params:     params("providerId", providerID, "priorityIndex", pi),
			})
		}
	}

	// Rewrite provider selectors.
	for ri, rule := range base.RequestRewriteRules {
		rulePath := fmt.Sprintf("/config/request_rewrite_rules/%d", ri)
		if len(rule.Providers) == 0 {
			// Wildcard preserved; deleting a provider does not change wildcard semantics.
			continue
		}
		matches := 0
		for _, id := range rule.Providers {
			if id == providerID {
				matches++
			}
		}
		if matches == 0 {
			continue
		}
		remaining := len(rule.Providers) - matches
		if remaining > 0 {
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:narrow_rewrite:%s", rule.Name),
				Kind:       ChangeUpdate,
				Source:     SourceAutomatic,
				Entity:     EntityRewriteRule,
				ReasonCode: ReasonRewriteSelectorNarrow,
				Path:       rulePath + "/providers",
				Params: params(
					"ruleName", rule.Name,
					"ruleIndex", ri,
					"providerId", providerID,
					"remainingCount", remaining,
				),
			})
			continue
		}
		// Singleton (or multi fully matching) would become empty=all — forbidden as automatic.
		choiceID := fmt.Sprintf("singleton_rewrite:%d", ri)
		choice := Choice{
			ID:   choiceID,
			Code: ReasonSingletonRewrite,
			Path: rulePath + "/providers",
			Params: params(
				"ruleName", rule.Name,
				"ruleIndex", ri,
				"providerId", providerID,
				"selectorCount", len(rule.Providers),
			),
			Options: []ChoiceOption{
				{ID: OptionKeepDormant},
				{ID: OptionDisableRule},
				{ID: OptionDeleteRule},
				{ID: OptionReplaceProviders},
			},
		}
		plan.Choices = append(plan.Choices, choice)
		plan.PreservedIssues = append(plan.PreservedIssues, Issue{
			ID:          "preserved:singleton_rewrite:" + rule.Name,
			Code:        ReasonSingletonRewrite,
			Disposition: DispositionPreserved,
			Path:        rulePath + "/providers",
			Params:      params("ruleName", rule.Name, "providerId", providerID),
		})
		if sel, ok := selected[choiceID]; ok {
			change, err := selectedSingletonRewriteChange(sel, rule, ri, rulePath, providerID)
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

	finalizePlan(&plan)
	if !plan.Executable {
		return Result{Plan: plan}, nil
	}

	candidate, err := applyProviderRemove(base, providerID, plan)
	if err != nil {
		return Result{}, err
	}
	// Second pass: remove pure auto aliases emptied only by this cleanup.
	candidate = cleanupEmptyPureAutoAliases(candidate, providerID, &plan)
	return Result{Plan: plan, Candidate: candidate}, nil
}

func selectedProtectedTargetChange(sel Selection, alias config.Alias, target config.Target, ai, ti int, path string) (Change, error) {
	base := Change{
		ID:     fmt.Sprintf("selected:protected_target:%d:%d", ai, ti),
		Source: SourceSelection,
		Path:   path,
		Params: params(
			"choiceId", sel.ChoiceID,
			"optionId", sel.OptionID,
			"alias", alias.Alias,
			"providerId", target.Provider,
			"model", target.Model,
			"aliasIndex", ai,
			"targetIndex", ti,
		),
	}
	switch sel.OptionID {
	case OptionRemoveTarget:
		base.Kind = ChangeRemove
		base.Entity = EntityAliasTarget
		base.ReasonCode = ReasonProtectedTarget
		return base, nil
	case OptionDeleteAlias:
		base.Kind = ChangeRemove
		base.Entity = EntityAlias
		base.ReasonCode = ReasonProtectedTarget
		return base, nil
	case OptionRebindTarget:
		newProvider, _ := sel.Params["providerId"].(string)
		newModel, _ := sel.Params["model"].(string)
		newProvider = strings.TrimSpace(newProvider)
		newModel = strings.TrimSpace(newModel)
		if newProvider == "" || newModel == "" {
			return Change{}, fmt.Errorf("rebind_target requires params.providerId and params.model")
		}
		base.Kind = ChangeUpdate
		base.Entity = EntityAliasTarget
		base.ReasonCode = ReasonProtectedTarget
		base.Params["newProviderId"] = newProvider
		base.Params["newModel"] = newModel
		return base, nil
	default:
		return Change{}, fmt.Errorf("unsupported option %q", sel.OptionID)
	}
}

func selectedSingletonRewriteChange(sel Selection, rule config.RequestRewriteRule, ri int, path, providerID string) (Change, error) {
	base := Change{
		ID:     fmt.Sprintf("selected:singleton_rewrite:%d", ri),
		Source: SourceSelection,
		Entity: EntityRewriteRule,
		Path:   path,
		Params: params(
			"choiceId", sel.ChoiceID,
			"optionId", sel.OptionID,
			"ruleName", rule.Name,
			"ruleIndex", ri,
			"providerId", providerID,
		),
	}
	switch sel.OptionID {
	case OptionKeepDormant:
		base.Kind = ChangeUpdate
		base.ReasonCode = ReasonSingletonRewrite
		base.Params["action"] = OptionKeepDormant
		return base, nil
	case OptionDisableRule:
		base.Kind = ChangeUpdate
		base.ReasonCode = ReasonSingletonRewrite
		base.Params["action"] = OptionDisableRule
		return base, nil
	case OptionDeleteRule:
		base.Kind = ChangeRemove
		base.ReasonCode = ReasonSingletonRewrite
		base.Params["action"] = OptionDeleteRule
		return base, nil
	case OptionReplaceProviders:
		raw, ok := sel.Params["providers"]
		if !ok {
			return Change{}, fmt.Errorf("replace_providers requires params.providers")
		}
		providers, err := stringSliceParam(raw)
		if err != nil {
			return Change{}, err
		}
		if len(providers) == 0 {
			return Change{}, fmt.Errorf("replace_providers must be non-empty (empty means wildcard)")
		}
		base.Kind = ChangeUpdate
		base.ReasonCode = ReasonSingletonRewrite
		base.Params["action"] = OptionReplaceProviders
		base.Params["providers"] = providers
		return base, nil
	default:
		return Change{}, fmt.Errorf("unsupported option %q", sel.OptionID)
	}
}

func stringSliceParam(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("providers must be string array")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("providers must be string array")
	}
}

func applyProviderRemove(base *config.Config, providerID string, plan Plan) (*config.Config, error) {
	cfg := CloneConfig(base)

	// Index selected actions by alias/target and rewrite.
	deleteAliases := map[string]bool{}
	removeTargets := map[string]bool{} // alias\x00provider\x00model
	rebindTargets := map[string]config.Target{}
	rewriteActions := map[int]Change{}

	for _, ch := range plan.SelectedChanges {
		switch ch.Entity {
		case EntityAlias:
			if alias, ok := ch.Params["alias"].(string); ok {
				deleteAliases[alias] = true
			}
		case EntityAliasTarget:
			alias, _ := ch.Params["alias"].(string)
			provider, _ := ch.Params["providerId"].(string)
			model, _ := ch.Params["model"].(string)
			key := alias + "\x00" + provider + "\x00" + model
			if ch.Kind == ChangeUpdate {
				newP, _ := ch.Params["newProviderId"].(string)
				newM, _ := ch.Params["newModel"].(string)
				rebindTargets[key] = config.Target{Provider: newP, Model: newM, Enabled: true}
			} else {
				removeTargets[key] = true
			}
		case EntityRewriteRule:
			if idx, ok := asInt(ch.Params["ruleIndex"]); ok {
				rewriteActions[idx] = ch
			}
		}
	}

	// Apply alias mutations.
	nextAliases := make([]config.Alias, 0, len(cfg.Aliases))
	for _, alias := range cfg.Aliases {
		if deleteAliases[alias.Alias] {
			continue
		}
		// automatic empty pure auto removal is handled later
		nextTargets := make([]config.Target, 0, len(alias.Targets))
		for _, t := range alias.Targets {
			key := alias.Alias + "\x00" + t.Provider + "\x00" + t.Model
			if t.Provider == providerID && TargetSystemOwned(alias, t) {
				continue // automatic cleanup
			}
			if removeTargets[key] {
				continue
			}
			if rebind, ok := rebindTargets[key]; ok {
				t.Provider = rebind.Provider
				t.Model = rebind.Model
				// rebind clears auto flag: becomes user-owned target identity
				t.AutoGenerated = false
			}
			nextTargets = append(nextTargets, t)
		}
		alias.Targets = nextTargets
		nextAliases = append(nextAliases, alias)
	}
	cfg.Aliases = nextAliases

	// Remove provider.
	nextProviders := make([]config.Provider, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			continue
		}
		nextProviders = append(nextProviders, p)
	}
	cfg.Providers = nextProviders

	// Priority.
	nextPriority := make([]string, 0, len(cfg.ProviderPriority))
	for _, id := range cfg.ProviderPriority {
		if id == providerID {
			continue
		}
		nextPriority = append(nextPriority, id)
	}
	cfg.ProviderPriority = nextPriority

	// Rewrite rules.
	nextRules := make([]config.RequestRewriteRule, 0, len(cfg.RequestRewriteRules))
	for ri, rule := range cfg.RequestRewriteRules {
		if ch, ok := rewriteActions[ri]; ok {
			action, _ := ch.Params["action"].(string)
			switch action {
			case OptionKeepDormant:
				// leave providers as-is (dormant singleton)
				nextRules = append(nextRules, rule)
			case OptionDisableRule:
				rule.Enabled = false
				nextRules = append(nextRules, rule)
			case OptionDeleteRule:
				// drop
			case OptionReplaceProviders:
				if providers, err := stringSliceParam(ch.Params["providers"]); err == nil {
					rule.Providers = providers
				}
				nextRules = append(nextRules, rule)
			default:
				nextRules = append(nextRules, rule)
			}
			continue
		}
		if len(rule.Providers) == 0 {
			nextRules = append(nextRules, rule)
			continue
		}
		// Automatic narrow.
		filtered := make([]string, 0, len(rule.Providers))
		for _, id := range rule.Providers {
			if id == providerID {
				continue
			}
			filtered = append(filtered, id)
		}
		if len(filtered) == 0 && len(rule.Providers) > 0 {
			// Unresolved singleton should not reach apply; keep as safety.
			nextRules = append(nextRules, rule)
			continue
		}
		rule.Providers = filtered
		nextRules = append(nextRules, rule)
	}
	cfg.RequestRewriteRules = nextRules
	return cfg, nil
}

func cleanupEmptyPureAutoAliases(cfg *config.Config, providerID string, plan *Plan) *config.Config {
	next := make([]config.Alias, 0, len(cfg.Aliases))
	for _, alias := range cfg.Aliases {
		if PureAutoAlias(alias) && len(alias.Targets) == 0 {
			// Only remove if we planned empty cleanup for this provider impact.
			planned := false
			for _, ch := range plan.AutomaticChanges {
				if ch.ReasonCode == ReasonEmptyAutoAliasCleanup {
					if a, ok := ch.Params["alias"].(string); ok && a == alias.Alias {
						planned = true
						break
					}
				}
			}
			if planned {
				continue
			}
		}
		next = append(next, alias)
	}
	cfg.Aliases = next
	return cfg
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
