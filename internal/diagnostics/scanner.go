package diagnostics

import (
	"fmt"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

type ScanOptions struct {
	// CatalogStates maps catalog freshness by group. Prefer CatalogStateKey(providerID, groupID).
	// A bare providerID key is only honored for the default group (legacy callers).
	CatalogStates map[string]string
}

// CatalogStateKey returns the stable CatalogStates map key for one provider group.
func CatalogStateKey(providerID, groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		groupID = config.DefaultGroupID
	}
	return providerID + "\x00" + groupID
}

// LookupCatalogState resolves a group's catalog state.
// Composite keys win; provider-only keys apply exclusively to the default group.
func LookupCatalogState(states map[string]string, providerID, groupID string) string {
	if len(states) == 0 {
		return ""
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		groupID = config.DefaultGroupID
	}
	if state := states[CatalogStateKey(providerID, groupID)]; state != "" {
		return state
	}
	if groupID == config.DefaultGroupID {
		return states[providerID]
	}
	return ""
}

func ScanConfig(cfg *config.Config, options ScanOptions) ([]Issue, error) {
	if cfg == nil {
		return nil, fmt.Errorf("scan config: nil config")
	}
	var issues []Issue
	providerIndexes := map[string][]int{}
	for i, provider := range cfg.Providers {
		providerIndexes[provider.ID] = append(providerIndexes[provider.ID], i)
	}

	// providerIndexesByID maps unique provider IDs to their sole index for group path building.
	providers := map[string]config.Provider{}
	providerIndexByID := map[string]int{}
	for id, indexes := range providerIndexes {
		if len(indexes) > 1 {
			issues = append(issues, ambiguous(CodeProviderIdentityAmbiguous, "provider", indexes, "/config/providers", Params{"providerId": id})...)
		} else {
			providers[id] = cfg.Providers[indexes[0]]
			providerIndexByID[id] = indexes[0]
		}
	}

	for i, provider := range cfg.Providers {
		providerPath := fmt.Sprintf("/config/providers/%d", i)
		providerKey := occurrenceKey(provider.ID, i, providerIndexes)
		if len(provider.Groups) == 0 {
			issues = append(issues, issue(CodeProviderGroupsEmpty, SeverityError, ReasonInvalid,
				providerPath+"/groups", Source{"provider", providerKey, providerPath}, nil, nil,
				Params{"providerId": provider.ID}))
			continue
		}

		groupIndexes := map[string][]int{}
		for j, group := range provider.Groups {
			groupIndexes[group.ID] = append(groupIndexes[group.ID], j)
		}
		for id, indexes := range groupIndexes {
			if id == "" || len(indexes) <= 1 {
				continue
			}
			issues = append(issues, ambiguousGroup(CodeProviderGroupIdentityAmbiguous, provider.ID, id, indexes, providerPath+"/groups")...)
		}

		for j, group := range provider.Groups {
			groupPath := fmt.Sprintf("%s/groups/%d", providerPath, j)
			groupKey := providerGroupKey(provider.ID, group.ID, j, groupIndexes)
			source := Source{"provider_group", groupKey, groupPath}

			if strings.TrimSpace(group.ID) == "" {
				issues = append(issues, issue(CodeProviderGroupIDEmpty, SeverityError, ReasonInvalid,
					groupPath+"/id", source, nil, nil,
					Params{"providerId": provider.ID, "groupIndex": j}))
			}

			protocol := strings.TrimSpace(group.Protocol)
			if protocol != "" && config.ValidateProtocol(protocol) != nil {
				params := Params{"providerId": provider.ID, "groupIndex": j, "protocol": protocol}
				if group.ID != "" {
					params["groupId"] = group.ID
				} else {
					// required groupId — use empty-safe sentinel via index-only path already covered;
					// still need groupId for registry: omit empty strings in normalize, so use placeholder index form.
					params["groupId"] = fmt.Sprintf("@index:%d", j)
				}
				issues = append(issues, issue(CodeProviderGroupProtocolUnknown, SeverityError, ReasonInvalid,
					groupPath+"/protocol", source, nil, nil, params))
			}

			if group.ID == "" || len(groupIndexes[group.ID]) > 1 {
				// Catalog and identity-bound issues require a unique group id.
				continue
			}
			if state := LookupCatalogState(options.CatalogStates, provider.ID, group.ID); validCatalogStates[state] {
				issues = append(issues, issue(CodeProviderCatalogStale, SeverityWarning, ReasonCatalogStale,
					groupPath+"/models", source, &Target{"model_catalog", groupKey, nil}, nil,
					Params{"providerId": provider.ID, "groupId": group.ID, "catalogState": state}))
			}
		}
	}

	aliasIndexes := map[string][]int{}
	for ai, alias := range cfg.Aliases {
		aliasIndexes[alias.Alias] = append(aliasIndexes[alias.Alias], ai)
	}
	for ai, alias := range cfg.Aliases {
		aliasPath := fmt.Sprintf("/config/aliases/%d", ai)
		aliasKey := occurrenceKey(alias.Alias, ai, aliasIndexes)
		aliasAmbiguous := len(aliasIndexes[alias.Alias]) > 1
		if !alias.Enabled {
			actions := []Action{ActionEnableAlias, ActionKeep}
			if aliasAmbiguous {
				actions = nil
			}
			issues = append(issues, issue(CodeAliasDisabled, SeverityInfo, ReasonDisabled, aliasPath+"/enabled", Source{"alias", aliasKey, aliasPath}, nil, actions, Params{"alias": alias.Alias}))
		}
		targetIDs := map[string][]int{}
		for ti, target := range alias.Targets {
			identity := targetIdentity(target)
			targetIDs[identity] = append(targetIDs[identity], ti)
		}
		missing, disabled, mismatch, uncertain := 0, 0, 0, 0
		for ti, target := range alias.Targets {
			targetPath := fmt.Sprintf("%s/targets/%d", aliasPath, ti)
			groupID := effectiveGroupID(target.Group)
			identity := targetIdentity(target)
			targetKey := occurrenceKey(identity, ti, targetIDs)
			params := Params{
				"alias":       alias.Alias,
				"targetIndex": ti,
				"providerId":  target.Provider,
				"groupId":     groupID,
				"model":       target.Model,
			}
			provider, exists := providers[target.Provider]
			ambiguousProvider := len(providerIndexes[target.Provider]) > 1
			actions := targetActions(alias)
			if aliasAmbiguous || len(targetIDs[identity]) > 1 {
				actions = nil
			}
			switch {
			case ambiguousProvider:
				// Provider ambiguity is authoritative; do not infer group availability
				// or executable actions from one occurrence. No sibling-group fallback.
				uncertain++
			case !exists:
				missing++
				issues = append(issues, issue(CodeAliasTargetProviderMissing, SeverityError, ReasonMissing, targetPath+"/provider", Source{"alias_target", targetKey, targetPath}, &Target{"provider", target.Provider, nil}, actions, params))
			default:
				group, groupIndex, groupOK := findUniqueGroup(provider, groupID)
				switch {
				case !groupOK:
					missing++
					// Exact group only — never fall back to default or sibling groups.
					issues = append(issues, issue(CodeAliasTargetGroupMissing, SeverityError, ReasonMissing, targetPath+"/group", Source{"alias_target", targetKey, targetPath}, &Target{"provider_group", CatalogStateKey(target.Provider, groupID), nil}, actions, params))
				case !config.ProtocolsMatch(alias.Protocol, group.Protocol):
					mismatch++
					params["aliasProtocol"] = config.NormalizeAliasProtocol(alias.Protocol)
					params["groupProtocol"] = config.NormalizeProviderProtocol(group.Protocol)
					groupPath := fmt.Sprintf("/config/providers/%d/groups/%d", providerIndexByID[target.Provider], groupIndex)
					issues = append(issues, issue(CodeAliasTargetProtocolMismatch, SeverityError, ReasonProtocolMismatch, targetPath+"/group", Source{"alias_target", targetKey, targetPath}, &Target{"provider_group", CatalogStateKey(target.Provider, groupID), &groupPath}, actions, params))
				case !target.Enabled:
					disabled++
					if !alias.AutoGenerated && !alias.Locked && !aliasAmbiguous && len(targetIDs[identity]) == 1 {
						actions = append(actions, ActionEnableTarget)
					}
					issues = append(issues, issue(CodeAliasTargetDisabled, SeverityInfo, ReasonDisabled, targetPath+"/enabled", Source{"alias_target", targetKey, targetPath}, nil, actions, params))
				case provider.Disabled:
					disabled++
					if actions != nil {
						actions = append(actions, ActionEnableProvider, ActionKeep)
					}
					issues = append(issues, issue(CodeAliasTargetProviderDisabled, SeverityWarning, ReasonDisabled, targetPath+"/provider", Source{"alias_target", targetKey, targetPath}, &Target{"provider", target.Provider, nil}, actions, params))
				case group.Disabled:
					disabled++
					if actions != nil {
						actions = append(actions, ActionEnableGroup, ActionKeep)
					}
					groupPath := fmt.Sprintf("/config/providers/%d/groups/%d", providerIndexByID[target.Provider], groupIndex)
					issues = append(issues, issue(CodeAliasTargetGroupDisabled, SeverityWarning, ReasonDisabled, targetPath+"/group", Source{"alias_target", targetKey, targetPath}, &Target{"provider_group", CatalogStateKey(target.Provider, groupID), &groupPath}, actions, params))
				default:
					if state := LookupCatalogState(options.CatalogStates, target.Provider, groupID); validCatalogStates[state] {
						params["catalogState"] = state
						if actions != nil {
							actions = append(actions, ActionRefreshCatalog, ActionKeep)
						}
						issues = append(issues, issue(CodeAliasTargetModelUnconfirmed, SeverityInfo, ReasonCatalogStale, targetPath+"/model", Source{"alias_target", targetKey, targetPath}, &Target{"model_symbol", target.Model, nil}, actions, params))
					}
				}
			}
		}
		for identity, indexes := range targetIDs {
			if len(indexes) > 1 {
				parts := strings.SplitN(identity, "\x00", 3)
				providerID, groupID, model := "", config.DefaultGroupID, ""
				if len(parts) > 0 {
					providerID = parts[0]
				}
				if len(parts) > 1 {
					groupID = parts[1]
				}
				if len(parts) > 2 {
					model = parts[2]
				}
				issues = append(issues, ambiguous(CodeAliasTargetIdentityAmbiguous, "alias_target", indexes, aliasPath+"/targets", Params{
					"alias": alias.Alias, "providerId": providerID, "groupId": groupID, "model": model,
				})...)
			}
		}
		if alias.Enabled && (len(alias.Targets) == 0 || missing+disabled+mismatch+uncertain == len(alias.Targets)) {
			issues = append(issues, issue(CodeAliasNoAvailableTarget, SeverityWarning, ReasonNoAvailableTarget, aliasPath, Source{"alias", aliasKey, aliasPath}, nil, nil, Params{"alias": alias.Alias, "targetCount": len(alias.Targets), "missingCount": missing, "disabledCount": disabled, "protocolMismatchCount": mismatch, "ambiguousCount": uncertain}))
		}
	}
	aliases := map[string]bool{}
	for id, indexes := range aliasIndexes {
		if len(indexes) > 1 {
			issues = append(issues, ambiguous(CodeAliasIdentityAmbiguous, "alias", indexes, "/config/aliases", Params{"alias": id})...)
		} else {
			aliases[id] = true
		}
	}

	rewriteIndexes := map[string][]int{}
	for ri, rule := range cfg.RequestRewriteRules {
		rewriteIndexes[rule.Name] = append(rewriteIndexes[rule.Name], ri)
	}
	for ri, rule := range cfg.RequestRewriteRules {
		rulePath := fmt.Sprintf("/config/request_rewrite_rules/%d", ri)
		ruleKey := occurrenceKey(rule.Name, ri, rewriteIndexes)
		ruleAmbiguous := len(rewriteIndexes[rule.Name]) > 1
		if !aliases[rule.Alias] && len(aliasIndexes[rule.Alias]) <= 1 {
			actions := []Action{ActionReplaceSelector, ActionDisableRule, ActionDeleteRule, ActionKeep}
			if ruleAmbiguous {
				actions = nil
			}
			issues = append(issues, issue(CodeRewriteAliasUnresolved, SeverityInfo, ReasonMissing, rulePath+"/alias", Source{"rewrite_rule", ruleKey, rulePath}, &Target{"alias", rule.Alias, nil}, actions, Params{"ruleName": rule.Name, "ruleIndex": ri, "alias": rule.Alias, "directFallbackPossible": true}))
		}
		if rule.ProviderGroups != nil {
			// Empty provider_groups is explicit wildcard — no missing selectors.
			for si, sel := range rule.ProviderGroups {
				providerID := strings.TrimSpace(sel.Provider)
				groupID := strings.TrimSpace(sel.Group)
				if len(providerIndexes[providerID]) > 1 {
					continue
				}
				provider, providerExists := providers[providerID]
				groupOK := false
				if providerExists && groupID != "" {
					_, _, groupOK = findUniqueGroup(provider, groupID)
				}
				if providerExists && groupOK {
					continue
				}
				actions := []Action{ActionReplaceSelector, ActionDisableRule, ActionDeleteRule, ActionKeep}
				if len(rule.ProviderGroups) > 1 {
					actions = append(actions, ActionRemoveSelector)
				}
				if ruleAmbiguous {
					actions = nil
				}
				// Exact (provider, group) only — do not fall back to default or siblings.
				params := Params{
					"ruleName":        rule.Name,
					"ruleIndex":       ri,
					"providerId":      providerID,
					"groupId":         groupID,
					"selectorIndex":   si,
					"selectorCount":   len(rule.ProviderGroups),
					"wildcardIfEmpty": true,
				}
				// normalizeParams drops empty strings; ensure required groupId/providerId survive.
				if providerID == "" {
					params["providerId"] = fmt.Sprintf("@index:%d", si)
				}
				if groupID == "" {
					params["groupId"] = fmt.Sprintf("@index:%d", si)
				}
				var target *Target
				if providerExists {
					target = &Target{"provider_group", CatalogStateKey(providerID, groupID), nil}
				} else if providerID != "" {
					target = &Target{"provider", providerID, nil}
				}
				issues = append(issues, issue(CodeRewriteProviderGroupMissing, SeverityWarning, ReasonMissing,
					fmt.Sprintf("%s/provider_groups/%d", rulePath, si),
					Source{"rewrite_rule", ruleKey, rulePath}, target, actions, params))
			}
		}
		// nil/empty ProviderGroups is wildcard — no per-provider missing selectors.
	}
	for id, indexes := range rewriteIndexes {
		if len(indexes) > 1 {
			issues = append(issues, ambiguous(CodeRewriteIdentityAmbiguous, "rewrite_rule", indexes, "/config/request_rewrite_rules", Params{"ruleName": id})...)
		}
	}
	for pi, providerID := range cfg.ProviderPriority {
		if len(providerIndexes[providerID]) > 1 {
			continue
		}
		if _, exists := providers[providerID]; !exists {
			issues = append(issues, issue(CodePriorityProviderMissing, SeverityInfo, ReasonMissing, fmt.Sprintf("/config/provider_priority/%d", pi), Source{"priority_entry", fmt.Sprintf("@index:%d", pi), fmt.Sprintf("/config/provider_priority/%d", pi)}, &Target{"provider", providerID, nil}, []Action{ActionRemovePriorityEntry, ActionKeep}, Params{"providerId": providerID, "priorityIndex": pi}))
		}
	}
	return SortAndDedupe(issues)
}

