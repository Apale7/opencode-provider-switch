package app

import (
	"context"
	"errors"
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
		Name:     strings.TrimSpace(in.Name),
		Alias:    strings.TrimSpace(in.Alias),
		Enabled:  in.Enabled,
		Override: in.Override,
		Ops:      cloneRequestRewriteOperations(in.Ops),
	}
	if in.ProviderGroups != nil {
		// Explicit ProviderGroups path (including empty wildcard).
		for _, selector := range in.ProviderGroups {
			if strings.TrimSpace(selector.Provider) == "" || strings.TrimSpace(selector.Group) == "" {
				return RequestRewriteRuleView{}, fmt.Errorf("request rewrite rule providerGroups require provider and group")
			}
		}
		rule.ProviderGroups = providerGroupSelectorsFromInput(in.ProviderGroups, nil)
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
	providerID := strings.TrimSpace(in.ID)
	if providerID == "" {
		return ProviderSaveResult{}, fmt.Errorf("provider id is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderSaveResult{}, err
	}
	existing := cfg.FindProvider(providerID)
	protocol := protocolForProviderUpsert(existing, in.DefaultGroup)
	if err := config.ValidateProviderBaseURLs(protocol, in.BaseURL, in.BaseURLs); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("invalid baseUrl: %w", err)
	}
	if err := config.ValidateProviderBaseURLStrategy(in.BaseURLStrategy); err != nil {
		return ProviderSaveResult{}, fmt.Errorf("invalid baseUrlStrategy: %w", err)
	}
	baseURLs := config.NormalizeProviderBaseURLs(in.BaseURL, in.BaseURLs)
	if len(baseURLs) == 0 {
		return ProviderSaveResult{}, fmt.Errorf("invalid baseUrl: missing base_url")
	}

	var groupInput *ProviderGroupInput
	if in.DefaultGroup != nil {
		normalized, normErr := normalizeDefaultGroupInput(*in.DefaultGroup, existing)
		if normErr != nil {
			return ProviderSaveResult{}, normErr
		}
		groupInput = &normalized
	}

	warnings := []string{}
	var discovery *providerGroupDiscoveryOutcome
	isCreate := existing == nil
	wasUpdate := existing != nil

	if isCreate {
		// Create requires nested default group; no top-level protocol/key fields.
		if groupInput == nil {
			return ProviderSaveResult{}, fmt.Errorf("defaultGroup is required when creating a provider")
		}
		// Fail-fast build (protocol/key validation) before network discovery.
		if _, buildErr := buildProviderGroupFromInput(*groupInput, nil); buildErr != nil {
			return ProviderSaveResult{}, buildErr
		}
		// Models == nil means discover; non-nil (including empty) stores as-is.
		if groupInput.Models == nil {
			created := buildProviderShell(providerID, in, baseURLs)
			group, buildErr := buildProviderGroupFromInput(*groupInput, nil)
			if buildErr != nil {
				return ProviderSaveResult{}, buildErr
			}
			created.Groups = []config.ProviderGroup{group}
			applyProviderAutoAliasOnCreate(&created, in.AutoAliasEnabled)
			var discErr error
			discovery, discErr = runProviderGroupDiscovery(ctx, &created, config.DefaultGroupID, nil)
			if discErr != nil {
				return ProviderSaveResult{}, discErr
			}
			warnings = append(warnings, discovery.Warnings...)
		}
	} else if groupInput != nil {
		// Fail-fast validate default-group apply against the pre-load snapshot.
		prospective := buildProviderShell(providerID, in, baseURLs)
		applyProviderSharedFieldsFromLive(&prospective, *existing, in, baseURLs)
		prospective.Groups = cloneProviderGroupsForEdit(existing.Groups)
		if err := applyDefaultGroupUpdate(&prospective, *groupInput); err != nil {
			return ProviderSaveResult{}, err
		}
		sharedChanged := !providerSharedConnectionEqual(*existing, prospective)
		// Models == nil means discover; non-nil (including empty) skips.
		if groupInput.Models == nil {
			if sharedChanged {
				// Shared connection change invalidates every group catalog;
				// discovery may re-trust only the default group on success.
				markAllGroupModelsUntrusted(&prospective)
			}
			var discErr error
			discovery, discErr = runProviderGroupDiscovery(ctx, &prospective, config.DefaultGroupID, existing)
			if discErr != nil {
				return ProviderSaveResult{}, discErr
			}
			warnings = append(warnings, discovery.Warnings...)
		} else if sharedChanged {
			warnings = append(warnings, "provider connection changed with skip models enabled; keeping existing model catalog as untrusted")
		}
	} else {
		// Shared-only edit never mutates groups; mark catalogs untrusted when connection changes.
		prospective := buildProviderShell(providerID, in, baseURLs)
		applyProviderSharedFieldsFromLive(&prospective, *existing, in, baseURLs)
		if !providerSharedConnectionEqual(*existing, prospective) {
			warnings = append(warnings, "provider connection changed with skip models enabled; keeping existing model catalog as untrusted")
		}
	}

	var saved config.Provider
	_, err = s.commitConfig(ctx, "", func(_ context.Context, live *config.Config) (configstore.Mutation[*config.Config], error) {
		liveExisting := live.FindProvider(providerID)
		if liveExisting == nil {
			if wasUpdate {
				return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "revision_conflict", Params: map[string]any{"providerId": providerID}}
			}
			// Create path (or concurrent delete of a provider we intended to update).
			if groupInput == nil {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("defaultGroup is required when creating a provider")
			}
			created := buildProviderShell(providerID, in, baseURLs)
			applyProviderSharedFieldsOnCreate(&created, in, baseURLs)
			group, buildErr := buildProviderGroupFromInput(*groupInput, nil)
			if buildErr != nil {
				return configstore.Mutation[*config.Config]{}, buildErr
			}
			created.Groups = []config.ProviderGroup{group}
			applyProviderAutoAliasOnCreate(&created, in.AutoAliasEnabled)
			if discovery != nil {
				// Create discovery used the same intended shared+default auth; apply catalog
				// only on explicit success (never replay pre-discovery empty/fail snapshots).
				if discovery.PrimaryBaseURL != "" {
					created.BaseURL = discovery.PrimaryBaseURL
				}
				if discovery.Succeeded {
					applyDiscoveredModelsToGroup(&created, config.DefaultGroupID, discovery.Models, discovery.ModelsSource)
				}
			}
			live.UpsertProvider(created)
			if current := live.FindProvider(providerID); current != nil {
				saved = *current
				warnings = append(warnings, appendAutoAliasWarnings(live, saved)...)
			} else {
				saved = created
			}
			return configstore.Mutation[*config.Config]{Value: live, Changed: true}, nil
		}

		// Update: field-level merge onto the latest provider — never wholesale-replace
		// groups from a pre-network or pre-mutation snapshot (would resurrect siblings).
		before := *liveExisting
		updated := *liveExisting
		applyProviderSharedFieldsFromLive(&updated, before, in, baseURLs)
		// Always start groups from live so concurrent key/rename/delete survive.
		updated.Groups = cloneProviderGroupsForEdit(liveExisting.Groups)
		sharedChanged := !providerSharedConnectionEqual(before, updated)

		if groupInput != nil {
			if existing.FindGroup(config.DefaultGroupID) != nil && liveExisting.FindGroup(config.DefaultGroupID) == nil {
				return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "revision_conflict", Params: map[string]any{"providerId": providerID, "groupId": config.DefaultGroupID}}
			}
			// Atomic shared + default-group update in one ConfigStore mutation.
			if err := applyDefaultGroupUpdate(&updated, *groupInput); err != nil {
				return configstore.Mutation[*config.Config]{}, err
			}
			if sharedChanged {
				markAllGroupModelsUntrusted(&updated)
			}
			if discovery != nil {
				if discovery.Fingerprint.MatchesProviderGroup(updated, config.DefaultGroupID) &&
					(!discovery.Succeeded || discovery.Fingerprint.MatchesProviderGroupCatalog(updated, config.DefaultGroupID)) {
					if discovery.PrimaryBaseURL != "" {
						// Probe may promote a reachable URL among the same EffectiveBaseURLs.
						updated.BaseURL = discovery.PrimaryBaseURL
					}
					// Success only: failure/empty must not overwrite live concurrent catalogs
					// with the pre-discovery snapshot. Connection untrusted is already applied.
					if discovery.Succeeded {
						applyDiscoveredModelsToGroup(&updated, config.DefaultGroupID, discovery.Models, discovery.ModelsSource)
					}
				} else {
					// Concurrent auth/connection drift vs discovery-time fingerprint.
					// Keep default-group catalog from apply (Models nil preserves live);
					// do not write stale discovery or resurrect sibling state.
					warnings = append(warnings, "discovery result discarded due to concurrent provider or group change")
				}
			} else if sharedChanged {
				// Skip-models path: catalogs already marked untrusted above.
			}
		} else if sharedChanged {
			// Shared-only edit: groups content stays live; catalogs become untrusted.
			markAllGroupModelsUntrusted(&updated)
		}

		live.UpsertProvider(updated)
		if current := live.FindProvider(providerID); current != nil {
			saved = *current
			warnings = append(warnings, appendAutoAliasWarnings(live, saved)...)
		} else {
			saved = updated
		}
		return configstore.Mutation[*config.Config]{Value: live, Changed: true}, nil
	})
	if err != nil {
		return ProviderSaveResult{}, err
	}
	return ProviderSaveResult{Provider: providerView(saved), Warnings: warnings}, nil
}

