package app

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/configstore"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
)

func (s *Service) ListRequestRewriteRules(ctx context.Context) ([]RequestRewriteRuleView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	return requestRewriteRuleViews(cfg.RequestRewriteRulesSnapshot()), nil
}

func (s *Service) UpsertRequestRewriteRule(ctx context.Context, in RequestRewriteRuleInput) (RequestRewriteRuleView, error) {
	rule := config.RequestRewriteRule{
		Name:      strings.TrimSpace(in.Name),
		Alias:     strings.TrimSpace(in.Alias),
		Providers: append([]string(nil), in.Providers...),
		Enabled:   in.Enabled,
		Override:  in.Override,
		Ops:       cloneRequestRewriteOperations(in.Ops),
	}
	if rule.Name == "" {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule name is required")
	}
	var view RequestRewriteRuleView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.UpsertRequestRewriteRule(rule)
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		current := cfg.FindRequestRewriteRule(rule.Name)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("request rewrite rule %q not found", rule.Name)
		}
		view = requestRewriteRuleView(*current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) SetRequestRewriteRuleEnabled(ctx context.Context, in RequestRewriteRuleStateInput) (RequestRewriteRuleView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule name is required")
	}
	var view RequestRewriteRuleView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if !cfg.SetRequestRewriteRuleEnabled(name, in.Enabled) {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("request rewrite rule %q not found", in.Name)
		}
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		current := cfg.FindRequestRewriteRule(name)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("request rewrite rule %q not found", name)
		}
		view = requestRewriteRuleView(*current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) RemoveRequestRewriteRule(ctx context.Context, in RequestRewriteRuleRemoveInput) (RequestRewriteRuleRemoveResult, error) {
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if !cfg.RemoveRequestRewriteRule(strings.TrimSpace(in.Name)) {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("request rewrite rule %q not found", in.Name)
		}
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		return RequestRewriteRuleRemoveResult{}, err
	}
	return RequestRewriteRuleRemoveResult{OK: true}, nil
}

func (s *Service) ReorderRequestRewriteRules(ctx context.Context, in RequestRewriteRuleReorderInput) (RequestRewriteRuleReorderResult, error) {
	var rules []RequestRewriteRuleView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if err := cfg.ReorderRequestRewriteRules(in.Names); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		rules = requestRewriteRuleViews(cfg.RequestRewriteRulesSnapshot())
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		return RequestRewriteRuleReorderResult{}, err
	}
	return RequestRewriteRuleReorderResult{Rules: rules}, nil
}

