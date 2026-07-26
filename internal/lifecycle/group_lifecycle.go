package lifecycle

import (
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// PlanGroupRemove builds a pure preview/execute plan for deleting one provider group.
// System-owned alias targets for the precise (provider, group) are cleaned automatically.
// Protected targets and singleton provider_groups selectors require explicit choices.
// Never silently rebinds to default or a sibling group.
func PlanGroupRemove(base *config.Config, baseRevision, providerID, groupID string, selections []Selection) (Result, error) {
	if base == nil {
		return Result{}, fmt.Errorf("lifecycle: config is required")
	}
	providerID = strings.TrimSpace(providerID)
	groupID = normalizeGroupID(groupID)
	if providerID == "" {
		return Result{}, fmt.Errorf("lifecycle: provider id is required")
	}
	if groupID == "" {
		return Result{}, fmt.Errorf("lifecycle: group id is required")
	}

	plan := emptyPlan(OpGroupRemove, baseRevision)
	selected := selectionMap(selections)

	providerIndex, groupIndex, provider, ok := findProviderGroup(base, providerID, groupID)
	if !ok {
		if providerIndex < 0 {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:provider_missing",
				Code:        ReasonProviderMissing,
				Disposition: DispositionBlocker,
				Path:        "/config/providers",
				Params:      params("providerId", providerID, "groupId", groupID),
			})
		} else {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:group_missing",
				Code:        ReasonGroupMissing,
				Disposition: DispositionBlocker,
				Path:        fmt.Sprintf("/config/providers/%d/groups", providerIndex),
				Params:      params("providerId", providerID, "groupId", groupID),
			})
		}
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}
	if len(provider.Groups) <= 1 {
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:last_group",
			Code:        ReasonLastGroup,
			Disposition: DispositionBlocker,
			Path:        fmt.Sprintf("/config/providers/%d/groups/%d", providerIndex, groupIndex),
			Params:      params("providerId", providerID, "groupId", groupID, "groupCount", len(provider.Groups)),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}

	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:remove_group:" + providerID + ":" + groupID,
		Kind:       ChangeRemove,
		Source:     SourceRequested,
		Entity:     EntityProviderGroup,
		ReasonCode: ReasonGroupRemove,
		Path:       fmt.Sprintf("/config/providers/%d/groups/%d", providerIndex, groupIndex),
		Params:     params("providerId", providerID, "groupId", groupID, "providerIndex", providerIndex, "groupIndex", groupIndex),
	})
	plan.RuntimeImpact.GroupRemoved = true
	plan.RuntimeImpact.RoutingChanged = true

	// Alias targets for precise (provider, group).
	for ai, alias := range base.Aliases {
		aliasPath := fmt.Sprintf("/config/aliases/%d", ai)
		for ti, target := range alias.Targets {
			if target.Provider != providerID || targetGroupID(target) != groupID {
				continue
			}
			targetPath := fmt.Sprintf("%s/targets/%d", aliasPath, ti)
			if TargetSystemOwned(alias, target) {
				plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
					ID:         fmt.Sprintf("auto:remove_target:%s:%s:%s:%s", alias.Alias, target.Provider, groupID, target.Model),
					Kind:       ChangeRemove,
					Source:     SourceAutomatic,
					Entity:     EntityAliasTarget,
					ReasonCode: ReasonSystemTargetCleanup,
					Path:       targetPath,
					Params: params(
						"alias", alias.Alias,
						"providerId", target.Provider,
						"groupId", groupID,
						"model", target.Model,
						"aliasIndex", ai,
						"targetIndex", ti,
					),
				})
				continue
			}
			choiceID := fmt.Sprintf("protected_target:%d:%d", ai, ti)
			choice := Choice{
				ID:   choiceID,
				Code: ReasonProtectedTarget,
				Path: targetPath,
				Params: params(
					"alias", alias.Alias,
					"providerId", target.Provider,
					"groupId", groupID,
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
				change, err := selectedGroupProtectedTargetChange(sel, alias, target, ai, ti, targetPath, providerID, groupID, base)
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

	// Empty pure auto alias cleanup when all remaining targets are this group's system targets.
	for ai, alias := range base.Aliases {
		if !PureAutoAlias(alias) || len(alias.Targets) == 0 {
			continue
		}
		allForGroup := true
		for _, t := range alias.Targets {
			if t.Provider != providerID || targetGroupID(t) != groupID || !TargetSystemOwned(alias, t) {
				allForGroup = false
				break
			}
		}
		if allForGroup {
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         "auto:remove_empty_alias:" + alias.Alias,
				Kind:       ChangeRemove,
				Source:     SourceAutomatic,
				Entity:     EntityAlias,
				ReasonCode: ReasonEmptyAutoAliasCleanup,
				Path:       fmt.Sprintf("/config/aliases/%d", ai),
				Params:     params("alias", alias.Alias, "aliasIndex", ai, "providerId", providerID, "groupId", groupID),
			})
		}
	}

	// Rewrite provider_groups selectors.
	for ri, rule := range base.RequestRewriteRules {
		rulePath := fmt.Sprintf("/config/request_rewrite_rules/%d", ri)
		if rule.ProviderGroups == nil {
			// No precise group selectors; legacy Providers alone is out of group-remove scope.
			continue
		}
		if len(rule.ProviderGroups) == 0 {
			// Explicit wildcard — group delete must not shrink or expand it.
			continue
		}
		matches := 0
		for _, sel := range rule.ProviderGroups {
			if sel.Provider == providerID && normalizeGroupID(sel.Group) == groupID {
				matches++
			}
		}
		if matches == 0 {
			continue
		}
		remaining := len(rule.ProviderGroups) - matches
		if remaining > 0 {
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:narrow_rewrite_groups:%s", rule.Name),
				Kind:       ChangeUpdate,
				Source:     SourceAutomatic,
				Entity:     EntityRewriteRule,
				ReasonCode: ReasonRewriteSelectorNarrow,
				Path:       rulePath + "/provider_groups",
				Params: params(
					"ruleName", rule.Name,
					"ruleIndex", ri,
					"providerId", providerID,
					"groupId", groupID,
					"remainingCount", remaining,
				),
			})
			continue
		}
		// Would become empty=wildcard — forbidden as automatic.
		choiceID := fmt.Sprintf("singleton_provider_groups:%d", ri)
		choice := Choice{
			ID:   choiceID,
			Code: ReasonSingletonRewrite,
			Path: rulePath + "/provider_groups",
			Params: params(
				"ruleName", rule.Name,
				"ruleIndex", ri,
				"providerId", providerID,
				"groupId", groupID,
				"selectorCount", len(rule.ProviderGroups),
			),
			Options: []ChoiceOption{
				{ID: OptionKeepDormant},
				{ID: OptionDisableRule},
				{ID: OptionDeleteRule},
				{ID: OptionReplaceProviderGroups},
			},
		}
		plan.Choices = append(plan.Choices, choice)
		plan.PreservedIssues = append(plan.PreservedIssues, Issue{
			ID:          "preserved:singleton_provider_groups:" + rule.Name,
			Code:        ReasonSingletonRewrite,
			Disposition: DispositionPreserved,
			Path:        rulePath + "/provider_groups",
			Params:      params("ruleName", rule.Name, "providerId", providerID, "groupId", groupID),
		})
		if sel, ok := selected[choiceID]; ok {
			change, err := selectedSingletonProviderGroupsChange(sel, rule, ri, rulePath, providerID, groupID, base)
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
	candidate, err := applyGroupRemove(base, providerID, groupID, plan)
	if err != nil {
		return Result{}, err
	}
	candidate = cleanupEmptyPureAutoAliases(candidate, providerID, &plan)
	if aliasName, targetIndex, identity, ok := duplicateAliasTargetIdentity(candidate); ok {
		plan.Blockers = append(plan.Blockers, Issue{
			ID:          "blocker:duplicate_rebind_target:" + aliasName,
			Code:        ReasonInvalidSelection,
			Disposition: DispositionBlocker,
			Path:        fmt.Sprintf("/config/aliases/%s/targets/%d", aliasName, targetIndex),
			Params:      params("alias", aliasName, "identity", identity),
		})
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}
	return Result{Plan: plan, Candidate: candidate}, nil
}

