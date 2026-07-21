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
	_ = ctx
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
	cfg, err := s.loadConfig()
	if err != nil {
		return RequestRewriteRuleView{}, err
	}
	cfg.UpsertRequestRewriteRule(rule)
	if errs := cfg.Validate(); len(errs) > 0 {
		return RequestRewriteRuleView{}, errs[0]
	}
	if err := cfg.Save(); err != nil {
		return RequestRewriteRuleView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return RequestRewriteRuleView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	current := cfg.FindRequestRewriteRule(rule.Name)
	if current == nil {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule %q not found", rule.Name)
	}
	return requestRewriteRuleView(*current), nil
}

func (s *Service) SetRequestRewriteRuleEnabled(ctx context.Context, in RequestRewriteRuleStateInput) (RequestRewriteRuleView, error) {
	_ = ctx
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule name is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return RequestRewriteRuleView{}, err
	}
	if !cfg.SetRequestRewriteRuleEnabled(name, in.Enabled) {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule %q not found", in.Name)
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return RequestRewriteRuleView{}, errs[0]
	}
	if err := cfg.Save(); err != nil {
		return RequestRewriteRuleView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return RequestRewriteRuleView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	current := cfg.FindRequestRewriteRule(name)
	if current == nil {
		return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule %q not found", name)
	}
	return requestRewriteRuleView(*current), nil
}