func (s *Service) UpsertProvider(ctx context.Context, in ProviderUpsertInput) (ProviderSaveResult, error) {
	_ = ctx
	if strings.TrimSpace(in.ID) == "" {
		return ProviderSaveResult{}, fmt.Errorf("provider id is required")
	}
	protocol := config.NormalizeProviderProtocol(strings.TrimSpace(in.Protocol))
	if err := config.ValidateProviderBaseURLs(protocol, in.BaseURL, in.BaseURLs); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("invalid baseUrl: %w", err)
	}
	if err := config.ValidateProviderBaseURLStrategy(in.BaseURLStrategy); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("invalid baseUrlStrategy: %w", err)
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderSaveResult{}, err
	}
	baseURLs := config.NormalizeProviderBaseURLs(in.BaseURL, in.BaseURLs)
	warnings := []string{}
	provider := config.Provider{
		ID:              strings.TrimSpace(in.ID),
		Name:            strings.TrimSpace(in.Name),
		Protocol:        protocol,
		BaseURL:         baseURLs[0],
		BaseURLs:        append([]string(nil), baseURLs...),
		BaseURLStrategy: config.NormalizeProviderBaseURLStrategy(in.BaseURLStrategy),
		APIKey:          firstProviderAPIKey(in.APIKey, in.APIKeys),
		APIKeys:         providerAPIKeyRemainder(in.APIKey, in.APIKeys),
		Headers:         normalizeProviderHeaders(in.Headers),
		Disabled:        in.Disabled,
	}
	var existing *config.Provider
	if cur := cfg.FindProvider(provider.ID); cur != nil {
		existing = cur
		if provider.Name == "" {
			provider.Name = cur.Name
		}
		if len(provider.EffectiveAPIKeys()) == 0 && !in.ClearAPIKeys {
			provider.APIKey = cur.APIKey
			provider.APIKeys = append([]string(nil), cur.APIKeys...)
		}
		if len(provider.Headers) == 0 && !in.ClearHeaders && len(cur.Headers) > 0 {
			provider.Headers = cloneHeaders(cur.Headers)
		}
		provider.Models = append([]string(nil), cur.Models...)
		provider.ModelsSource = cur.ModelsSource
		if !providerConnectionEqual(*cur, provider) {
			provider.ModelsSource = ""
		}
		// nil autoAliasEnabled on update keeps the existing value.
		if in.AutoAliasEnabled != nil {
			v := *in.AutoAliasEnabled
			provider.AutoAliasEnabled = &v
		} else if cur.AutoAliasEnabled != nil {
			v := *cur.AutoAliasEnabled
			provider.AutoAliasEnabled = &v
		}
	} else if in.AutoAliasEnabled != nil {
		v := *in.AutoAliasEnabled
		provider.AutoAliasEnabled = &v
	} else {
		// Create with nil => default enabled (persist explicit true for clarity).
		enabled := true
		provider.AutoAliasEnabled = &enabled
	}
	if !in.SkipModels {
		warnings = append(warnings, discoverProviderModels(&provider, existing)...)
	} else if existing != nil && !providerConnectionEqual(*existing, provider) {
		provider.Models = append([]string(nil), existing.Models...)
		provider.ModelsSource = ""
		warnings = append(warnings, "provider connection changed with skip models enabled; keeping existing model catalog as untrusted")
	}
	_, err = s.commitConfig(ctx, "", func(_ context.Context, live *config.Config) (configstore.Mutation[*config.Config], error) {
		live.UpsertProvider(provider)
		warnings = append(warnings, appendAutoAliasWarnings(live, provider)...)
		return configstore.Mutation[*config.Config]{Value: live, Changed: true}, nil
	})
	if err != nil {
		return ProviderSaveResult{}, err
	}
	return ProviderSaveResult{Provider: providerView(provider), Warnings: warnings}, nil
}

func (s *Service) RefreshProviderModels(ctx context.Context, in ProviderRefreshModelsInput) (ProviderSaveResult, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return ProviderSaveResult{}, fmt.Errorf("provider id is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderSaveResult{}, err
	}
	existing := cfg.FindProvider(id)
	if existing == nil {
		return ProviderSaveResult{}, fmt.Errorf("provider %q not found", id)
	}
	provider := *existing
	warnings := discoverProviderModels(&provider, existing)
	_, err = s.commitConfig(ctx, "", func(_ context.Context, live *config.Config) (configstore.Mutation[*config.Config], error) {
		live.UpsertProvider(provider)
		warnings = append(warnings, appendAutoAliasWarnings(live, provider)...)
		return configstore.Mutation[*config.Config]{Value: live, Changed: true}, nil
	})
	if err != nil {
		return ProviderSaveResult{}, err
	}
	return ProviderSaveResult{Provider: providerView(provider), Warnings: warnings}, nil
}