// protocolForProviderUpsert picks a protocol solely for Base URL validation.
// Nested DefaultGroup protocol wins when present (create and atomic update);
// otherwise update falls back to the existing default group.
func protocolForProviderUpsert(existing *config.Provider, defaultGroup *ProviderGroupInput) string {
	if defaultGroup != nil && strings.TrimSpace(defaultGroup.Protocol) != "" {
		return config.NormalizeProviderProtocol(defaultGroup.Protocol)
	}
	if existing != nil {
		if g := existing.FindGroup(config.DefaultGroupID); g != nil {
			return config.NormalizeProviderProtocol(g.Protocol)
		}
		if len(existing.Groups) > 0 {
			return config.NormalizeProviderProtocol(existing.Groups[0].Protocol)
		}
		return config.DefaultProviderProtocol()
	}
	return config.DefaultProviderProtocol()
}

func (s *Service) RefreshProviderModels(ctx context.Context, in ProviderRefreshModelsInput) (ProviderSaveResult, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return ProviderSaveResult{}, fmt.Errorf("provider id is required")
	}
	groupID := resolveProviderGroupID(in.Group)
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderSaveResult{}, err
	}
	existing := cfg.FindProvider(id)
	if existing == nil {
		return ProviderSaveResult{}, fmt.Errorf("provider %q not found", id)
	}
	if existing.FindGroup(groupID) == nil {
		// Exact group only — never fall back to first or same-protocol sibling.
		return ProviderSaveResult{}, fmt.Errorf("provider %q group %q not found", id, groupID)
	}

	// Discover outside the mutation (network I/O). Capture fingerprint of the
	// auth/connection used so the commit can refuse stale results.
	work := *existing
	work.Groups = cloneProviderGroupsForEdit(existing.Groups)
	if existing.Headers != nil {
		work.Headers = cloneHeaders(existing.Headers)
	}
	if existing.AutoAliasEnabled != nil {
		v := *existing.AutoAliasEnabled
		work.AutoAliasEnabled = &v
	}
	discovery, discErr := runProviderGroupDiscovery(ctx, &work, groupID, existing)
	if discErr != nil {
		return ProviderSaveResult{}, discErr
	}
	warnings := append([]string(nil), discovery.Warnings...)

	var saved config.Provider
	_, err = s.commitConfig(ctx, "", func(_ context.Context, live *config.Config) (configstore.Mutation[*config.Config], error) {
		liveProvider := live.FindProvider(id)
		if liveProvider == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q not found", id)
		}
		if liveProvider.FindGroup(groupID) == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found", id, groupID)
		}
		// Refuse stale discovery when shared connection or target-group auth drifted.
		// Do not apply discovery.Models — concurrent key/protocol change must keep the
		// live catalog (e.g. keep-me) and surface a stable revision_conflict.
		if !discovery.Fingerprint.MatchesProviderGroup(*liveProvider, groupID) ||
			(discovery.Succeeded && !discovery.Fingerprint.MatchesProviderGroupCatalog(*liveProvider, groupID)) {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{
				Code: "revision_conflict",
				Params: map[string]any{
					"providerId": id,
					"groupId":    groupID,
					"reason":     "provider connection or group auth changed during model discovery",
				},
				Err: fmt.Errorf("provider %q group %q connection or auth changed during model discovery", id, groupID),
			}
		}

		// Precise merge: only the target group catalog (+ optional primary BaseURL
		// promotion among the same EffectiveBaseURLs). Never replace sibling groups.
		// BaseURL promotion may reorder EffectiveBaseURLs; that is intentional and
		// must not re-run the full fingerprint check after assignment.
		// Failure/empty discovery must not replay pre-discovery catalog over live
		// concurrent model updates — keep commit-time live catalog.
		updated := *liveProvider
		updated.Groups = cloneProviderGroupsForEdit(liveProvider.Groups)
		if discovery.PrimaryBaseURL != "" {
			updated.BaseURL = discovery.PrimaryBaseURL
		}
		if discovery.Succeeded {
			applyDiscoveredModelsToGroup(&updated, groupID, discovery.Models, discovery.ModelsSource)
		}
		live.UpsertProvider(updated)
		if current := live.FindProvider(id); current != nil {
			saved = *current
			warnings = append(warnings, appendAutoAliasWarnings(live, saved)...)
		} else {
			saved = updated
		}
		return configstore.Mutation[*config.Config]{Value: live, Changed: true}, nil
	})
	if err != nil {
		return ProviderSaveResult{}, err
	}
	return ProviderSaveResult{Provider: providerView(saved), Warnings: warnings}, nil
}