func duplicateAliasTargetIdentity(cfg *config.Config) (aliasName string, targetIndex int, identity string, ok bool) {
	if cfg == nil {
		return "", 0, "", false
	}
	for _, alias := range cfg.Aliases {
		seen := map[string]bool{}
		for ti, target := range alias.Targets {
			key := strings.TrimSpace(target.Provider) + "\n" + targetGroupID(target) + "\n" + strings.TrimSpace(target.Model)
			if seen[key] {
				return alias.Alias, ti, key, true
			}
			seen[key] = true
		}
	}
	return "", 0, "", false
}

// PlanGroupIDChange atomically renames one group identity and every precise reference.
// Alias targets and rewrite provider_groups selectors matching (provider, oldID) become newID.
// Auto-generated flags are preserved. Other groups are never touched.
func PlanGroupIDChange(base *config.Config, baseRevision, providerID, oldGroupID, newGroupID string, selections []Selection) (Result, error) {
	if base == nil {
		return Result{}, fmt.Errorf("lifecycle: config is required")
	}
	_ = selections // ID change is deterministic; no required choices.
	providerID = strings.TrimSpace(providerID)
	oldGroupID = normalizeGroupID(oldGroupID)
	newGroupID = normalizeGroupID(newGroupID)
	if providerID == "" {
		return Result{}, fmt.Errorf("lifecycle: provider id is required")
	}
	if oldGroupID == "" || newGroupID == "" {
		return Result{}, fmt.Errorf("lifecycle: group id is required")
	}

	plan := emptyPlan(OpGroupIDChange, baseRevision)
	providerIndex, groupIndex, provider, ok := findProviderGroup(base, providerID, oldGroupID)
	if !ok {
		if providerIndex < 0 {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:provider_missing",
				Code:        ReasonProviderMissing,
				Disposition: DispositionBlocker,
				Path:        "/config/providers",
				Params:      params("providerId", providerID, "oldGroupId", oldGroupID, "newGroupId", newGroupID),
			})
		} else {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:group_missing",
				Code:        ReasonGroupMissing,
				Disposition: DispositionBlocker,
				Path:        fmt.Sprintf("/config/providers/%d/groups", providerIndex),
				Params:      params("providerId", providerID, "oldGroupId", oldGroupID, "newGroupId", newGroupID),
			})
		}
		finalizePlan(&plan)
		return Result{Plan: plan}, nil
	}
	if oldGroupID == newGroupID {
		finalizePlan(&plan)
		plan.Executable = true
		plan.NoOp = true
		return Result{Plan: plan, Candidate: CloneConfig(base)}, nil
	}
	for _, g := range provider.Groups {
		if normalizeGroupID(g.ID) == newGroupID {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:group_id_conflict",
				Code:        ReasonGroupIDConflict,
				Disposition: DispositionBlocker,
				Path:        fmt.Sprintf("/config/providers/%d/groups", providerIndex),
				Params:      params("providerId", providerID, "oldGroupId", oldGroupID, "newGroupId", newGroupID),
			})
			finalizePlan(&plan)
			return Result{Plan: plan}, nil
		}
	}

	plan.RequestedChanges = append(plan.RequestedChanges, Change{
		ID:         "requested:group_id_change:" + providerID + ":" + oldGroupID + ":" + newGroupID,
		Kind:       ChangeUpdate,
		Source:     SourceRequested,
		Entity:     EntityProviderGroup,
		ReasonCode: ReasonGroupIDChange,
		Path:       fmt.Sprintf("/config/providers/%d/groups/%d/id", providerIndex, groupIndex),
		Params: params(
			"providerId", providerID,
			"oldGroupId", oldGroupID,
			"newGroupId", newGroupID,
			"providerIndex", providerIndex,
			"groupIndex", groupIndex,
		),
	})
	plan.RuntimeImpact.GroupIDChanged = true
	plan.RuntimeImpact.RoutingChanged = true

	// Precise alias targets: old -> new, preserve ownership flags (Enabled/AutoGenerated/etc).
	// If a dangling (provider, newGroup, model) target already exists while newGroup is not a
	// real destination group, keep the routable old target properties, retarget it, and drop
	// the dangling new-side identity (never prefer dangling properties over the old target).
	for ai, alias := range base.Aliases {
		oldModels := map[string]bool{}
		for _, t := range alias.Targets {
			if t.Provider == providerID && targetGroupID(t) == oldGroupID {
				oldModels[t.Model] = true
			}
		}
		// Drop dangling new-side collisions first (recorded as automatic removes).
		for ti, target := range alias.Targets {
			if target.Provider != providerID || targetGroupID(target) != newGroupID {
				continue
			}
			if !oldModels[target.Model] {
				continue
			}
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:dedupe_dangling_target_on_group_id_change:%d:%d", ai, ti),
				Kind:       ChangeRemove,
				Source:     SourceAutomatic,
				Entity:     EntityAliasTarget,
				ReasonCode: ReasonGroupIDChange,
				Path:       fmt.Sprintf("/config/aliases/%d/targets/%d", ai, ti),
				Params: params(
					"alias", alias.Alias,
					"providerId", providerID,
					"oldGroupId", oldGroupID,
					"newGroupId", newGroupID,
					"model", target.Model,
					"aliasIndex", ai,
					"targetIndex", ti,
					"action", "dedupe_collision",
					"prefer", "routable_old",
					"autoGenerated", target.AutoGenerated,
					"enabled", target.Enabled,
				),
			})
		}
		// Always retarget old-side targets, preserving their properties.
		for ti, target := range alias.Targets {
			if target.Provider != providerID || targetGroupID(target) != oldGroupID {
				continue
			}
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:retarget_group:%d:%d", ai, ti),
				Kind:       ChangeUpdate,
				Source:     SourceAutomatic,
				Entity:     EntityAliasTarget,
				ReasonCode: ReasonGroupIDChange,
				Path:       fmt.Sprintf("/config/aliases/%d/targets/%d/group", ai, ti),
				Params: params(
					"alias", alias.Alias,
					"providerId", providerID,
					"oldGroupId", oldGroupID,
					"newGroupId", newGroupID,
					"model", target.Model,
					"aliasIndex", ai,
					"targetIndex", ti,
					"autoGenerated", target.AutoGenerated,
					"enabled", target.Enabled,
				),
			})
		}
	}

	// Precise rewrite selectors: old -> new (duplicate after rename is dropped).
	for ri, rule := range base.RequestRewriteRules {
		if rule.ProviderGroups == nil || len(rule.ProviderGroups) == 0 {
			continue
		}
		existingNewSel := false
		for _, sel := range rule.ProviderGroups {
			if sel.Provider == providerID && normalizeGroupID(sel.Group) == newGroupID {
				existingNewSel = true
				break
			}
		}
		for si, sel := range rule.ProviderGroups {
			if sel.Provider != providerID || normalizeGroupID(sel.Group) != oldGroupID {
				continue
			}
			if existingNewSel {
				plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
					ID:         fmt.Sprintf("auto:dedupe_rewrite_group_id:%d:%d", ri, si),
					Kind:       ChangeRemove,
					Source:     SourceAutomatic,
					Entity:     EntityRewriteRule,
					ReasonCode: ReasonGroupIDChange,
					Path:       fmt.Sprintf("/config/request_rewrite_rules/%d/provider_groups/%d", ri, si),
					Params: params(
						"ruleName", rule.Name,
						"ruleIndex", ri,
						"selectorIndex", si,
						"providerId", providerID,
						"oldGroupId", oldGroupID,
						"newGroupId", newGroupID,
						"action", "dedupe_collision",
					),
				})
				continue
			}
			plan.AutomaticChanges = append(plan.AutomaticChanges, Change{
				ID:         fmt.Sprintf("auto:rewrite_group_id:%d:%d", ri, si),
				Kind:       ChangeUpdate,
				Source:     SourceAutomatic,
				Entity:     EntityRewriteRule,
				ReasonCode: ReasonGroupIDChange,
				Path:       fmt.Sprintf("/config/request_rewrite_rules/%d/provider_groups/%d/group", ri, si),
				Params: params(
					"ruleName", rule.Name,
					"ruleIndex", ri,
					"selectorIndex", si,
					"providerId", providerID,
					"oldGroupId", oldGroupID,
					"newGroupId", newGroupID,
				),
			})
		}
	}

	finalizePlan(&plan)
	if !plan.Executable {
		return Result{Plan: plan}, nil
	}
	candidate := applyGroupIDChange(base, providerID, oldGroupID, newGroupID)
	return Result{Plan: plan, Candidate: candidate}, nil
}