func (s *Service) PingProviderBaseURL(ctx context.Context, in ProviderPingInput) (ProviderPingResult, error) {
	id := strings.TrimSpace(in.ID)
	baseURL := config.NormalizeProviderBaseURL(in.BaseURL)
	protocol := config.NormalizeProviderProtocol(strings.TrimSpace(in.Protocol))
	if id == "" && protocol == "" {
		return ProviderPingResult{}, fmt.Errorf("provider id or protocol is required")
	}
	if baseURL == "" {
		return ProviderPingResult{}, fmt.Errorf("baseUrl is required")
	}
	provider := &config.Provider{
		ID:       id,
		Protocol: protocol,
		BaseURL:  baseURL,
		BaseURLs: []string{baseURL},
		APIKey:   firstProviderAPIKey(in.APIKey, in.APIKeys),
		APIKeys:  providerAPIKeyRemainder(in.APIKey, in.APIKeys),
		Headers:  normalizeProviderHeaders(in.Headers),
	}
	if id != "" {
		cfg, err := s.loadConfig()
		if err != nil {
			return ProviderPingResult{}, err
		}
		existing := cfg.FindProvider(id)
		if existing != nil {
			provider = existing
			if protocol != "" {
				provider.Protocol = protocol
			}
			provider.BaseURL = baseURL
			provider.BaseURLs = config.NormalizeProviderBaseURLs(baseURL, []string{baseURL})
			if len(config.NormalizeProviderAPIKeys(in.APIKey, in.APIKeys)) > 0 {
				provider.APIKey = firstProviderAPIKey(in.APIKey, in.APIKeys)
				provider.APIKeys = providerAPIKeyRemainder(in.APIKey, in.APIKeys)
			}
			if len(in.Headers) > 0 {
				provider.Headers = normalizeProviderHeaders(in.Headers)
			}
		} else if protocol == "" {
			return ProviderPingResult{}, fmt.Errorf("provider %q not found", id)
		}
	}
	if provider.Protocol == "" {
		return ProviderPingResult{}, fmt.Errorf("provider protocol is required")
	}
	startedAt := time.Now()
	probe, err := opencode.ProbeProviderBaseURLWithAuthFallback(ctx, provider.Protocol, baseURL, provider.EffectiveAPIKeys(), provider.Headers)
	latency := time.Since(startedAt).Milliseconds()
	result := ProviderPingResult{
		ID:        id,
		BaseURL:   baseURL,
		LatencyMs: latency,
	}
	if probe != nil {
		result.StatusCode = probe.StatusCode
		result.Reachable = probe.Reachable
		if probe.LatencyMs > 0 {
			result.LatencyMs = probe.LatencyMs
		}
		result.Error = probe.Error
	}
	if err != nil {
		if result.Error == "" {
			result.Error = err.Error()
		}
		return result, err
	}
	return result, nil
}

func (s *Service) RemoveProvider(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	// Convenience path: plan with no explicit selections. Protected targets block.
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return err
	}
	planned, err := lifecycle.PlanProviderRemove(cfg, string(rev), id, nil)
	if err != nil {
		return err
	}
	if !planned.Plan.Executable {
		return &OutcomeError{
			Code: "plan_not_executable",
			Params: map[string]any{
				"operationKind": lifecycle.OpProviderRemove,
				"providerId":    id,
				"blockerCount":  len(planned.Plan.Blockers),
				"choiceCount":   len(planned.Plan.Choices),
			},
		}
	}
	if planned.Plan.NoOp || planned.Candidate == nil {
		if cfg.FindProvider(id) == nil {
			return fmt.Errorf("provider %q not found", id)
		}
		return nil
	}
	_, err = s.commitConfigReplace(ctx, rev, planned.Candidate)
	return err
}

// RemoveProviderWithPlan executes provider removal with explicit lifecycle selections.
func (s *Service) RemoveProviderWithPlan(ctx context.Context, id string, selections []lifecycle.Selection) error {
	id = strings.TrimSpace(id)
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return err
	}
	planned, err := lifecycle.PlanProviderRemove(cfg, string(rev), id, selections)
	if err != nil {
		return err
	}
	if !planned.Plan.Executable {
		return &OutcomeError{
			Code: "plan_not_executable",
			Params: map[string]any{
				"operationKind": lifecycle.OpProviderRemove,
				"providerId":    id,
				"blockerCount":  len(planned.Plan.Blockers),
				"choiceCount":   len(planned.Plan.Choices),
			},
		}
	}
	if planned.Candidate == nil {
		return nil
	}
	_, err = s.commitConfigReplace(ctx, rev, planned.Candidate)
	return err
}

func (s *Service) SetProviderPriority(ctx context.Context, in ProviderPriorityInput) (ProviderPriorityResult, error) {
	var warnings []string
	var ordered []string
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		warnings = providerPriorityInputWarnings(cfg, in.OrderedIDs)
		cfg.SetProviderPriority(in.OrderedIDs)
		ordered = cfg.ProviderPriorityOrder()
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		return ProviderPriorityResult{}, err
	}
	return ProviderPriorityResult{OrderedIDs: ordered, Warnings: warnings}, nil
}

func (s *Service) GetProviderPriority(ctx context.Context) (ProviderPriorityResult, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderPriorityResult{}, err
	}
	return ProviderPriorityResult{OrderedIDs: cfg.ProviderPriorityOrder()}, nil
}

