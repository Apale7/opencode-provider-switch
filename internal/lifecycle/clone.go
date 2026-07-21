package lifecycle

import (
	"encoding/json"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// CloneConfig deep-copies exported config fields into a detached snapshot.
// path/mu are intentionally not preserved; planners never touch disk.
func CloneConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	type wire struct {
		Server              config.Server               `json:"server"`
		Admin               config.Admin                `json:"admin"`
		Desktop             config.Desktop              `json:"desktop"`
		Providers           []config.Provider           `json:"providers"`
		Aliases             []config.Alias              `json:"aliases"`
		RequestRewriteRules []config.RequestRewriteRule `json:"request_rewrite_rules"`
		ProviderPriority    []string                    `json:"provider_priority"`
		AutoAliasEnabled    bool                        `json:"auto_alias_enabled"`
	}
	src := wire{
		Server:              cfg.Server,
		Admin:               cfg.Admin,
		Desktop:             cfg.Desktop,
		Providers:           cfg.Providers,
		Aliases:             cfg.Aliases,
		RequestRewriteRules: cfg.RequestRewriteRules,
		ProviderPriority:    cfg.ProviderPriority,
		AutoAliasEnabled:    cfg.AutoAliasEnabled,
	}
	raw, err := json.Marshal(src)
	if err != nil {
		out := config.Default()
		return out
	}
	var dst wire
	if err := json.Unmarshal(raw, &dst); err != nil {
		out := config.Default()
		return out
	}
	out := config.Default()
	out.Server = dst.Server
	out.Admin = dst.Admin
	out.Desktop = dst.Desktop
	out.Providers = dst.Providers
	if out.Providers == nil {
		out.Providers = []config.Provider{}
	}
	out.Aliases = dst.Aliases
	if out.Aliases == nil {
		out.Aliases = []config.Alias{}
	}
	out.RequestRewriteRules = dst.RequestRewriteRules
	if out.RequestRewriteRules == nil {
		out.RequestRewriteRules = []config.RequestRewriteRule{}
	}
	out.ProviderPriority = dst.ProviderPriority
	if out.ProviderPriority == nil {
		out.ProviderPriority = []string{}
	}
	out.AutoAliasEnabled = dst.AutoAliasEnabled
	return out
}

func emptyPlan(kind, baseRevision string) Plan {
	return Plan{
		ContractVersion:  ContractVersion,
		PlannerVersion:   PlannerVersion,
		BaseRevision:     baseRevision,
		OperationKind:    kind,
		RequestedChanges: []Change{},
		AutomaticChanges: []Change{},
		SelectedChanges:  []Change{},
		Blockers:         []Issue{},
		Choices:          []Choice{},
		PreservedIssues:  []Issue{},
	}
}

func selectionMap(selections []Selection) map[string]Selection {
	out := make(map[string]Selection, len(selections))
	for _, s := range selections {
		if s.ChoiceID == "" {
			continue
		}
		out[s.ChoiceID] = s
	}
	return out
}

func params(kv ...any) map[string]any {
	if len(kv)%2 != 0 {
		panic("lifecycle: params requires even key/value count")
	}
	out := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, _ := kv[i].(string)
		out[key] = kv[i+1]
	}
	return out
}

func finalizePlan(plan *Plan) {
	if plan.RequestedChanges == nil {
		plan.RequestedChanges = []Change{}
	}
	if plan.AutomaticChanges == nil {
		plan.AutomaticChanges = []Change{}
	}
	if plan.SelectedChanges == nil {
		plan.SelectedChanges = []Change{}
	}
	if plan.Blockers == nil {
		plan.Blockers = []Issue{}
	}
	if plan.Choices == nil {
		plan.Choices = []Choice{}
	}
	if plan.PreservedIssues == nil {
		plan.PreservedIssues = []Issue{}
	}
	// Drop stale selection-required blockers before recompute.
	filtered := plan.Blockers[:0]
	for _, issue := range plan.Blockers {
		if issue.Code == ReasonSelectionRequired {
			continue
		}
		filtered = append(filtered, issue)
	}
	plan.Blockers = filtered

	for _, choice := range plan.Choices {
		found := false
		for _, ch := range plan.SelectedChanges {
			if ch.Params != nil {
				if id, ok := ch.Params["choiceId"].(string); ok && id == choice.ID {
					found = true
					break
				}
			}
		}
		if !found {
			plan.Blockers = append(plan.Blockers, Issue{
				ID:          "blocker:" + choice.ID,
				Code:        ReasonSelectionRequired,
				Disposition: DispositionBlocker,
				Path:        choice.Path,
				Params:      params("choiceId", choice.ID, "code", choice.Code),
			})
		}
	}
	hasBlocker := false
	for _, issue := range plan.Blockers {
		if issue.Disposition == DispositionBlocker {
			hasBlocker = true
			break
		}
	}
	plan.Executable = !hasBlocker
	if plan.Executable && len(plan.RequestedChanges)+len(plan.AutomaticChanges)+len(plan.SelectedChanges) == 0 {
		plan.NoOp = true
	}
}