func selectedGroupProtectedTargetChange(sel Selection, alias config.Alias, target config.Target, ai, ti int, path, providerID, removingGroupID string, base *config.Config) (Change, error) {
	baseChange := Change{
		ID:     fmt.Sprintf("selected:protected_target:%d:%d", ai, ti),
		Source: SourceSelection,
		Path:   path,
		Params: params(
			"choiceId", sel.ChoiceID,
			"optionId", sel.OptionID,
			"alias", alias.Alias,
			"providerId", target.Provider,
			"groupId", targetGroupID(target),
			"model", target.Model,
			"aliasIndex", ai,
			"targetIndex", ti,
		),
	}
	switch sel.OptionID {
	case OptionRemoveTarget:
		baseChange.Kind = ChangeRemove
		baseChange.Entity = EntityAliasTarget
		baseChange.ReasonCode = ReasonProtectedTarget
		return baseChange, nil
	case OptionDeleteAlias:
		baseChange.Kind = ChangeRemove
		baseChange.Entity = EntityAlias
		baseChange.ReasonCode = ReasonProtectedTarget
		return baseChange, nil
	case OptionRebindTarget:
		newProvider, _ := sel.Params["providerId"].(string)
		newGroup, _ := sel.Params["groupId"].(string)
		newModel, _ := sel.Params["model"].(string)
		newProvider = strings.TrimSpace(newProvider)
		// Require an explicit groupId string; never default empty to "default".
		newGroup = strings.TrimSpace(newGroup)
		newModel = strings.TrimSpace(newModel)
		if newProvider == "" || newGroup == "" || newModel == "" {
			return Change{}, fmt.Errorf("rebind_target requires params.providerId, params.groupId and params.model")
		}
		// Group remove rebind must stay on the same provider and an explicit surviving group.
		if newProvider != providerID {
			return Change{}, fmt.Errorf("rebind_target must keep the same provider %q", providerID)
		}
		if normalizeGroupID(newGroup) == normalizeGroupID(removingGroupID) {
			return Change{}, fmt.Errorf("rebind_target cannot target the group being removed")
		}
		if _, _, _, ok := findProviderGroup(base, newProvider, newGroup); !ok {
			return Change{}, fmt.Errorf("rebind_target group %q not found on provider %q", newGroup, newProvider)
		}
		baseChange.Kind = ChangeUpdate
		baseChange.Entity = EntityAliasTarget
		baseChange.ReasonCode = ReasonProtectedTarget
		baseChange.Params["newProviderId"] = newProvider
		baseChange.Params["newGroupId"] = newGroup
		baseChange.Params["newModel"] = newModel
		return baseChange, nil
	default:
		return Change{}, fmt.Errorf("unsupported option %q", sel.OptionID)
	}
}