func (s *Service) GetAutoAliasSettings(ctx context.Context) (AutoAliasSettingsResult, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return AutoAliasSettingsResult{}, err
	}
	return AutoAliasSettingsResult{Enabled: cfg.IsAutoAliasEnabled()}, nil
}

func (s *Service) SetAutoAliasSettings(ctx context.Context, in AutoAliasSettingsInput) (AutoAliasSettingsResult, error) {
	var enabled bool
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		cfg.SetAutoAliasEnabled(in.Enabled)
		enabled = cfg.IsAutoAliasEnabled()
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		return AutoAliasSettingsResult{}, err
	}
	return AutoAliasSettingsResult{Enabled: enabled}, nil
}

func (s *Service) UpgradeAutoAlias(ctx context.Context, in AliasLockInput) (AliasView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return AliasView{}, fmt.Errorf("alias name is required")
	}
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return AliasView{}, err
	}
	planned, err := lifecycle.PlanAliasUpgrade(cfg, string(rev), name)
	if err != nil {
		return AliasView{}, err
	}
	if !planned.Plan.Executable {
		return AliasView{}, &OutcomeError{
			Code:   "plan_not_executable",
			Params: map[string]any{"operationKind": lifecycle.OpAliasUpgrade, "alias": name},
		}
	}
	out := planned.Candidate
	if out == nil {
		out = cfg
	} else {
		if _, err := s.commitConfigReplace(ctx, rev, out); err != nil {
			return AliasView{}, err
		}
	}
	idx, err := lifecycle.RequireUniqueAlias(out, name)
	if err != nil {
		return AliasView{}, err
	}
	return aliasView(out, out.Aliases[idx]), nil
}

func (s *Service) SetAliasTargetDisabled(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if err := lifecycle.RequireManualAliasMutation(cfg, alias); err != nil {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Err: err}
		}
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		updated := *current
		found := false
		for i := range updated.Targets {
			if updated.Targets[i].Provider == providerID && updated.Targets[i].Model == model {
				updated.Targets[i].Enabled = !in.Disabled
				found = true
				break
			}
		}
		if !found {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("target %s/%s not found on alias %s", providerID, model, alias)
		}
		cfg.UpsertAlias(updated)
		view = aliasView(cfg, updated)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) SetProviderDisabled(ctx context.Context, in ProviderStateInput) (ProviderView, error) {
	id := strings.TrimSpace(in.ID)
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return ProviderView{}, err
	}
	planned, err := lifecycle.PlanProviderDisabled(cfg, string(rev), id, in.Disabled)
	if err != nil {
		return ProviderView{}, err
	}
	if !planned.Plan.Executable {
		return ProviderView{}, &OutcomeError{Code: "plan_not_executable", Params: map[string]any{"providerId": id}}
	}
	out := planned.Candidate
	if out == nil {
		out = cfg
	} else if !planned.Plan.NoOp {
		if _, err := s.commitConfigReplace(ctx, rev, out); err != nil {
			return ProviderView{}, err
		}
	}
	p := out.FindProvider(id)
	if p == nil {
		return ProviderView{}, fmt.Errorf("provider %q not found", id)
	}
	return providerView(*p), nil
}

func (s *Service) ImportProviders(ctx context.Context, in ProviderImportInput) (ProviderImportResult, error) {
	_ = ctx
	sourcePath := strings.TrimSpace(in.SourcePath)
	if sourcePath == "" {
		p, existed := opencode.ResolveGlobalConfigPath()
		if !existed {
			return ProviderImportResult{}, fmt.Errorf("no OpenCode config found at %s; use sourcePath to specify", p)
		}
		sourcePath = p
	}
	raw, err := opencode.Load(sourcePath)
	if err != nil {
		return ProviderImportResult{}, err
	}
	imports := opencode.ImportCustomProviders(raw)
	result := ProviderImportResult{SourcePath: sourcePath}
	if len(imports) == 0 {
		return result, nil
	}
	_, err = s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		changed := false
		for _, ip := range imports {
			if !in.Overwrite && cfg.FindProvider(ip.ID) != nil {
				result.Skipped++
				result.Warnings = append(result.Warnings, fmt.Sprintf("skip %q (already exists, enable overwrite to replace it)", ip.ID))
				continue
			}
			baseURL := config.NormalizeProviderBaseURL(ip.BaseURL)
			if err := config.ValidateProviderBaseURL(ip.Protocol, baseURL); err != nil {
				result.Skipped++
				result.Warnings = append(result.Warnings, fmt.Sprintf("skip %q (invalid baseURL %q: %v)", ip.ID, ip.BaseURL, err))
				continue
			}
			merged := mergeImportedProvider(cfg.FindProvider(ip.ID), opencode.ImportableProvider{
				ID:       ip.ID,
				Name:     ip.Name,
				Protocol: ip.Protocol,
				BaseURL:  baseURL,
				APIKey:   ip.APIKey,
				Headers:  ip.Headers,
				Models:   ip.Models,
			})
			cfg.UpsertProvider(merged)
			result.Imported++
			result.Providers = append(result.Providers, providerView(merged))
			changed = true
		}
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: changed}, nil
	})
	if err != nil {
		return ProviderImportResult{}, err
	}
	return result, nil
}