func (s *Service) PingProviderBaseURL(ctx context.Context, in ProviderPingInput) (ProviderPingResult, error) {
	id := strings.TrimSpace(in.ID)
	groupID := resolveProviderGroupID(in.Group)
	baseURL := config.NormalizeProviderBaseURL(in.BaseURL)
	// Keep raw protocol separate from Normalize: empty means "use group default"
	// for existing providers, not "force DefaultProviderProtocol".
	rawProtocol := strings.TrimSpace(in.Protocol)
	protocol := config.NormalizeProviderProtocol(rawProtocol)
	if id == "" && rawProtocol == "" {
		return ProviderPingResult{}, fmt.Errorf("provider id or protocol is required")
	}
	if baseURL == "" {
		return ProviderPingResult{}, fmt.Errorf("baseUrl is required")
	}
	// Draft/new-provider path: protocol and keys come from the request only.
	probeInput := opencode.ProviderGroupModelsInput{
		ProviderID: id,
		GroupID:    groupID,
		Protocol:   protocol,
		BaseURLs:   []string{baseURL},
		APIKeys:    config.NormalizeProviderAPIKeys(in.APIKey, in.APIKeys),
		Headers:    normalizeProviderHeaders(in.Headers),
	}
	if id != "" {
		cfg, err := s.loadConfig()
		if err != nil {
			return ProviderPingResult{}, err
		}
		existing := cfg.FindProvider(id)
		if existing != nil {
			group := existing.FindGroup(groupID)
			if group == nil {
				return ProviderPingResult{}, fmt.Errorf("provider %q group %q not found", id, groupID)
			}
			// Shared BaseURLs/headers from Provider; protocol/keys from the exact Group.
			// Empty request protocol must NOT override the group's protocol.
			probeInput.Protocol = group.Protocol
			probeInput.APIKeys = group.EffectiveAPIKeys()
			probeInput.Headers = normalizeProviderHeaders(existing.Headers)
			if rawProtocol != "" {
				probeInput.Protocol = protocol
			}
			if len(config.NormalizeProviderAPIKeys(in.APIKey, in.APIKeys)) > 0 {
				probeInput.APIKeys = config.NormalizeProviderAPIKeys(in.APIKey, in.APIKeys)
			}
			if len(in.Headers) > 0 {
				probeInput.Headers = normalizeProviderHeaders(in.Headers)
			}
		} else if rawProtocol == "" {
			return ProviderPingResult{}, fmt.Errorf("provider %q not found", id)
		}
	}
	if strings.TrimSpace(probeInput.Protocol) == "" {
		return ProviderPingResult{}, fmt.Errorf("provider protocol is required")
	}
	startedAt := time.Now()
	probe, err := opencode.ProbeProviderGroupBaseURL(ctx, probeInput, baseURL)
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

// ProviderGroupCreateInput creates one group under an existing provider.
type ProviderGroupCreateInput struct {
	ProviderID string             `json:"providerId"`
	Group      ProviderGroupInput `json:"group"`
}

// ProviderGroupUpdateInput updates one existing group.
// GroupID is the path identity; Group.ID may differ to request a stable ID change.
type ProviderGroupUpdateInput struct {
	ProviderID       string                `json:"providerId"`
	GroupID          string                `json:"groupId"`
	Group            ProviderGroupInput    `json:"group"`
	Selections       []lifecycle.Selection `json:"selections,omitempty"`
	ExpectedRevision ConfigRevision        `json:"expectedRevision,omitempty"`
}

// ProviderGroupDeleteInput deletes one group with optional lifecycle selections.
type ProviderGroupDeleteInput struct {
	ProviderID       string                `json:"providerId"`
	GroupID          string                `json:"groupId"`
	Selections       []lifecycle.Selection `json:"selections,omitempty"`
	ExpectedRevision ConfigRevision        `json:"expectedRevision,omitempty"`
}

// ProviderGroupRefreshModelsInput refreshes models for one exact group.
type ProviderGroupRefreshModelsInput struct {
	ProviderID string `json:"providerId"`
	GroupID    string `json:"groupId"`
}

// ProviderGroupPingInput pings Base URL using one exact group's protocol and keys.
type ProviderGroupPingInput struct {
	ProviderID string            `json:"providerId"`
	GroupID    string            `json:"groupId"`
	BaseURL    string            `json:"baseUrl,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	APIKey     string            `json:"apiKey,omitempty"`
	APIKeys    []string          `json:"apiKeys,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

func (s *Service) ListProviderGroups(ctx context.Context, providerID string) ([]ProviderGroupView, error) {
	_ = ctx
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	provider := cfg.FindProvider(providerID)
	if provider == nil {
		return nil, fmt.Errorf("provider %q not found", providerID)
	}
	return providerGroupViews(provider.Groups), nil
}

func (s *Service) CreateProviderGroup(ctx context.Context, in ProviderGroupCreateInput) (ProviderGroupView, error) {
	providerID := strings.TrimSpace(in.ProviderID)
	if providerID == "" {
		return ProviderGroupView{}, fmt.Errorf("provider id is required")
	}
	group, err := buildProviderGroupFromInput(in.Group, nil)
	if err != nil {
		return ProviderGroupView{}, err
	}
	var view ProviderGroupView
	_, err = s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		provider := cfg.FindProvider(providerID)
		if provider == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q not found", providerID)
		}
		if strings.TrimSpace(group.ID) == "" {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("group id is required")
		}
		if provider.FindGroup(group.ID) != nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q already has group %q", providerID, group.ID)
		}
		updated := *provider
		// Append only — never rewrite sibling groups.
		updated.Groups = append(cloneProviderGroupsForEdit(provider.Groups), group)
		cfg.UpsertProvider(updated)
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		current := cfg.FindProvider(providerID)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q not found", providerID)
		}
		created := current.FindGroup(group.ID)
		if created == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found after create", providerID, group.ID)
		}
		view = providerGroupView(*created)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) UpdateProviderGroup(ctx context.Context, in ProviderGroupUpdateInput) (ProviderGroupView, error) {
	providerID := strings.TrimSpace(in.ProviderID)
	pathGroupID := strings.TrimSpace(in.GroupID)
	if providerID == "" {
		return ProviderGroupView{}, fmt.Errorf("provider id is required")
	}
	if pathGroupID == "" {
		return ProviderGroupView{}, fmt.Errorf("group id is required")
	}
	desiredID := strings.TrimSpace(in.Group.ID)
	if desiredID == "" {
		desiredID = pathGroupID
	}
	if err := validateProviderGroupAPIKeysInput(in.Group); err != nil {
		return ProviderGroupView{}, err
	}
	protocol := config.NormalizeProviderProtocol(strings.TrimSpace(in.Group.Protocol))
	if err := config.ValidateProtocol(protocol); err != nil {
		return ProviderGroupView{}, fmt.Errorf("invalid group protocol: %w", err)
	}

	// Stable ID change uses the same explicit lifecycle contract as delete — never silent rebind.
	if desiredID != pathGroupID {
		rev, cfg, err := s.SnapshotConfigRevision(ctx)
		if err != nil {
			return ProviderGroupView{}, err
		}
		if in.ExpectedRevision != "" && rev != in.ExpectedRevision {
			return ProviderGroupView{}, &OutcomeError{
				Code: "revision_conflict",
				Params: map[string]any{
					"expected": string(in.ExpectedRevision),
					"current":  string(rev),
				},
			}
		}
		planned, err := lifecycle.PlanGroupIDChange(cfg, string(rev), providerID, pathGroupID, desiredID, in.Selections)
		if err != nil {
			return ProviderGroupView{}, err
		}
		if !planned.Plan.Executable {
			return ProviderGroupView{}, &OutcomeError{
				Code: "plan_not_executable",
				Params: map[string]any{
					"operationKind": lifecycle.OpGroupIDChange,
					"providerId":    providerID,
					"groupId":       pathGroupID,
					"newGroupId":    desiredID,
					"blockerCount":  len(planned.Plan.Blockers),
					"choiceCount":   len(planned.Plan.Choices),
				},
			}
		}
		candidate := planned.Candidate
		if candidate == nil {
			candidate = cfg
		}
		// Apply non-identity fields onto the renamed group only.
		provider := candidate.FindProvider(providerID)
		if provider == nil {
			return ProviderGroupView{}, fmt.Errorf("provider %q not found", providerID)
		}
		group := provider.FindGroup(desiredID)
		if group == nil {
			return ProviderGroupView{}, fmt.Errorf("provider %q group %q not found", providerID, desiredID)
		}
		before := *group
		if err := applyProviderGroupInput(group, in.Group, &before); err != nil {
			return ProviderGroupView{}, err
		}
		if !providerGroupAuthEqual(before, *group) {
			group.ModelsSource = ""
		}
		updated := *provider
		candidate.UpsertProvider(updated)
		if errs := candidate.ValidateForPersist(); len(errs) > 0 {
			return ProviderGroupView{}, errs[0]
		}
		if !planned.Plan.NoOp || groupFieldsChanged(before, *group) {
			if _, err := s.commitConfigReplace(ctx, rev, candidate); err != nil {
				return ProviderGroupView{}, err
			}
		}
		final := candidate.FindProvider(providerID)
		if final == nil {
			return ProviderGroupView{}, fmt.Errorf("provider %q not found", providerID)
		}
		g := final.FindGroup(desiredID)
		if g == nil {
			return ProviderGroupView{}, fmt.Errorf("provider %q group %q not found", providerID, desiredID)
		}
		return providerGroupView(*g), nil
	}

	var view ProviderGroupView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		provider := cfg.FindProvider(providerID)
		if provider == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q not found", providerID)
		}
		existing := provider.FindGroup(pathGroupID)
		if existing == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found", providerID, pathGroupID)
		}
		// Clone all groups and mutate only the target index so siblings stay byte-stable.
		updated := *provider
		updated.Groups = cloneProviderGroupsForEdit(provider.Groups)
		idx := -1
		for i := range updated.Groups {
			if updated.Groups[i].ID == pathGroupID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found", providerID, pathGroupID)
		}
		before := updated.Groups[idx]
		if err := applyProviderGroupInput(&updated.Groups[idx], in.Group, &before); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		if !providerGroupAuthEqual(before, updated.Groups[idx]) {
			updated.Groups[idx].ModelsSource = ""
		}
		cfg.UpsertProvider(updated)
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		current := cfg.FindProvider(providerID)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q not found", providerID)
		}
		group := current.FindGroup(pathGroupID)
		if group == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found", providerID, pathGroupID)
		}
		view = providerGroupView(*group)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func (s *Service) DeleteProviderGroup(ctx context.Context, in ProviderGroupDeleteInput) error {
	providerID := strings.TrimSpace(in.ProviderID)
	groupID := strings.TrimSpace(in.GroupID)
	if providerID == "" {
		return fmt.Errorf("provider id is required")
	}
	if groupID == "" {
		return fmt.Errorf("group id is required")
	}
	rev, cfg, err := s.SnapshotConfigRevision(ctx)
	if err != nil {
		return err
	}
	if len(in.Selections) > 0 {
		if strings.TrimSpace(string(in.ExpectedRevision)) == "" {
			return &OutcomeError{Code: "revision_required"}
		}
		if rev != in.ExpectedRevision {
			return &OutcomeError{Code: "revision_conflict", Params: map[string]any{"expected": string(in.ExpectedRevision), "current": string(rev)}}
		}
	}
	planned, err := lifecycle.PlanGroupRemove(cfg, string(rev), providerID, groupID, in.Selections)
	if err != nil {
		return err
	}
	if !planned.Plan.Executable {
		return &OutcomeError{
			Code: "plan_not_executable",
			Params: map[string]any{
				"operationKind": lifecycle.OpGroupRemove,
				"providerId":    providerID,
				"groupId":       groupID,
				"blockerCount":  len(planned.Plan.Blockers),
				"choiceCount":   len(planned.Plan.Choices),
			},
		}
	}
	if planned.Plan.NoOp || planned.Candidate == nil {
		if p := cfg.FindProvider(providerID); p == nil || p.FindGroup(groupID) == nil {
			return fmt.Errorf("provider %q group %q not found", providerID, groupID)
		}
		return nil
	}
	_, err = s.commitConfigReplace(ctx, rev, planned.Candidate)
	return err
}