func selectedSingletonProviderGroupsChange(sel Selection, rule config.RequestRewriteRule, ri int, path, providerID, removingGroupID string, cfg *config.Config) (Change, error) {
	change := Change{
		ID:     fmt.Sprintf("selected:singleton_provider_groups:%d", ri),
		Source: SourceSelection,
		Entity: EntityRewriteRule,
		Path:   path,
		Params: params(
			"choiceId", sel.ChoiceID,
			"optionId", sel.OptionID,
			"ruleName", rule.Name,
			"ruleIndex", ri,
			"providerId", providerID,
			"groupId", removingGroupID,
		),
	}
	switch sel.OptionID {
	case OptionKeepDormant:
		change.Kind = ChangeUpdate
		change.ReasonCode = ReasonSingletonRewrite
		change.Params["action"] = OptionKeepDormant
		return change, nil
	case OptionDisableRule:
		change.Kind = ChangeUpdate
		change.ReasonCode = ReasonSingletonRewrite
		change.Params["action"] = OptionDisableRule
		return change, nil
	case OptionDeleteRule:
		change.Kind = ChangeRemove
		change.ReasonCode = ReasonSingletonRewrite
		change.Params["action"] = OptionDeleteRule
		return change, nil
	case OptionReplaceProviderGroups:
		raw, ok := sel.Params["providerGroups"]
		if !ok {
			raw, ok = sel.Params["provider_groups"]
		}
		if !ok {
			return Change{}, fmt.Errorf("replace_provider_groups requires params.providerGroups")
		}
		groups, err := providerGroupSelectorParam(raw)
		if err != nil {
			return Change{}, err
		}
		if len(groups) == 0 {
			return Change{}, fmt.Errorf("replace_provider_groups must be non-empty (empty means wildcard)")
		}
		validated, err := validateReplaceProviderGroups(groups, cfg, providerID, removingGroupID)
		if err != nil {
			return Change{}, err
		}
		change.Kind = ChangeUpdate
		change.ReasonCode = ReasonSingletonRewrite
		change.Params["action"] = OptionReplaceProviderGroups
		change.Params["providerGroups"] = validated
		return change, nil
	default:
		return Change{}, fmt.Errorf("unsupported option %q", sel.OptionID)
	}
}