func (s *Service) UpsertAlias(ctx context.Context, in AliasUpsertInput) (AliasView, error) {
	name := strings.TrimSpace(in.Alias)
	if name == "" {
		return AliasView{}, fmt.Errorf("alias name is required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		// Any existing auto alias must be upgraded first (manual-only lookup is insufficient).
		if err := lifecycle.RequireManualAliasMutation(cfg, name); err != nil {
			// Allow create when alias is missing.
			if !strings.Contains(err.Error(), lifecycle.ReasonAliasMissing) {
				return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Err: err}
			}
		}
		a := config.Alias{Alias: name, DisplayName: strings.TrimSpace(in.DisplayName), Protocol: config.NormalizeAliasProtocol(strings.TrimSpace(in.Protocol)), Enabled: !in.Disabled}
		if existing := cfg.FindAlias(name); existing != nil {
			if a.DisplayName == "" {
				a.DisplayName = existing.DisplayName
			}
			if strings.TrimSpace(in.Protocol) == "" {
				a.Protocol = existing.Protocol
			}
			a.Targets = existing.Targets
		}
		cfg.UpsertAlias(a)
		view = aliasView(cfg, a)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) RemoveAlias(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return err
	}
	// Default selections: keep rewrite rules (explicit keep for each impact).
	planned, err := lifecycle.PlanAliasRemove(cfg, string(rev), name, nil, lifecycle.ExternalRefs{})
	if err != nil {
		return err
	}
	// Auto-resolve rewrite impacts as keep_rule for legacy RemoveAlias convenience.
	if !planned.Plan.Executable && len(planned.Plan.Choices) > 0 {
		selections := make([]lifecycle.Selection, 0, len(planned.Plan.Choices))
		for _, choice := range planned.Plan.Choices {
			if choice.Code == lifecycle.ReasonRewriteSelectorImpact {
				selections = append(selections, lifecycle.Selection{ChoiceID: choice.ID, OptionID: lifecycle.OptionKeepRule})
			}
		}
		planned, err = lifecycle.PlanAliasRemove(cfg, string(rev), name, selections, lifecycle.ExternalRefs{})
		if err != nil {
			return err
		}
	}
	if !planned.Plan.Executable {
		return &OutcomeError{
			Code: "plan_not_executable",
			Params: map[string]any{
				"operationKind": lifecycle.OpAliasRemove,
				"alias":         name,
				"blockerCount":  len(planned.Plan.Blockers),
			},
		}
	}
	if planned.Candidate == nil {
		return fmt.Errorf("alias %q not found", name)
	}
	_, err = s.commitConfigReplace(ctx, rev, planned.Candidate)
	return err
}