func (s *Service) RefreshProviderGroupModels(ctx context.Context, in ProviderGroupRefreshModelsInput) (ProviderSaveResult, error) {
	return s.RefreshProviderModels(ctx, ProviderRefreshModelsInput{
		ID:    strings.TrimSpace(in.ProviderID),
		Group: strings.TrimSpace(in.GroupID),
	})
}

func (s *Service) PingProviderGroupBaseURL(ctx context.Context, in ProviderGroupPingInput) (ProviderPingResult, error) {
	providerID := strings.TrimSpace(in.ProviderID)
	groupID := strings.TrimSpace(in.GroupID)
	if providerID == "" {
		return ProviderPingResult{}, fmt.Errorf("provider id is required")
	}
	if groupID == "" {
		return ProviderPingResult{}, fmt.Errorf("group id is required")
	}
	// Exact group only — empty group is rejected (unlike legacy PingProviderBaseURL default mapping).
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderPingResult{}, err
	}
	provider := cfg.FindProvider(providerID)
	if provider == nil {
		return ProviderPingResult{}, fmt.Errorf("provider %q not found", providerID)
	}
	if provider.FindGroup(groupID) == nil {
		return ProviderPingResult{}, fmt.Errorf("provider %q group %q not found", providerID, groupID)
	}
	baseURL := config.NormalizeProviderBaseURL(in.BaseURL)
	if baseURL == "" {
		baseURL = provider.BaseURL
	}
	return s.PingProviderBaseURL(ctx, ProviderPingInput{
		ID:       providerID,
		Group:    groupID,
		Protocol: in.Protocol,
		BaseURL:  baseURL,
		APIKey:   in.APIKey,
		APIKeys:  in.APIKeys,
		Headers:  in.Headers,
	})
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
	cfg, err := s.loadConfig()
	if err != nil {
		return AliasView{}, err
	}
	alias := cfg.FindAlias(name)
	if alias == nil {
		return AliasView{}, fmt.Errorf("alias %q not found", name)
	}
	return aliasView(cfg, *alias), nil
}