// validateReplaceProviderGroups checks each selector exists, rejects the group being
// removed, and dedupes. Invalid entries become plan blockers via the caller.
func validateReplaceProviderGroups(groups []config.ProviderGroupSelector, cfg *config.Config, removingProviderID, removingGroupID string) ([]config.ProviderGroupSelector, error) {
	removingGroupID = normalizeGroupID(removingGroupID)
	seen := map[string]bool{}
	out := make([]config.ProviderGroupSelector, 0, len(groups))
	for _, sel := range groups {
		p := strings.TrimSpace(sel.Provider)
		g := normalizeGroupID(sel.Group)
		if p == "" || g == "" {
			return nil, fmt.Errorf("replace_provider_groups entries require provider and group")
		}
		if p == removingProviderID && g == removingGroupID {
			return nil, fmt.Errorf("replace_provider_groups must not include the group being removed (%s/%s)", p, g)
		}
		if _, _, _, ok := findProviderGroup(cfg, p, g); !ok {
			return nil, fmt.Errorf("replace_provider_groups: provider/group %q/%q not found", p, g)
		}
		key := p + "\x00" + g
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, config.ProviderGroupSelector{Provider: p, Group: g})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("replace_provider_groups must be non-empty (empty means wildcard)")
	}
	return out, nil
}

