// Package config manages the local ocswitch JSON config file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	AppName            = "ocswitch"
	ConfigEnvVar       = "OCSWITCH_CONFIG"
	ConfigDirName      = "ocswitch"
	DefaultLocalAPIKey = "ocswitch-local"
)

// Target is one concrete upstream candidate for an alias.
type Target struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
}

// Alias maps a logical model name to ordered upstream targets.
type Alias struct {
	Alias       string   `json:"alias"`
	DisplayName string   `json:"display_name,omitempty"`
	Enabled     bool     `json:"enabled"`
	Targets     []Target `json:"targets"`
}

// Provider is one upstream OpenAI-compatible endpoint.
type Provider struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	BaseURL  string            `json:"base_url"`
	APIKey   string            `json:"api_key"`
	Headers  map[string]string `json:"headers,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

// Server holds proxy listen settings.
type Server struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	APIKey string `json:"api_key"`
}

// Config is the on-disk ocswitch config.
type Config struct {
	Server    Server     `json:"server"`
	Providers []Provider `json:"providers"`
	Aliases   []Alias    `json:"aliases"`

	path string
	mu   sync.RWMutex
}

// IsEnabled reports whether the provider can be used for routing.
func (p Provider) IsEnabled() bool {
	return !p.Disabled
}

// ValidateProviderBaseURL checks the MVP requirement that upstream base URLs
// point at an OpenAI-compatible /v1 root.
func ValidateProviderBaseURL(baseURL string) error {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return fmt.Errorf("missing base_url")
	}
	if !strings.HasSuffix(trimmed, "/v1") {
		return fmt.Errorf("base_url must end with /v1")
	}
	return nil
}

// Default returns an empty config with sane defaults.
func Default() *Config {
	return &Config{
		Server: Server{
			Host:   "127.0.0.1",
			Port:   9982,
			APIKey: DefaultLocalAPIKey,
		},
		Providers: []Provider{},
		Aliases:   []Alias{},
	}
}

// DefaultPath returns ~/.config/ocswitch/config.json (XDG aware).
func DefaultPath() string {
	if p := os.Getenv(ConfigEnvVar); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, ConfigDirName, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AppName + "-config.json"
	}
	return filepath.Join(home, ".config", ConfigDirName, "config.json")
}

// Load reads the config at path. Missing file returns a default config anchored to path.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	c := Default()
	c.path = path
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 9982
	}
	if c.Server.APIKey == "" {
		c.Server.APIKey = DefaultLocalAPIKey
	}
	c.path = path
	return c, nil
}

// Path returns the on-disk path of this config.
func (c *Config) Path() string { return c.path }

// Save writes config atomically.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.path == "" {
		c.path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// sort for stability
	providers := append([]Provider(nil), c.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	aliases := append([]Alias(nil), c.Aliases...)
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
	snap := struct {
		Server    Server     `json:"server"`
		Providers []Provider `json:"providers"`
		Aliases   []Alias    `json:"aliases"`
	}{c.Server, providers, aliases}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, c.path)
}

// FindProvider returns the provider with matching id or nil.
func (c *Config) FindProvider(id string) *Provider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.findProviderLocked(id)
}

func (c *Config) findProviderLocked(id string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

// UpsertProvider adds or replaces a provider by id.
func (c *Config) UpsertProvider(p Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Providers {
		if c.Providers[i].ID == p.ID {
			c.Providers[i] = p
			return
		}
	}
	c.Providers = append(c.Providers, p)
}

// RemoveProvider deletes a provider and returns true if removed.
func (c *Config) RemoveProvider(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			c.Providers = append(c.Providers[:i], c.Providers[i+1:]...)
			return true
		}
	}
	return false
}

// FindAlias returns the alias record with matching name or nil.
func (c *Config) FindAlias(name string) *Alias {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Aliases {
		if c.Aliases[i].Alias == name {
			return &c.Aliases[i]
		}
	}
	return nil
}

// UpsertAlias adds or replaces an alias.
func (c *Config) UpsertAlias(a Alias) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Aliases {
		if c.Aliases[i].Alias == a.Alias {
			c.Aliases[i] = a
			return
		}
	}
	c.Aliases = append(c.Aliases, a)
}

// RemoveAlias deletes an alias.
func (c *Config) RemoveAlias(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Aliases {
		if c.Aliases[i].Alias == name {
			c.Aliases = append(c.Aliases[:i], c.Aliases[i+1:]...)
			return true
		}
	}
	return false
}

// AddTarget appends a target to an alias; creates alias if missing.
func (c *Config) AddTarget(alias string, t Target) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Aliases {
		if c.Aliases[i].Alias == alias {
			// prevent exact duplicate
			for _, x := range c.Aliases[i].Targets {
				if x.Provider == t.Provider && x.Model == t.Model {
					return fmt.Errorf("target %s/%s already bound to alias %s", t.Provider, t.Model, alias)
				}
			}
			c.Aliases[i].Targets = append(c.Aliases[i].Targets, t)
			return nil
		}
	}
	return fmt.Errorf("alias %q not found", alias)
}

// RemoveTarget removes a target by provider+model.
func (c *Config) RemoveTarget(alias, provider, model string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Aliases {
		if c.Aliases[i].Alias != alias {
			continue
		}
		out := c.Aliases[i].Targets[:0]
		found := false
		for _, t := range c.Aliases[i].Targets {
			if t.Provider == provider && t.Model == model {
				found = true
				continue
			}
			out = append(out, t)
		}
		if !found {
			return fmt.Errorf("target %s/%s not found on alias %s", provider, model, alias)
		}
		c.Aliases[i].Targets = out
		return nil
	}
	return fmt.Errorf("alias %q not found", alias)
}

// AvailableTargets returns alias targets that are individually enabled and point
// at providers that still exist and are enabled.
func (c *Config) AvailableTargets(alias Alias) []Target {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.availableTargetsLocked(alias)
}

func (c *Config) availableTargetsLocked(alias Alias) []Target {
	targets := make([]Target, 0, len(alias.Targets))
	for _, t := range alias.Targets {
		if !t.Enabled {
			continue
		}
		provider := c.findProviderLocked(t.Provider)
		if provider == nil || !provider.IsEnabled() {
			continue
		}
		targets = append(targets, t)
	}
	return targets
}

// AvailableAliasNames returns alias names that are enabled and still have at
// least one routable target after provider availability is applied.
func (c *Config) AvailableAliasNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.Aliases))
	for _, a := range c.Aliases {
		if !a.Enabled {
			continue
		}
		if len(c.availableTargetsLocked(a)) == 0 {
			continue
		}
		names = append(names, a.Alias)
	}
	return names
}

// Validate returns a non-nil error slice for every structural issue found.
func (c *Config) Validate() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var errs []error
	ids := map[string]bool{}
	for _, p := range c.Providers {
		if p.ID == "" {
			errs = append(errs, fmt.Errorf("provider with empty id"))
			continue
		}
		if ids[p.ID] {
			errs = append(errs, fmt.Errorf("duplicate provider id %q", p.ID))
		}
		ids[p.ID] = true
		if p.BaseURL == "" {
			errs = append(errs, fmt.Errorf("provider %q missing base_url", p.ID))
			continue
		}
		if err := ValidateProviderBaseURL(p.BaseURL); err != nil {
			errs = append(errs, fmt.Errorf("provider %q %s", p.ID, err))
		}
	}
	seen := map[string]bool{}
	for _, a := range c.Aliases {
		if a.Alias == "" {
			errs = append(errs, fmt.Errorf("alias with empty name"))
			continue
		}
		if seen[a.Alias] {
			errs = append(errs, fmt.Errorf("duplicate alias %q", a.Alias))
		}
		seen[a.Alias] = true
		enabled := 0
		for _, t := range a.Targets {
			if t.Provider == "" || t.Model == "" {
				errs = append(errs, fmt.Errorf("alias %q has malformed target", a.Alias))
				continue
			}
			if !ids[t.Provider] {
				errs = append(errs, fmt.Errorf("alias %q references unknown provider %q", a.Alias, t.Provider))
			}
		}
		enabled = len(c.availableTargetsLocked(a))
		if a.Enabled && enabled == 0 {
			errs = append(errs, fmt.Errorf("alias %q has no available targets", a.Alias))
		}
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Errorf("invalid server port %d", c.Server.Port))
	}
	return errs
}