func (s *Service) SetAliasTargetDisabled(ctx context.Context, in AliasTargetInput) (AliasView, error) {
	alias := strings.TrimSpace(in.Alias)
	providerID := strings.TrimSpace(in.Provider)
	groupID := strings.TrimSpace(in.Group)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || groupID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider, group and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		updated := *current
		found := false
		for i := range updated.Targets {
			targetGroup := strings.TrimSpace(updated.Targets[i].Group)
			if updated.Targets[i].Provider == providerID && targetGroup == groupID && updated.Targets[i].Model == model {
				updated.Targets[i].Enabled = !in.Disabled
				updated.Targets[i].Group = groupID
				found = true
				break
			}
		}
		if !found {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("target %s/%s/%s not found on alias %s", providerID, groupID, model, alias)
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
			if isMaskedAPIKeyPlaceholder(ip.APIKey) {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q api key must not contain a masked placeholder", ip.ID)
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
			if saved := cfg.FindProvider(merged.ID); saved != nil {
				result.Providers = append(result.Providers, providerView(*saved))
			}
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
		cfg.NormalizeCatalogAliasOwnership()
		displayName := strings.TrimSpace(in.DisplayName)
		protocolInput := strings.TrimSpace(in.Protocol)
		protocol := config.NormalizeAliasProtocol(protocolInput)
		if existing := cfg.FindAlias(name); existing != nil {
			updated := *existing
			if displayName != "" {
				updated.DisplayName = displayName
			}
			if protocolInput != "" {
				if updated.AutoGenerated && !cfg.AliasNameMatchesProviderModel(name, protocol) {
					return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q is a model catalog alias; protocol %q does not match any catalog entry", name, protocol)
				}
				updated.Protocol = protocol
			}
			updated.Enabled = !in.Disabled
			if updated.AutoGenerated && !cfg.AliasNameMatchesProviderModel(name, updated.Protocol) {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q is a model catalog alias; edit the existing automatic alias or choose a custom alias name", name)
			}
			cfg.UpsertAlias(updated)
			view = aliasView(cfg, updated)
			return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
		}
		if cfg.AliasNameMatchesProviderModel(name, protocol) {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q is managed by the Provider Group model catalog; refresh models to create the automatic alias or choose a custom alias name", name)
		}
		a := config.Alias{Alias: name, DisplayName: displayName, Protocol: protocol, Enabled: !in.Disabled}
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
	groupID := strings.TrimSpace(in.Group)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || groupID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider, group and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		p, group := cfg.FindProviderGroup(providerID, groupID)
		if p == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q does not exist; add it first", providerID)
		}
		if group == nil {
			// Exact group only — never fall back to first/default/same-protocol sibling.
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("provider %q group %q not found", providerID, groupID)
		}
		groupProtocol := config.NormalizeProviderProtocol(group.Protocol)
		if err := validateProviderModelKnown(providerID, group.Models, group.ModelsSource, model); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		indexes := lifecycle.FindAliasesByName(cfg, alias)
		if len(indexes) > 1 {
			return configstore.Mutation[*config.Config]{}, &OutcomeError{Code: "plan_not_executable", Params: map[string]any{"reason": lifecycle.ReasonAliasAmbiguous}}
		}
		if len(indexes) == 1 {
			currentAlias := cfg.Aliases[indexes[0]]
			if !config.ProtocolsMatch(currentAlias.Protocol, groupProtocol) {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q protocol %q does not match provider %q group %q protocol %q", alias, config.NormalizeAliasProtocol(currentAlias.Protocol), providerID, groupID, groupProtocol)
			}
		} else {
			cfg.UpsertAlias(config.Alias{Alias: alias, Protocol: groupProtocol, Enabled: true, AutoGenerated: cfg.AliasNameMatchesProviderModel(alias, groupProtocol)})
		}
		if err := cfg.AddTarget(alias, config.Target{Provider: providerID, Group: groupID, Model: model, Enabled: !in.Disabled}); err != nil {
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
	groupID := strings.TrimSpace(in.Group)
	model := strings.TrimSpace(in.Model)
	if alias == "" || providerID == "" || groupID == "" || model == "" {
		return AliasView{}, fmt.Errorf("alias, provider, group and model are required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		current := cfg.FindAlias(alias)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", alias)
		}
		found := false
		for _, target := range current.Targets {
			if target.Provider == providerID && strings.TrimSpace(target.Group) == groupID && target.Model == model {
				found = true
				if current.AutoGenerated && target.AutoGenerated {
					return configstore.Mutation[*config.Config]{}, fmt.Errorf("system-generated target %s/%s/%s on automatic model alias %q cannot be removed; disable the target instead", providerID, groupID, model, alias)
				}
				break
			}
		}
		if !found {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("target %s/%s/%s not found on alias %s", providerID, groupID, model, alias)
		}
		if err := cfg.RemoveTargetFromGroup(alias, providerID, groupID, model); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		current = cfg.FindAlias(alias)
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
		groupID := strings.TrimSpace(target.Group)
		model := strings.TrimSpace(target.Model)
		if providerID == "" || groupID == "" || model == "" {
			return AliasView{}, fmt.Errorf("target provider, group and model are required")
		}
		refs = append(refs, config.TargetRef{
			Provider: providerID,
			Group:    groupID,
			Model:    model,
		})
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
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

func (s *Service) ResetAliasTargetOrder(ctx context.Context, in AliasLockInput) (AliasView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return AliasView{}, fmt.Errorf("alias name is required")
	}
	var view AliasView
	_, err := s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		if err := cfg.ResetAliasTargetOrder(name); err != nil {
			return configstore.Mutation[*config.Config]{}, err
		}
		current := cfg.FindAlias(name)
		if current == nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("alias %q not found", name)
		}
		view = aliasView(cfg, *current)
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	return view, err
}

func providerConnectionEqual(a, b config.Provider) bool {
	if !providerSharedConnectionEqual(a, b) {
		return false
	}
	ag := a.FindGroup(config.DefaultGroupID)
	bg := b.FindGroup(config.DefaultGroupID)
	if ag == nil || bg == nil {
		return ag == nil && bg == nil
	}
	return providerGroupAuthEqual(*ag, *bg)
}

// providerSharedConnectionEqual compares Provider-level shared connection fields.
// When these change, every group discovery result becomes untrusted.
func providerSharedConnectionEqual(a, b config.Provider) bool {
	return config.ProviderBaseURLsEqual(a, b) &&
		config.NormalizeProviderBaseURLStrategy(a.BaseURLStrategy) == config.NormalizeProviderBaseURLStrategy(b.BaseURLStrategy) &&
		reflect.DeepEqual(normalizeProviderHeaders(a.Headers), normalizeProviderHeaders(b.Headers))
}

// providerGroupAuthEqual compares one group's protocol and API keys.
func providerGroupAuthEqual(a, b config.ProviderGroup) bool {
	return config.NormalizeProviderProtocol(a.Protocol) == config.NormalizeProviderProtocol(b.Protocol) &&
		reflect.DeepEqual(a.EffectiveAPIKeys(), b.EffectiveAPIKeys())
}

func resolveProviderGroupID(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return config.DefaultGroupID
	}
	return groupID
}

func cloneProviderGroupsForEdit(in []config.ProviderGroup) []config.ProviderGroup {
	if len(in) == 0 {
		return nil
	}
	out := make([]config.ProviderGroup, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].APIKeys = append([]string(nil), in[i].APIKeys...)
		out[i].Models = append([]string(nil), in[i].Models...)
	}
	return out
}

func markAllGroupModelsUntrusted(provider *config.Provider) {
	if provider == nil {
		return
	}
	for i := range provider.Groups {
		provider.Groups[i].ModelsSource = ""
	}
}

func markGroupModelsUntrusted(provider *config.Provider, groupID string) {
	if provider == nil {
		return
	}
	groupID = resolveProviderGroupID(groupID)
	if g := provider.FindGroup(groupID); g != nil {
		g.ModelsSource = ""
	}
}

func applyDiscoveredModelsToGroup(provider *config.Provider, groupID string, models []string, source string) {
	if provider == nil {
		return
	}
	groupID = resolveProviderGroupID(groupID)
	normalized := config.NormalizeProviderModels(models)
	if g := provider.FindGroup(groupID); g != nil {
		g.Models = append([]string(nil), normalized...)
		g.ModelsSource = source
	}
}