func providerGroupSelectorParam(raw any) ([]config.ProviderGroupSelector, error) {
	switch v := raw.(type) {
	case []config.ProviderGroupSelector:
		out := make([]config.ProviderGroupSelector, 0, len(v))
		for _, sel := range v {
			p := strings.TrimSpace(sel.Provider)
			g := normalizeGroupID(sel.Group)
			if p == "" || g == "" {
				continue
			}
			out = append(out, config.ProviderGroupSelector{Provider: p, Group: g})
		}
		return out, nil
	case []any:
		out := make([]config.ProviderGroupSelector, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("providerGroups must be object array")
			}
			p, _ := m["provider"].(string)
			g, _ := m["group"].(string)
			p = strings.TrimSpace(p)
			g = normalizeGroupID(g)
			if p == "" || g == "" {
				return nil, fmt.Errorf("providerGroups entries require provider and group")
			}
			out = append(out, config.ProviderGroupSelector{Provider: p, Group: g})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("providerGroups must be object array")
	}
}

func applyGroupRemove(base *config.Config, providerID, groupID string, plan Plan) (*config.Config, error) {
	cfg := CloneConfig(base)
	groupID = normalizeGroupID(groupID)

	// Index selections by precise aliasIndex/targetIndex so duplicate
	// (alias, provider, group, model) identities never overwrite each other.
	deleteAliasIndexes := map[int]bool{}
	removeTargetIndexes := map[string]bool{} // "aliasIndex:targetIndex"
	rebindByIndex := map[string]config.Target{}
	rewriteActions := map[int]Change{}

	for _, ch := range plan.SelectedChanges {
		switch ch.Entity {
		case EntityAlias:
			if ai, ok := asInt(ch.Params["aliasIndex"]); ok {
				deleteAliasIndexes[ai] = true
			}
		case EntityAliasTarget:
			ai, aiOK := asInt(ch.Params["aliasIndex"])
			ti, tiOK := asInt(ch.Params["targetIndex"])
			if !aiOK || !tiOK {
				// Skip malformed selected changes rather than applying by non-unique identity.
				continue
			}
			key := indexKey(ai, ti)
			if ch.Kind == ChangeUpdate {
				newP, _ := ch.Params["newProviderId"].(string)
				newG, _ := ch.Params["newGroupId"].(string)
				newM, _ := ch.Params["newModel"].(string)
				rebindByIndex[key] = config.Target{
					Provider: newP,
					Group:    normalizeGroupID(newG),
					Model:    newM,
					Enabled:  true,
				}
			} else {
				removeTargetIndexes[key] = true
			}
		case EntityRewriteRule:
			if idx, ok := asInt(ch.Params["ruleIndex"]); ok {
				rewriteActions[idx] = ch
			}
		}
	}

	nextAliases := make([]config.Alias, 0, len(cfg.Aliases))
	for ai, alias := range cfg.Aliases {
		if deleteAliasIndexes[ai] {
			continue
		}
		nextTargets := make([]config.Target, 0, len(alias.Targets))
		for ti, t := range alias.Targets {
			key := indexKey(ai, ti)
			if t.Provider == providerID && targetGroupID(t) == groupID && TargetSystemOwned(alias, t) {
				continue
			}
			if removeTargetIndexes[key] {
				continue
			}
			if rebind, ok := rebindByIndex[key]; ok {
				t.Provider = rebind.Provider
				t.Group = rebind.Group
				t.Model = rebind.Model
				t.AutoGenerated = false
			}
			nextTargets = append(nextTargets, t)
		}
		alias.Targets = nextTargets
		nextAliases = append(nextAliases, alias)
	}
	cfg.Aliases = nextAliases

	// Remove the group from the provider.
	for i := range cfg.Providers {
		if cfg.Providers[i].ID != providerID {
			continue
		}
		nextGroups := make([]config.ProviderGroup, 0, len(cfg.Providers[i].Groups))
		for _, g := range cfg.Providers[i].Groups {
			if normalizeGroupID(g.ID) == groupID {
				continue
			}
			nextGroups = append(nextGroups, g)
		}
		cfg.Providers[i].Groups = nextGroups
		break
	}

	// Rewrite provider_groups.
	nextRules := make([]config.RequestRewriteRule, 0, len(cfg.RequestRewriteRules))
	for ri, rule := range cfg.RequestRewriteRules {
		if ch, ok := rewriteActions[ri]; ok {
			action, _ := ch.Params["action"].(string)
			switch action {
			case OptionKeepDormant:
				nextRules = append(nextRules, rule)
			case OptionDisableRule:
				rule.Enabled = false
				nextRules = append(nextRules, rule)
			case OptionDeleteRule:
				// drop
			case OptionReplaceProviderGroups:
				if groups, err := providerGroupSelectorParam(ch.Params["providerGroups"]); err == nil {
					rule.ProviderGroups = groups
				}
				nextRules = append(nextRules, rule)
			default:
				nextRules = append(nextRules, rule)
			}
			continue
		}
		if rule.ProviderGroups == nil || len(rule.ProviderGroups) == 0 {
			nextRules = append(nextRules, rule)
			continue
		}
		filtered := make([]config.ProviderGroupSelector, 0, len(rule.ProviderGroups))
		for _, sel := range rule.ProviderGroups {
			if sel.Provider == providerID && normalizeGroupID(sel.Group) == groupID {
				continue
			}
			filtered = append(filtered, sel)
		}
		if len(filtered) == 0 && len(rule.ProviderGroups) > 0 {
			// Unresolved singleton safety: keep original.
			nextRules = append(nextRules, rule)
			continue
		}
		rule.ProviderGroups = filtered
		nextRules = append(nextRules, rule)
	}
	cfg.RequestRewriteRules = nextRules
	return cfg, nil
}