func issue(code Code, severity Severity, reason Reason, path string, source Source, target *Target, actions []Action, params Params) Issue {
	return Issue{SchemaVersion: SchemaVersion, Code: code, Severity: severity, Path: path, Source: source, Target: target, Reason: reason, AllowedActions: actions, Params: params}
}

func ambiguous(code Code, kind EntityKind, indexes []int, root string, params Params) []Issue {
	paths := make([]string, len(indexes))
	for i, index := range indexes {
		paths[i] = fmt.Sprintf("%s/%d", root, index)
	}
	issues := make([]Issue, 0, len(indexes))
	for _, index := range indexes {
		occurrenceParams := make(Params, len(params)+2)
		for key, value := range params {
			occurrenceParams[key] = value
		}
		occurrenceParams["occurrenceCount"] = len(indexes)
		occurrenceParams["occurrencePaths"] = paths
		path := fmt.Sprintf("%s/%d", root, index)
		issues = append(issues, issue(code, SeverityError, ReasonAmbiguous, path, Source{kind, fmt.Sprintf("@index:%d", index), path}, nil, nil, occurrenceParams))
	}
	return issues
}

func ambiguousGroup(code Code, providerID, groupID string, indexes []int, root string) []Issue {
	return ambiguous(code, "provider_group", indexes, root, Params{"providerId": providerID, "groupId": groupID})
}