func (s *Service) BindAliasTarget(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		p := cfg.FindProvider(providerID)
		if p == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q does not exist; add it first", providerID)
		}
		providerProtocol := config.NormalizeProviderProtocol(p.Protocol)
		if err := validateProviderModelKnown(providerID, p.Models, p.ModelsSource, model); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		indexes := lifecycle.FindAliasesByName(cfg, alias)
		if len(indexes) > 1 {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Params: map[string]any{"reason": lifecycle.ReasonAliasAmbiguous}}
		}
		if len(indexes) == 1 {
			if err := lifecycle.RequireManualAliasMutation(cfg, alias); err != nil {
				return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Err: err}
			}
			currentAlias := cfg.Aliases[indexes[0]]
			if !config.ProtocolsMatch(currentAlias.Protocol, providerProtocol) {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q protocol %q does not match provider %q protocol %q", alias, config.NormalizeAliasProtocol(currentAlias.Protocol), providerID, providerProtocol)
			}
		} else {
			cfg.UpsertAlias(config.Alias{Alias: alias, Protocol: providerProtocol, Enabled: true})
		}
		if err := cfg.AddTarget(alias, config.Target{Provider: providerID, Model: model, Enabled: !in.Disabled}); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		view = aliasView(cfg, *current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) UnbindAliasTarget(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if err := lifecycle.RequireManualAliasMutation(cfg, alias); err != nil {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Err: err}
		}
		if err := cfg.RemoveTarget(alias, providerID, model); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		view = aliasView(cfg, *current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) ReorderAliasTargets(ctx context.Context, in AliasTargetReorderInput) (AliasView, error) {
	alias := strings.TrimSpace(in.Alias)
	if alias == "" {
		return AliasView{}, fmt.Errorf("alias is required")
	}
	refs := make([]config.TargetRef, 0, len(in.Targets))
	for _, target := range in.Targets {
		providerID := strings.TrimSpace(target.Provider)
		model := strings.TrimSpace(target.Model)
		if providerID == "" || model == "" {
			return AliasView{}, fmt.Errorf("target provider and model are required")
		}
		refs = append(refs, config.TargetRef{Provider: providerID, Model: model})
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if err := lifecycle.RequireManualAliasMutation(cfg, alias); err != nil {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Err: err}
		}
		if err := cfg.ReorderTargets(alias, refs); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		view = aliasView(cfg, *current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func providerConnectionEqual(a, b config.Provider) bool {
	return config.ProviderBaseURLsEqual(a, b) &&
		config.NormalizeProviderBaseURLStrategy(a.BaseURLStrategy) == config.NormalizeProviderBaseURLStrategy(b.BaseURLStrategy) &&
		config.ProviderAPIKeysEqual(a, b) &&
		reflect.DeepEqual(normalizeProviderHeaders(a.Headers), normalizeProviderHeaders(b.Headers))
}

func firstProviderAPIKey(primary string, apiKeys []string) string {
	normalized := config.NormalizeProviderAPIKeys(primary, apiKeys)
	if len(normalized) == 0 {
		return ""
	}
	return normalized[0]
}

func providerAPIKeyRemainder(primary string, apiKeys []string) []string {
	normalized := config.NormalizeProviderAPIKeys(primary, apiKeys)
	if len(normalized) <= 1 {
		return nil
	}
	return append([]string(nil), normalized[1:]...)
}

func normalizeProviderHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeImportedProvider(existing *config.Provider, ip opencode.ImportableProvider) config.Provider {
	importedModels := config.NormalizeProviderModels(ip.Models)
	merged := config.Provider{
		ID:              ip.ID,
		Name:            ip.Name,
		Protocol:        config.NormalizeProviderProtocol(ip.Protocol),
		BaseURL:         config.NormalizeProviderBaseURL(ip.BaseURL),
		BaseURLs:        config.NormalizeProviderBaseURLs(ip.BaseURL, nil),
		BaseURLStrategy: config.ProviderBaseURLStrategyOrdered,
		APIKey:          ip.APIKey,
		APIKeys:         nil,
		Headers:         cloneHeaders(ip.Headers),
		Models:          importedModels,
		ModelsSource:    "imported",
	}
	if len(importedModels) == 0 {
		merged.ModelsSource = ""
	}
	if existing == nil {
		return merged
	}
	merged.Headers = cloneHeaders(existing.Headers)
	merged.Disabled = existing.Disabled
	if existing.AutoAliasEnabled != nil {
		v := *existing.AutoAliasEnabled
		merged.AutoAliasEnabled = &v
	}
	if merged.Name == "" {
		merged.Name = existing.Name
	}
	if existing.ModelsSource == "discovered" {
		prospective := merged
		prospective.Headers = cloneHeaders(existing.Headers)
		prospective.Disabled = existing.Disabled
		if providerConnectionEqual(*existing, prospective) {
			merged.Models = append([]string(nil), existing.Models...)
			merged.ModelsSource = existing.ModelsSource
			return merged
		}
		if len(importedModels) == 0 {
			merged.Models = append([]string(nil), existing.Models...)
			merged.ModelsSource = ""
			return merged
		}
	}
	if len(importedModels) == 0 {
		merged.Models = nil
		merged.ModelsSource = ""
	}
	return merged
}

func validateProviderModelKnown(providerID string, known []string, source string, model string) error {
	if source != "discovered" || len(known) == 0 {
		return nil
	}
	if slices.Contains(known, model) {
		return nil
	}
	choices := make([]string, 0, len(known))
	for _, item := range known {
		choices = append(choices, providerID+"/"+item)
	}
	sort.Strings(choices)
	return fmt.Errorf("model %q is not in provider %q discovered models; available: %s", model, providerID, strings.Join(choices, ", "))
}

func appendAutoAliasWarnings(cfg *config.Config, provider config.Provider) []string {
	if provider.ModelsSource != "discovered" || len(provider.Models) == 0 {
		return nil
	}
	// Both global and per-provider switches must be on.
	if !cfg.IsAutoAliasEnabled() || !provider.EffectiveAutoAliasEnabled() {
		return nil
	}
	created, updated := cfg.AutoGenerateAliases(provider)
	var warnings []string
	if len(created) > 0 {
		warnings = append(warnings, fmt.Sprintf("auto-generated %d alias(es): %s", len(created), strings.Join(created, ", ")))
	}
	if len(updated) > 0 {
		warnings = append(warnings, fmt.Sprintf("updated %d auto alias(es): %s", len(updated), strings.Join(updated, ", ")))
	}
	return warnings
}

func providerPriorityInputWarnings(cfg *config.Config, ids []string) []string {
	known := make(map[string]struct{}, len(cfg.Providers))
	for _, p := range cfg.Providers {
		known[p.ID] = struct{}{}
	}
	seen := make(map[string]struct{})
	var unknown, duplicates []string
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := known[id]; !ok {
			if _, already := seen["unknown:"+id]; !already {
				unknown = append(unknown, id)
				seen["unknown:"+id] = struct{}{}
			}
			continue
		}
		if _, ok := seen[id]; ok {
			duplicates = append(duplicates, id)
			continue
		}
		seen[id] = struct{}{}
	}
	var warnings []string
	if len(unknown) > 0 {
		warnings = append(warnings, fmt.Sprintf("ignored unknown provider id(s): %s", strings.Join(unknown, ", ")))
	}
	if len(duplicates) > 0 {
		warnings = append(warnings, fmt.Sprintf("ignored duplicate provider id(s): %s", strings.Join(duplicates, ", ")))
	}
	return warnings
}

func discoverProviderModels(provider *config.Provider, existing *config.Provider) []string {
	if provider == nil {
		return nil
	}
	models, probe, err := opencode.FetchProviderModelsWithAuthFallback(provider.Protocol, provider.EffectiveBaseURLs(), provider.EffectiveAPIKeys(), provider.Headers)
	if probe != nil && probe.Reachable && probe.BaseURL != "" {
		provider.BaseURL = probe.BaseURL
	}
	if err != nil {
		warnings := []string{}
		if existing != nil && !providerConnectionEqual(*existing, *provider) {
			provider.Models = append([]string(nil), existing.Models...)
			provider.ModelsSource = ""
			warnings = append(warnings, "provider connection changed and model discovery failed; keeping existing model catalog as untrusted")
		}
		warnings = append(warnings, fmt.Sprintf("could not discover provider models: %v", err))
		return warnings
	}
	if normalized := config.NormalizeProviderModels(models); len(normalized) > 0 {
		provider.Models = normalized
		provider.ModelsSource = "discovered"
		return nil
	}
	if existing != nil && !providerConnectionEqual(*existing, *provider) {
		provider.Models = append([]string(nil), existing.Models...)
		provider.ModelsSource = ""
		return []string{"provider connection changed and model discovery returned no models; keeping existing model catalog as untrusted"}
	}
	return []string{"provider model discovery returned no models; keeping existing model catalog"}
}