func applyGroupIDChange(base *config.Config, providerID, oldGroupID, newGroupID string) *config.Config {
	cfg := CloneConfig(base)
	oldGroupID = normalizeGroupID(oldGroupID)
	newGroupID = normalizeGroupID(newGroupID)

	for i := range cfg.Providers {
		if cfg.Providers[i].ID != providerID {
			continue
		}
		for gi := range cfg.Providers[i].Groups {
			if normalizeGroupID(cfg.Providers[i].Groups[gi].ID) == oldGroupID {
				cfg.Providers[i].Groups[gi].ID = newGroupID
			}
		}
		break
	}

	for ai := range cfg.Aliases {
		// Models present on the routable old group. When a dangling newGroup identity
		// collides, drop the dangling entry and retarget the old target (preserving
		// Enabled/AutoGenerated and other fields).
		oldModels := map[string]bool{}
		for _, t := range cfg.Aliases[ai].Targets {
			if t.Provider == providerID && targetGroupID(t) == oldGroupID {
				oldModels[t.Model] = true
			}
		}
		next := make([]config.Target, 0, len(cfg.Aliases[ai].Targets))
		for _, t := range cfg.Aliases[ai].Targets {
			if t.Provider == providerID && targetGroupID(t) == newGroupID && oldModels[t.Model] {
				// Dangling same-identity collision: remove new-side target.
				continue
			}
			if t.Provider == providerID && targetGroupID(t) == oldGroupID {
				t.Group = newGroupID
			}
			next = append(next, t)
		}
		cfg.Aliases[ai].Targets = next
	}

	// Rewrite selectors: retarget old->new and dedupe.
	for ri := range cfg.RequestRewriteRules {
		rule := &cfg.RequestRewriteRules[ri]
		if rule.ProviderGroups == nil {
			continue
		}
		seen := map[string]bool{}
		next := make([]config.ProviderGroupSelector, 0, len(rule.ProviderGroups))
		for _, sel := range rule.ProviderGroups {
			g := normalizeGroupID(sel.Group)
			if sel.Provider == providerID && g == oldGroupID {
				g = newGroupID
			}
			key := sel.Provider + "\x00" + g
			if seen[key] {
				continue
			}
			seen[key] = true
			next = append(next, config.ProviderGroupSelector{Provider: sel.Provider, Group: g})
		}
		rule.ProviderGroups = next
	}
	return cfg
}

func findProviderGroup(base *config.Config, providerID, groupID string) (providerIndex, groupIndex int, provider config.Provider, ok bool) {
	providerIndex, groupIndex = -1, -1
	groupID = normalizeGroupID(groupID)
	for i, p := range base.Providers {
		if p.ID != providerID {
			continue
		}
		providerIndex = i
		provider = p
		for gi, g := range p.Groups {
			if normalizeGroupID(g.ID) == groupID {
				return i, gi, p, true
			}
		}
		return providerIndex, -1, p, false
	}
	return -1, -1, config.Provider{}, false
}

func normalizeGroupID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return config.DefaultGroupID
	}
	return id
}

func targetGroupID(t config.Target) string {
	return normalizeGroupID(t.Group)
}

func indexKey(aliasIndex, targetIndex int) string {
	return fmt.Sprintf("%d:%d", aliasIndex, targetIndex)
}