func preserveExistingGroupModels(provider *config.Provider, existing *config.Provider, groupID string, untrusted bool) {
	if provider == nil || existing == nil {
		return
	}
	groupID = resolveProviderGroupID(groupID)
	source := ""
	if !untrusted {
		if eg := existing.FindGroup(groupID); eg != nil {
			source = eg.ModelsSource
		}
	}
	var models []string
	if eg := existing.FindGroup(groupID); eg != nil {
		models = append([]string(nil), eg.Models...)
	}
	applyDiscoveredModelsToGroup(provider, groupID, models, source)
	if untrusted {
		markGroupModelsUntrusted(provider, groupID)
	}
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
	protocol := config.NormalizeProviderProtocol(ip.Protocol)
	source := "imported"
	if len(importedModels) == 0 {
		source = ""
	}
	merged := config.Provider{
		ID:              ip.ID,
		Name:            ip.Name,
		BaseURL:         config.NormalizeProviderBaseURL(ip.BaseURL),
		BaseURLs:        config.NormalizeProviderBaseURLs(ip.BaseURL, nil),
		BaseURLStrategy: config.ProviderBaseURLStrategyOrdered,
		Headers:         cloneHeaders(ip.Headers),
		Groups: []config.ProviderGroup{{
			ID:           config.DefaultGroupID,
			Name:         config.DefaultGroupName,
			Protocol:     protocol,
			APIKey:       strings.TrimSpace(ip.APIKey),
			Models:       append([]string(nil), importedModels...),
			ModelsSource: source,
		}},
	}
	if existing == nil {
		return merged
	}
	// Preserve sibling groups; import only rewrites the default group catalog/auth.
	merged.Groups = cloneProviderGroupsForEdit(existing.Groups)
	keys := config.NormalizeProviderAPIKeys(ip.APIKey, nil)
	if g := merged.FindGroup(config.DefaultGroupID); g != nil {
		g.Protocol = protocol
		if len(keys) == 0 {
			g.APIKey = ""
			g.APIKeys = nil
		} else {
			g.APIKey = keys[0]
			if len(keys) > 1 {
				g.APIKeys = append([]string(nil), keys[1:]...)
			} else {
				g.APIKeys = nil
			}
		}
		if len(importedModels) > 0 {
			g.Models = append([]string(nil), importedModels...)
			g.ModelsSource = "imported"
		} else {
			g.Models = nil
			g.ModelsSource = ""
		}
	} else {
		// Existing provider without a default group — materialize one from import.
		group := config.ProviderGroup{
			ID:           config.DefaultGroupID,
			Name:         config.DefaultGroupName,
			Protocol:     protocol,
			Models:       append([]string(nil), importedModels...),
			ModelsSource: source,
		}
		if len(keys) > 0 {
			group.APIKey = keys[0]
			if len(keys) > 1 {
				group.APIKeys = append([]string(nil), keys[1:]...)
			}
		}
		merged.Groups = append(merged.Groups, group)
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
	if eg := existing.FindGroup(config.DefaultGroupID); eg != nil && eg.ModelsSource == "discovered" {
		prospective := merged
		prospective.Headers = cloneHeaders(existing.Headers)
		prospective.Disabled = existing.Disabled
		if providerConnectionEqual(*existing, prospective) {
			if g := merged.FindGroup(config.DefaultGroupID); g != nil {
				g.Models = append([]string(nil), eg.Models...)
				g.ModelsSource = eg.ModelsSource
			}
			return merged
		}
		if len(importedModels) == 0 {
			if g := merged.FindGroup(config.DefaultGroupID); g != nil {
				g.Models = append([]string(nil), eg.Models...)
				g.ModelsSource = ""
			}
			return merged
		}
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
	// Auto-alias is group-scoped; trigger when any group has a discovered catalog.
	hasDiscovered := false
	for _, group := range provider.Groups {
		if group.ModelsSource == "discovered" && len(group.Models) > 0 {
			hasDiscovered = true
			break
		}
	}
	if !hasDiscovered {
		return nil
	}
	var removed []string
	for _, group := range provider.Groups {
		if group.ModelsSource == "discovered" {
			removed = append(removed, cfg.ReconcileProviderGroupAutoTargets(provider.ID, group.ID, group.Models)...)
		}
	}
	var warnings []string
	if len(removed) > 0 {
		warnings = append(warnings, fmt.Sprintf("removed stale targets from %d auto alias(es): %s", len(removed), strings.Join(removed, ", ")))
	}
	// Generation is optional, but trusted catalog reconciliation above must always
	// revoke stale system targets even after auto-alias creation is disabled.
	if !cfg.IsAutoAliasEnabled() || !provider.EffectiveAutoAliasEnabled() {
		return warnings
	}
	created, updated := cfg.AutoGenerateAliases(provider)
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

// providerDiscoveryFingerprint captures the shared connection + group auth used
// for an out-of-transaction model discovery probe.
type providerDiscoveryFingerprint struct {
	BaseURLs        []string
	BaseURLStrategy string
	Headers         map[string]string
	Protocol        string
	APIKeys         []string
	Models          []string
	ModelsSource    string
}

func captureProviderDiscoveryFingerprint(provider config.Provider, groupID string) providerDiscoveryFingerprint {
	groupID = resolveProviderGroupID(groupID)
	fp := providerDiscoveryFingerprint{
		BaseURLs:        append([]string(nil), provider.EffectiveBaseURLs()...),
		BaseURLStrategy: config.NormalizeProviderBaseURLStrategy(provider.BaseURLStrategy),
		Headers:         normalizeProviderHeaders(provider.Headers),
	}
	if g := provider.FindGroup(groupID); g != nil {
		fp.Protocol = config.NormalizeProviderProtocol(g.Protocol)
		fp.APIKeys = append([]string(nil), g.EffectiveAPIKeys()...)
		fp.Models = append([]string(nil), g.Models...)
		fp.ModelsSource = g.ModelsSource
	}
	return fp
}

// MatchesProviderGroup reports whether live provider shared connection and the
// target group's protocol/API keys still match the discovery-time fingerprint.
func (fp providerDiscoveryFingerprint) MatchesProviderGroup(provider config.Provider, groupID string) bool {
	groupID = resolveProviderGroupID(groupID)
	if !slices.Equal(fp.BaseURLs, provider.EffectiveBaseURLs()) {
		return false
	}
	if fp.BaseURLStrategy != config.NormalizeProviderBaseURLStrategy(provider.BaseURLStrategy) {
		return false
	}
	if !reflect.DeepEqual(fp.Headers, normalizeProviderHeaders(provider.Headers)) {
		return false
	}
	group := provider.FindGroup(groupID)
	if group == nil {
		return false
	}
	if fp.Protocol != config.NormalizeProviderProtocol(group.Protocol) {
		return false
	}
	// slices.Equal treats nil and empty as equal (unlike reflect.DeepEqual).
	return slices.Equal(fp.APIKeys, group.EffectiveAPIKeys())
}

func (fp providerDiscoveryFingerprint) MatchesProviderGroupCatalog(provider config.Provider, groupID string) bool {
	group := provider.FindGroup(resolveProviderGroupID(groupID))
	return group != nil && slices.Equal(fp.Models, group.Models) && fp.ModelsSource == group.ModelsSource
}

// providerGroupDiscoveryOutcome is the catalog result of an out-of-transaction
// discovery plus the fingerprint required to apply it safely.
// Succeeded is true only when discovery returned a non-empty catalog; failure
// and empty results must not overwrite the live concurrent catalog at commit.
type providerGroupDiscoveryOutcome struct {
	Fingerprint    providerDiscoveryFingerprint
	Models         []string
	ModelsSource   string
	PrimaryBaseURL string
	Warnings       []string
	Succeeded      bool
}

// runProviderGroupDiscovery probes models for one group and captures a merge-safe
// outcome. Mutates provider in place for local prospective builds; callers that
// commit must re-apply only successful catalog fields under fingerprint check.
// Caller ctx is required so cancel/deadline abort serial BaseURL/key probes.
func runProviderGroupDiscovery(ctx context.Context, provider *config.Provider, groupID string, existing *config.Provider) (*providerGroupDiscoveryOutcome, error) {
	if provider == nil {
		return &providerGroupDiscoveryOutcome{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = resolveProviderGroupID(groupID)
	beforeBaseURL := provider.BaseURL
	fp := captureProviderDiscoveryFingerprint(*provider, groupID)
	warnings, succeeded, err := discoverProviderGroupModels(ctx, provider, groupID, existing)
	if err != nil {
		return nil, err
	}
	out := &providerGroupDiscoveryOutcome{
		Fingerprint: fp,
		Warnings:    warnings,
		Succeeded:   succeeded,
	}
	if succeeded {
		if g := provider.FindGroup(groupID); g != nil {
			out.Models = append([]string(nil), g.Models...)
			out.ModelsSource = g.ModelsSource
		}
	}
	if provider.BaseURL != beforeBaseURL && strings.TrimSpace(provider.BaseURL) != "" {
		out.PrimaryBaseURL = provider.BaseURL
	}
	return out, nil
}

func buildProviderShell(providerID string, in ProviderUpsertInput, baseURLs []string) config.Provider {
	return config.Provider{
		ID:              providerID,
		Name:            strings.TrimSpace(in.Name),
		BaseURL:         baseURLs[0],
		BaseURLs:        append([]string(nil), baseURLs...),
		BaseURLStrategy: config.NormalizeProviderBaseURLStrategy(in.BaseURLStrategy),
		Headers:         normalizeProviderHeaders(in.Headers),
		Disabled:        in.Disabled,
	}
}

// applyProviderSharedFieldsFromLive writes caller-intended shared fields onto
// provider while preserving live Name/Headers/AutoAlias when the input omits them.
func applyProviderSharedFieldsFromLive(provider *config.Provider, live config.Provider, in ProviderUpsertInput, baseURLs []string) {
	if provider == nil {
		return
	}
	provider.ID = live.ID
	name := strings.TrimSpace(in.Name)
	if name == "" {
		provider.Name = live.Name
	} else {
		provider.Name = name
	}
	provider.BaseURL = baseURLs[0]
	provider.BaseURLs = append([]string(nil), baseURLs...)
	provider.BaseURLStrategy = config.NormalizeProviderBaseURLStrategy(in.BaseURLStrategy)
	headers := normalizeProviderHeaders(in.Headers)
	if len(headers) == 0 && !in.ClearHeaders && len(live.Headers) > 0 {
		provider.Headers = cloneHeaders(live.Headers)
	} else if in.ClearHeaders {
		provider.Headers = nil
	} else {
		provider.Headers = headers
	}
	provider.Disabled = in.Disabled
	if in.AutoAliasEnabled != nil {
		v := *in.AutoAliasEnabled
		provider.AutoAliasEnabled = &v
	} else if live.AutoAliasEnabled != nil {
		v := *live.AutoAliasEnabled
		provider.AutoAliasEnabled = &v
	} else {
		provider.AutoAliasEnabled = nil
	}
}

func applyProviderSharedFieldsOnCreate(provider *config.Provider, in ProviderUpsertInput, baseURLs []string) {
	if provider == nil {
		return
	}
	provider.Name = strings.TrimSpace(in.Name)
	provider.BaseURL = baseURLs[0]
	provider.BaseURLs = append([]string(nil), baseURLs...)
	provider.BaseURLStrategy = config.NormalizeProviderBaseURLStrategy(in.BaseURLStrategy)
	provider.Headers = normalizeProviderHeaders(in.Headers)
	provider.Disabled = in.Disabled
}

func applyProviderAutoAliasOnCreate(provider *config.Provider, autoAliasEnabled *bool) {
	if provider == nil {
		return
	}
	if autoAliasEnabled != nil {
		v := *autoAliasEnabled
		provider.AutoAliasEnabled = &v
		return
	}
	// Create with nil => default enabled (persist explicit true for clarity).
	enabled := true
	provider.AutoAliasEnabled = &enabled
}

func normalizeDefaultGroupInput(in ProviderGroupInput, existing *config.Provider) (ProviderGroupInput, error) {
	groupInput := in
	if strings.TrimSpace(groupInput.ID) == "" {
		groupInput.ID = config.DefaultGroupID
	}
	if groupInput.ID != config.DefaultGroupID {
		return ProviderGroupInput{}, fmt.Errorf("defaultGroup.id must be %q", config.DefaultGroupID)
	}
	// Create: empty name defaults to DefaultGroupName.
	// Update: leave empty so commit-time apply preserves live concurrent renames
	// (do not freeze pre-network snapshot name and overwrite at commit).
	if strings.TrimSpace(groupInput.Name) == "" && existing == nil {
		groupInput.Name = config.DefaultGroupName
	}
	return groupInput, nil
}

// applyDefaultGroupUpdate mutates only the default group on provider (create if missing).
func applyDefaultGroupUpdate(provider *config.Provider, groupInput ProviderGroupInput) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	idx := -1
	for i := range provider.Groups {
		if provider.Groups[i].ID == config.DefaultGroupID {
			idx = i
			break
		}
	}
	if idx < 0 {
		createInput := groupInput
		if strings.TrimSpace(createInput.Name) == "" {
			createInput.Name = config.DefaultGroupName
		}
		group, buildErr := buildProviderGroupFromInput(createInput, nil)
		if buildErr != nil {
			return buildErr
		}
		provider.Groups = append(provider.Groups, group)
		return nil
	}
	before := provider.Groups[idx]
	beforeModels := append([]string(nil), before.Models...)
	beforeSource := before.ModelsSource
	if err := applyProviderGroupInput(&provider.Groups[idx], groupInput, &before); err != nil {
		return err
	}
	if !providerGroupAuthEqual(before, provider.Groups[idx]) {
		provider.Groups[idx].ModelsSource = ""
	}
	// CLI --skip-models re-passes the existing catalog as non-nil Models
	// so discovery is skipped; keep provenance when auth+catalog unchanged.
	if groupInput.Models != nil &&
		providerGroupAuthEqual(before, provider.Groups[idx]) &&
		reflect.DeepEqual(provider.Groups[idx].Models, beforeModels) {
		provider.Groups[idx].ModelsSource = beforeSource
	}
	return nil
}

// discoverProviderModels is the migration-period helper that always targets the
// default group. New call sites should use discoverProviderGroupModels.
func discoverProviderModels(ctx context.Context, provider *config.Provider, existing *config.Provider) ([]string, bool, error) {
	return discoverProviderGroupModels(ctx, provider, config.DefaultGroupID, existing)
}

// discoverProviderGroupModels refreshes models for one exact group.
// BaseURLs/headers come from the Provider; protocol/API keys come from the Group.
// Results are written only to the target group's models/models_source.
// Succeeded is true only for a non-empty discovered catalog. Caller cancel/deadline
// aborts immediately and is returned as a hard error (not a soft warning).
func discoverProviderGroupModels(ctx context.Context, provider *config.Provider, groupID string, existing *config.Provider) (warnings []string, succeeded bool, err error) {
	if provider == nil {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = resolveProviderGroupID(groupID)
	if provider.FindGroup(groupID) == nil {
		return []string{fmt.Sprintf("provider %q group %q not found", provider.ID, groupID)}, false, nil
	}
	group := provider.FindGroup(groupID)
	if group == nil {
		return []string{fmt.Sprintf("provider %q group %q not found", provider.ID, groupID)}, false, nil
	}
	// Snapshot sibling groups so discovery cannot accidentally mutate them.
	siblingSnapshots := snapshotSiblingGroupCatalogs(*provider, groupID)

	probeInput := opencode.ProviderGroupModelsInput{
		ProviderID: provider.ID,
		GroupID:    groupID,
		Protocol:   group.Protocol,
		BaseURLs:   provider.EffectiveBaseURLs(),
		APIKeys:    group.EffectiveAPIKeys(),
		Headers:    provider.Headers,
	}
	models, probe, fetchErr := opencode.FetchProviderGroupModels(ctx, probeInput)
	if probe != nil && probe.Reachable && probe.BaseURL != "" {
		provider.BaseURL = probe.BaseURL
	}

	// Cancel/deadline must surface immediately — do not soft-warn and continue commit.
	if fetchErr != nil && (ctx.Err() != nil || errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded)) {
		restoreSiblingGroupCatalogs(provider, siblingSnapshots)
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, fetchErr
	}

	sharedChanged := existing != nil && !providerSharedConnectionEqual(*existing, *provider)
	groupAuthChanged := false
	if existing != nil {
		if eg := existing.FindGroup(groupID); eg != nil {
			groupAuthChanged = !providerGroupAuthEqual(*eg, *group)
		} else {
			// Newly materialised group relative to the previous snapshot.
			groupAuthChanged = true
		}
	}
	catalogChanged := sharedChanged || groupAuthChanged

	if fetchErr != nil {
		if existing != nil && catalogChanged {
			// Local prospective only: commit keeps live catalog and applies untrusted
			// via sharedChanged / auth change on the mutation path.
			preserveExistingGroupModels(provider, existing, groupID, true)
			if sharedChanged {
				markAllGroupModelsUntrusted(provider)
			}
			warnings = append(warnings, "provider connection changed and model discovery failed; keeping existing model catalog as untrusted")
		}
		restoreSiblingGroupCatalogs(provider, siblingSnapshots)
		warnings = append(warnings, fmt.Sprintf("could not discover provider models: %v", fetchErr))
		return warnings, false, nil
	}
	if normalized := config.NormalizeProviderModels(models); len(normalized) > 0 {
		applyDiscoveredModelsToGroup(provider, groupID, normalized, "discovered")
		restoreSiblingGroupCatalogs(provider, siblingSnapshots)
		return nil, true, nil
	}
	if existing != nil && catalogChanged {
		preserveExistingGroupModels(provider, existing, groupID, true)
		if sharedChanged {
			markAllGroupModelsUntrusted(provider)
		}
		restoreSiblingGroupCatalogs(provider, siblingSnapshots)
		return []string{"provider connection changed and model discovery returned no models; keeping existing model catalog as untrusted"}, false, nil
	}
	restoreSiblingGroupCatalogs(provider, siblingSnapshots)
	return []string{"provider model discovery returned no models; keeping existing model catalog"}, false, nil
}

type groupCatalogSnapshot struct {
	Models       []string
	ModelsSource string
}

func snapshotSiblingGroupCatalogs(provider config.Provider, targetGroupID string) map[string]groupCatalogSnapshot {
	out := make(map[string]groupCatalogSnapshot, len(provider.Groups))
	targetGroupID = resolveProviderGroupID(targetGroupID)
	for _, g := range provider.Groups {
		if g.ID == targetGroupID {
			continue
		}
		out[g.ID] = groupCatalogSnapshot{
			Models:       append([]string(nil), g.Models...),
			ModelsSource: g.ModelsSource,
		}
	}
	return out
}

func restoreSiblingGroupCatalogs(provider *config.Provider, snapshots map[string]groupCatalogSnapshot) {
	if provider == nil || len(snapshots) == 0 {
		return
	}
	for i := range provider.Groups {
		snap, ok := snapshots[provider.Groups[i].ID]
		if !ok {
			continue
		}
		provider.Groups[i].Models = append([]string(nil), snap.Models...)
		provider.Groups[i].ModelsSource = snap.ModelsSource
	}
}

func buildProviderGroupFromInput(in ProviderGroupInput, existing *config.ProviderGroup) (config.ProviderGroup, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return config.ProviderGroup{}, fmt.Errorf("group id is required")
	}
	protocol := strings.TrimSpace(in.Protocol)
	if err := config.ValidateProtocol(protocol); err != nil {
		return config.ProviderGroup{}, fmt.Errorf("invalid group protocol: %w", err)
	}
	group := config.ProviderGroup{
		ID:       id,
		Name:     strings.TrimSpace(in.Name),
		Protocol: protocol,
		Disabled: in.Disabled,
	}
	if existing != nil {
		group.Models = append([]string(nil), existing.Models...)
		group.ModelsSource = existing.ModelsSource
		group.APIKey = existing.APIKey
		group.APIKeys = append([]string(nil), existing.APIKeys...)
	}
	if err := applyProviderGroupInput(&group, in, existing); err != nil {
		return config.ProviderGroup{}, err
	}
	return group, nil
}

func applyProviderGroupInput(group *config.ProviderGroup, in ProviderGroupInput, existing *config.ProviderGroup) error {
	if group == nil {
		return fmt.Errorf("group is required")
	}
	if err := validateProviderGroupAPIKeysInput(in); err != nil {
		return err
	}
	id := strings.TrimSpace(in.ID)
	if id != "" {
		group.ID = id
	}
	// Empty Name means omitted: keep live/existing name so concurrent renames survive.
	name := strings.TrimSpace(in.Name)
	if in.NameChanged {
		group.Name = name
	} else if name != "" {
		group.Name = name
	} else if existing != nil {
		if strings.TrimSpace(group.Name) == "" {
			group.Name = strings.TrimSpace(existing.Name)
		}
	} else {
		group.Name = ""
	}
	protocol := strings.TrimSpace(in.Protocol)
	if err := config.ValidateProtocol(protocol); err != nil {
		return fmt.Errorf("invalid group protocol: %w", err)
	}
	group.Protocol = protocol
	group.Disabled = in.Disabled
	if in.Models != nil {
		group.Models = config.NormalizeProviderModels(in.Models)
		// Explicit models write is a manual catalog — not discovered/imported provenance.
		group.ModelsSource = ""
	} else if existing != nil {
		group.Models = append([]string(nil), existing.Models...)
		group.ModelsSource = existing.ModelsSource
	}
	if !in.APIKeysChanged {
		if existing != nil {
			group.APIKey = existing.APIKey
			group.APIKeys = append([]string(nil), existing.APIKeys...)
		}
		return nil
	}
	if err := rejectMaskedProviderGroupAPIKeys(in.APIKeys); err != nil {
		return err
	}
	keys := config.NormalizeProviderAPIKeys("", in.APIKeys)
	if len(keys) == 0 {
		group.APIKey = ""
		group.APIKeys = nil
		return nil
	}
	group.APIKey = keys[0]
	if len(keys) > 1 {
		group.APIKeys = append([]string(nil), keys[1:]...)
	} else {
		group.APIKeys = nil
	}
	return nil
}

func validateProviderGroupAPIKeysInput(in ProviderGroupInput) error {
	if in.APIKeysChanged {
		return rejectMaskedProviderGroupAPIKeys(in.APIKeys)
	}
	for _, key := range in.APIKeys {
		if strings.TrimSpace(key) != "" {
			return fmt.Errorf("apiKeys must be empty when apiKeysChanged is false")
		}
	}
	return nil
}

func rejectMaskedProviderGroupAPIKeys(keys []string) error {
	for _, key := range keys {
		if isMaskedAPIKeyPlaceholder(key) {
			return fmt.Errorf("apiKeys must not contain masked placeholders")
		}
	}
	return nil
}

// isMaskedAPIKeyPlaceholder detects UI/mask placeholders that must never be persisted.
// Covers existing maskKey output (ellipsis), asterisks, bullets, and short "***".
func isMaskedAPIKeyPlaceholder(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	if k == "***" || k == "…" || k == "..." {
		return true
	}
	if strings.Contains(k, "…") || strings.Contains(k, "...") {
		return true
	}
	if strings.Contains(k, "*") {
		return true
	}
	// Bullet / middle-dot style masks from password fields.
	if strings.ContainsAny(k, "•●∙·▪▫") {
		return true
	}
	// All-mask-character tokens (e.g. "••••").
	maskOnly := true
	for _, r := range k {
		if r != '*' && r != '•' && r != '●' && r != '∙' && r != '·' && r != '…' && r != '.' {
			maskOnly = false
			break
		}
	}
	return maskOnly && len(k) > 0
}

func groupFieldsChanged(before, after config.ProviderGroup) bool {
	return before.Name != after.Name ||
		before.Protocol != after.Protocol ||
		before.Disabled != after.Disabled ||
		before.ModelsSource != after.ModelsSource ||
		!reflect.DeepEqual(before.EffectiveAPIKeys(), after.EffectiveAPIKeys()) ||
		!reflect.DeepEqual(before.Models, after.Models)
}

func providerGroupSelectorsFromInput(groups []ProviderGroupSelectorInput, legacyProviders []string) []config.ProviderGroupSelector {
	if groups != nil {
		// Preserve explicit empty slice as wildcard (distinct from omitted/nil).
		out := make([]config.ProviderGroupSelector, 0, len(groups))
		seen := map[string]bool{}
		for _, sel := range groups {
			provider := strings.TrimSpace(sel.Provider)
			group := strings.TrimSpace(sel.Group)
			key := provider + "\x00" + group
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, config.ProviderGroupSelector{Provider: provider, Group: group})
		}
		if len(groups) == 0 {
			return []config.ProviderGroupSelector{}
		}
		return out
	}
	// Legacy non-empty providers map only to default group — never expand to siblings.
	if len(legacyProviders) == 0 {
		return nil
	}
	out := make([]config.ProviderGroupSelector, 0, len(legacyProviders))
	seen := map[string]bool{}
	for _, provider := range legacyProviders {
		provider = strings.TrimSpace(provider)
		if provider == "" || seen[provider] {
			continue
		}
		seen[provider] = true
		out = append(out, config.ProviderGroupSelector{Provider: provider, Group: config.DefaultGroupID})
	}
	return out
}