func (s *Service) RemoveRequestRewriteRule(ctx context.Context, in RequestRewriteRuleRemoveInput) (RequestRewriteRuleRemoveResult, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return RequestRewriteRuleRemoveResult{}, err
	}
	if !cfg.RemoveRequestRewriteRule(strings.TrimSpace(in.Name)) {
		return RequestRewriteRuleRemoveResult{}, fmt.Errorf("request rewrite rule %q not found", in.Name)
	}
	if err := cfg.Save(); err != nil {
		return RequestRewriteRuleRemoveResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return RequestRewriteRuleRemoveResult{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return RequestRewriteRuleRemoveResult{OK: true}, nil
}

func (s *Service) ReorderRequestRewriteRules(ctx context.Context, in RequestRewriteRuleReorderInput) (RequestRewriteRuleReorderResult, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return RequestRewriteRuleReorderResult{}, err
	}
	if err := cfg.ReorderRequestRewriteRules(in.Names); err != nil {
		return RequestRewriteRuleReorderResult{}, err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return RequestRewriteRuleReorderResult{}, errs[0]
	}
	if err := cfg.Save(); err != nil {
		return RequestRewriteRuleReorderResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return RequestRewriteRuleReorderResult{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return RequestRewriteRuleReorderResult{Rules: requestRewriteRuleViews(cfg.RequestRewriteRulesSnapshot())}, nil
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
	cfg.UpsertProvider(provider)
	warnings = append(warnings, appendAutoAliasWarnings(cfg, provider)...)
	if err := cfg.Save(); err != nil {
		return ProviderSaveResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return ProviderSaveResult{Provider: providerView(provider), Warnings: warnings}, nil
}

func (s *Service) RefreshProviderModels(ctx context.Context, in ProviderRefreshModelsInput) (ProviderSaveResult, error) {
	_ = ctx
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
	cfg.UpsertProvider(provider)
	warnings = append(warnings, appendAutoAliasWarnings(cfg, provider)...)
	if err := cfg.Save(); err != nil {
		return ProviderSaveResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("reload proxy config: %w", err)
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
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if !cfg.RemoveProvider(id) {
		return fmt.Errorf("provider %q not found", id)
	}
	_ = cfg.RemoveProviderAutoTargets(id)
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return fmt.Errorf("reload proxy config: %w", err)
	}
	return nil
}

func (s *Service) SetProviderPriority(ctx context.Context, in ProviderPriorityInput) (ProviderPriorityResult, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderPriorityResult{}, err
	}
	warnings := providerPriorityInputWarnings(cfg, in.OrderedIDs)
	cfg.SetProviderPriority(in.OrderedIDs)
	if err := cfg.Save(); err != nil {
		return ProviderPriorityResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return ProviderPriorityResult{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return ProviderPriorityResult{
		OrderedIDs: cfg.ProviderPriorityOrder(),
		Warnings:   warnings,
	}, nil
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
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return AutoAliasSettingsResult{}, err
	}
	cfg.SetAutoAliasEnabled(in.Enabled)
	if err := cfg.Save(); err != nil {
		return AutoAliasSettingsResult{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AutoAliasSettingsResult{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return AutoAliasSettingsResult{Enabled: cfg.IsAutoAliasEnabled()}, nil
}

func (s *Service) UpgradeAutoAlias(ctx context.Context, in AliasLockInput) (AliasView, error) {
	_ = ctx
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return AliasView{}, fmt.Errorf("alias name is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	locked, err := cfg.LockAutoAlias(name)
	if err != nil {
		return AliasView{}, err
	}
	locked.AutoGenerated = false
	locked.Locked = false
	for i := range locked.Targets {
		locked.Targets[i].AutoGenerated = false
	}
	cfg.UpsertAlias(locked)
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return aliasView(cfg, locked), nil
}

func (s *Service) SetAliasTargetDisabled(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	_ = ctx
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	current := cfg.FindAlias(alias)
	if current == nil {
		return AliasView{}, fmt.Errorf("alias %q not found", alias)
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
		return AliasView{}, fmt.Errorf("target %s/%s not found on alias %s", providerID, model, alias)
	}
	cfg.UpsertAlias(updated)
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return aliasView(cfg, updated), nil
}

func (s *Service) SetProviderDisabled(ctx context.Context, in ProviderStateInput) (ProviderView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderView{}, err
	}
	existing := cfg.FindProvider(strings.TrimSpace(in.ID))
	if existing == nil {
		return ProviderView{}, fmt.Errorf("provider %q not found", in.ID)
	}
	updated := *existing
	updated.Disabled = in.Disabled
	cfg.UpsertProvider(updated)
	if err := cfg.Save(); err != nil {
		return ProviderView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return ProviderView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return providerView(updated), nil
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
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderImportResult{}, err
	}
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
	}
	if result.Imported > 0 {
		if err := cfg.Save(); err != nil {
			return ProviderImportResult{}, err
		}
		if err := s.reloadRunningProxyConfig(cfg); err != nil {
			return ProviderImportResult{}, fmt.Errorf("reload proxy config: %w", err)
		}
	}
	return result, nil
}

func (s *Service) UpsertAlias(ctx context.Context, in AliasUpsertInput) (AliasView, error) {
	_ = ctx
	name := strings.TrimSpace(in.Alias)
	if name == "" {
		return AliasView{}, fmt.Errorf("alias name is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	a := config.Alias{Alias: name, DisplayName: strings.TrimSpace(in.DisplayName), Protocol: config.NormalizeAliasProtocol(strings.TrimSpace(in.Protocol)), Enabled: !in.Disabled}
	existing := cfg.FindAlias(name)
	if existing == nil {
		if auto := cfg.FindAutoAlias(name); auto != nil {
			if !auto.Locked {
				return AliasView{}, fmt.Errorf("auto-generated alias %q must be upgraded to manual before editing", name)
			}
			existing = auto
		}
	}
	if existing != nil {
		if a.DisplayName == "" {
			a.DisplayName = existing.DisplayName
		}
		if strings.TrimSpace(in.Protocol) == "" {
			a.Protocol = existing.Protocol
		}
		a.Targets = existing.Targets
	}
	cfg.UpsertAlias(a)
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	return aliasView(cfg, a), nil
}

func (s *Service) RemoveAlias(ctx context.Context, name string) error {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	if !cfg.RemoveAlias(strings.TrimSpace(name)) {
		return fmt.Errorf("alias %q not found", name)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return fmt.Errorf("reload proxy config: %w", err)
	}
	return nil
}

func (s *Service) BindAliasTarget(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	_ = ctx
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	p := cfg.FindProvider(providerID)
	if p == nil {
		return AliasView{}, fmt.Errorf("provider %q does not exist; add it first", providerID)
	}
	providerProtocol := config.NormalizeProviderProtocol(p.Protocol)
	if err := validateProviderModelKnown(providerID, p.Models, p.ModelsSource, model); err != nil {
		return AliasView{}, err
	}
	currentAlias := cfg.FindAlias(alias)
	if currentAlias == nil {
		cfg.UpsertAlias(config.Alias{Alias: alias, Protocol: providerProtocol, Enabled: true})
	} else if !config.ProtocolsMatch(currentAlias.Protocol, providerProtocol) {
		return AliasView{}, fmt.Errorf("alias %q protocol %q does not match provider %q protocol %q", alias, config.NormalizeAliasProtocol(currentAlias.Protocol), providerID, providerProtocol)
	}
	if err := cfg.AddTarget(alias, config.Target{Provider: providerID, Model: model, Enabled: !in.Disabled}); err != nil {
		return AliasView{}, err
	}
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	current := cfg.FindAlias(alias)
	if current == nil {
		return AliasView{}, fmt.Errorf("alias %q not found", alias)
	}
	return aliasView(cfg, *current), nil
}

func (s *Service) UnbindAliasTarget(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	_ = ctx
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider and model are required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	if err := cfg.RemoveTarget(alias, providerID, model); err != nil {
		return AliasView{}, err
	}
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	current := cfg.FindAlias(alias)
	if current == nil {
		return AliasView{}, fmt.Errorf("alias %q not found", alias)
	}
	return aliasView(cfg, *current), nil
}

func (s *Service) ReorderAliasTargets(ctx context.Context, in AliasTargetReorderInput) (AliasView, error) {
	_ = ctx
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
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	if err := cfg.ReorderTargets(alias, refs); err != nil {
		return AliasView{}, err
	}
	if err := cfg.Save(); err != nil {
		return AliasView{}, err
	}
	if err := s.reloadRunningProxyConfig(cfg); err != nil {
		return AliasView{}, fmt.Errorf("reload proxy config: %w", err)
	}
	current := cfg.FindAlias(alias)
	if current == nil {
		return AliasView{}, fmt.Errorf("alias %q not found", alias)
	}
	return aliasView(cfg, *current), nil
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
