package config

import (
	"fmt"
	"strings"
)

const (
	// CurrentSchemaVersion is the canonical on-disk schema version.
	CurrentSchemaVersion = 2
	// DefaultGroupID is the stable group id used for legacy migration.
	DefaultGroupID = "default"
	// DefaultGroupName is the display name for the migrated default group.
	DefaultGroupName = "Default"
)

// ProviderGroup is one business group under a Provider.
type ProviderGroup struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Protocol     string   `json:"protocol"`
	APIKey       string   `json:"api_key,omitempty"`
	APIKeys      []string `json:"api_keys,omitempty"`
	Models       []string `json:"models,omitempty"`
	ModelsSource string   `json:"models_source,omitempty"`
	Disabled     bool     `json:"disabled,omitempty"`
}

// ProviderGroupSelector is a precise rewrite selector for (provider, group).
type ProviderGroupSelector struct {
	Provider string `json:"provider"`
	Group    string `json:"group"`
}

// IsEnabled reports whether the group can be used for routing.
func (g ProviderGroup) IsEnabled() bool {
	return !g.Disabled
}

// EffectiveAPIKeys returns the group's normalized upstream API key list.
func (g ProviderGroup) EffectiveAPIKeys() []string {
	return NormalizeProviderAPIKeys(g.APIKey, g.APIKeys)
}

// FindGroup returns the group with matching id or nil.
func (p *Provider) FindGroup(id string) *ProviderGroup {
	if p == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	for i := range p.Groups {
		if p.Groups[i].ID == id {
			return &p.Groups[i]
		}
	}
	return nil
}

// FindProviderGroup returns the provider and group for the given ids.
func (c *Config) FindProviderGroup(providerID, groupID string) (*Provider, *ProviderGroup) {
	if c == nil {
		return nil, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := c.findProviderLocked(providerID)
	if p == nil {
		return nil, nil
	}
	clone := cloneProvider(*p)
	g := clone.FindGroup(groupID)
	if g == nil {
		return &clone, nil
	}
	return &clone, g
}

func cloneProviderGroup(g ProviderGroup) ProviderGroup {
	g.Protocol = strings.TrimSpace(g.Protocol)
	apiKeys := NormalizeProviderAPIKeys(g.APIKey, g.APIKeys)
	if len(apiKeys) > 0 {
		g.APIKey = apiKeys[0]
	} else {
		g.APIKey = strings.TrimSpace(g.APIKey)
	}
	if len(apiKeys) > 1 {
		g.APIKeys = cloneStrings(apiKeys[1:])
	} else {
		g.APIKeys = nil
	}
	g.Models = cloneStrings(g.Models)
	return g
}

func cloneProviderGroups(in []ProviderGroup) []ProviderGroup {
	if len(in) == 0 {
		return nil
	}
	out := make([]ProviderGroup, len(in))
	for i := range in {
		out[i] = cloneProviderGroup(in[i])
	}
	return out
}

func normalizeProviderGroup(g *ProviderGroup) {
	if g == nil {
		return
	}
	g.ID = strings.TrimSpace(g.ID)
	g.Name = strings.TrimSpace(g.Name)
	g.Protocol = strings.TrimSpace(g.Protocol)
	apiKeys := NormalizeProviderAPIKeys(g.APIKey, g.APIKeys)
	if len(apiKeys) > 0 {
		g.APIKey = apiKeys[0]
	} else {
		g.APIKey = strings.TrimSpace(g.APIKey)
	}
	if len(apiKeys) > 1 {
		g.APIKeys = cloneStrings(apiKeys[1:])
	} else {
		g.APIKeys = nil
	}
	g.Models = cloneStrings(g.Models)
	g.ModelsSource = strings.TrimSpace(g.ModelsSource)
}

func normalizeProviderGroups(groups []ProviderGroup) {
	for i := range groups {
		normalizeProviderGroup(&groups[i])
	}
}

func cloneProviderGroupSelectors(in []ProviderGroupSelector) []ProviderGroupSelector {
	if in == nil {
		return nil
	}
	// Preserve explicit empty wildcard (non-nil empty slice) vs omitted (nil).
	out := make([]ProviderGroupSelector, len(in))
	copy(out, in)
	return out
}

// protocolAuthHeaderNames returns shared header keys managed by group protocols.
func protocolAuthHeaderNames() []string {
	return []string{"Authorization", "X-Api-Key", "X-API-Key"}
}

func headerKeyEqualsFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func providerSharedHeadersConflictWithProtocols(p Provider) error {
	if len(p.Groups) <= 1 {
		// Single legacy default group keeps compatible diagnostics (no hard block).
		return nil
	}
	if len(p.Headers) == 0 {
		return nil
	}
	authNames := protocolAuthHeaderNames()
	for key := range p.Headers {
		for _, auth := range authNames {
			if headerKeyEqualsFold(key, auth) {
				return fmt.Errorf("provider %q shared headers must not set protocol auth header %q when multiple groups are configured", p.ID, key)
			}
		}
	}
	return nil
}