func targetActions(alias config.Alias) []Action {
	if alias.AutoGenerated {
		return []Action{ActionRebindTarget, ActionDeleteAlias, ActionKeep}
	}
	if alias.Locked {
		return []Action{ActionDeleteAlias, ActionKeep}
	}
	return []Action{ActionRebindTarget, ActionRemoveTarget, ActionDeleteAlias}
}

func occurrenceKey(value string, index int, indexes map[string][]int) string {
	if value == "" || len(indexes[value]) > 1 {
		return fmt.Sprintf("@index:%d", index)
	}
	return value
}

func providerGroupKey(providerID, groupID string, index int, groupIndexes map[string][]int) string {
	if groupID == "" || len(groupIndexes[groupID]) > 1 {
		return fmt.Sprintf("@index:%d", index)
	}
	return CatalogStateKey(providerID, groupID)
}

func effectiveGroupID(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return config.DefaultGroupID
	}
	return group
}

func targetIdentity(target config.Target) string {
	return target.Provider + "\x00" + effectiveGroupID(target.Group) + "\x00" + target.Model
}

// findUniqueGroup returns the group and its index only when the id is unique and present.
// Duplicate group ids are treated as not found so callers do not silently pick one sibling.
func findUniqueGroup(provider config.Provider, groupID string) (config.ProviderGroup, int, bool) {
	groupID = effectiveGroupID(groupID)
	found := -1
	for i, group := range provider.Groups {
		if group.ID != groupID {
			continue
		}
		if found >= 0 {
			return config.ProviderGroup{}, -1, false
		}
		found = i
	}
	if found < 0 {
		return config.ProviderGroup{}, -1, false
	}
	return provider.Groups[found], found, true
}
