// Package config manages the local ocswitch JSON config file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Apale7/opencode-provider-switch/internal/fileutil"
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
	Protocol    string   `json:"protocol,omitempty"`
	Enabled     bool     `json:"enabled"`
	Targets     []Target `json:"targets"`
}

// Provider is one upstream OpenAI-compatible endpoint.
type Provider struct {
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	Protocol     string            `json:"protocol,omitempty"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key"`
	Headers      map[string]string `json:"headers,omitempty"`
	Models       []string          `json:"models,omitempty"`
	ModelsSource string            `json:"models_source,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
}

// Server holds proxy listen settings.
type Server struct {
	Host                    string `json:"host"`
	Port                    int    `json:"port"`
	APIKey                  string `json:"api_key"`
	ConnectTimeoutMs        int    `json:"connect_timeout_ms,omitempty"`
	ResponseHeaderTimeoutMs int    `json:"response_header_timeout_ms,omitempty"`
	FirstByteTimeoutMs      int    `json:"first_byte_timeout_ms,omitempty"`
	RequestReadTimeoutMs    int    `json:"request_read_timeout_ms,omitempty"`
	StreamIdleTimeoutMs     int    `json:"stream_idle_timeout_ms,omitempty"`
}

const (
	DefaultConnectTimeoutMs        = 10_000
	DefaultResponseHeaderTimeoutMs = 15_000
	DefaultFirstByteTimeoutMs      = 15_000
	DefaultRequestReadTimeoutMs    = 30_000
	DefaultStreamIdleTimeoutMs     = 60_000
)

// Desktop holds desktop-shell user preferences.
type Desktop struct {
	LaunchAtLogin  bool   `json:"launch_at_login,omitempty"`
	AutoStartProxy bool   `json:"auto_start_proxy,omitempty"`
	MinimizeToTray bool   `json:"minimize_to_tray,omitempty"`
	Notifications  bool   `json:"notifications,omitempty"`
	Theme          string `json:"theme,omitempty"`
	Language       string `json:"language,omitempty"`
}

// Config is the on-disk ocswitch config.
type Config struct {
	Server    Server     `json:"server"`
	Desktop   Desktop    `json:"desktop,omitempty"`
	Providers []Provider `json:"providers"`
	Aliases   []Alias    `json:"aliases"`

	path string
	mu   sync.RWMutex
}

// IsEnabled reports whether the provider can be used for routing.
func (p Provider) IsEnabled() bool {
	return !p.Disabled
}

// NormalizeProviderBaseURL canonicalizes equivalent /v1 roots for comparisons
// and persisted config values.
func NormalizeProviderBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// ValidateProviderBaseURL checks protocol-specific upstream base URL rules.
func ValidateProviderBaseURL(protocol string, baseURL string) error {
	protocol = NormalizeProviderProtocol(protocol)
	if err := ValidateProtocol(protocol); err != nil {
		return err
	}
	trimmed := NormalizeProviderBaseURL(baseURL)
	if trimmed == "" {
		return fmt.Errorf("missing base_url")
	}
	if !strings.HasSuffix(trimmed, ProtocolLocalBasePath(protocol)) {
		return fmt.Errorf("base_url must end with /v1")
	}
	return nil
}

// Default returns an empty config with sane defaults.
func Default() *Config {
	return &Config{
		Server: Server{
			Host:                    "127.0.0.1",
			Port:                    9982,
			APIKey:                  DefaultLocalAPIKey,
			ConnectTimeoutMs:        DefaultConnectTimeoutMs,
			ResponseHeaderTimeoutMs: DefaultResponseHeaderTimeoutMs,
			FirstByteTimeoutMs:      DefaultFirstByteTimeoutMs,
			RequestReadTimeoutMs:    DefaultRequestReadTimeoutMs,
			StreamIdleTimeoutMs:     DefaultStreamIdleTimeoutMs,
		},
		Desktop:   Desktop{},
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
	normalizeProviders(c.Providers)
	normalizeAliases(c.Aliases)
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 9982
	}
	if c.Server.APIKey == "" {
		c.Server.APIKey = DefaultLocalAPIKey
	}
	normalizeServerTimeouts(&c.Server)
	c.path = path
	return c, nil
}

// Path returns the on-disk path of this config.
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// Save writes config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		c.path = DefaultPath()
	}
	path := c.path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return fileutil.WithLockedFile(path, func() error {
		providers := cloneProviders(c.Providers)
		normalizeProviders(providers)
		sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
		aliases := cloneAliases(c.Aliases)
		normalizeAliases(aliases)
		sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
		snap := struct {
			Server    Server     `json:"server"`
			Desktop   Desktop    `json:"desktop,omitempty"`
			Providers []Provider `json:"providers"`
			Aliases   []Alias    `json:"aliases"`
		}{c.Server, c.Desktop, providers, aliases}
		data, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		data = append(data, '\n')
		return fileutil.AtomicWriteFile(path, data, 0o600)
	})
}

// FindProvider returns the provider with matching id or nil.
func (c *Config) FindProvider(id string) *Provider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := c.findProviderLocked(id)
	if p == nil {
		return nil
	}
	clone := cloneProvider(*p)
	return &clone
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
	p.Protocol = NormalizeProviderProtocol(p.Protocol)
	cloned := cloneProvider(p)
	for i := range c.Providers {
		if c.Providers[i].ID == p.ID {
			c.Providers[i] = cloned
			return
		}
	}
	c.Providers = append(c.Providers, cloned)
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
			clone := cloneAlias(c.Aliases[i])
			return &clone
		}
	}
	return nil
}

// UpsertAlias adds or replaces an alias.
func (c *Config) UpsertAlias(a Alias) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a.Protocol = NormalizeAliasProtocol(a.Protocol)
	cloned := cloneAlias(a)
	for i := range c.Aliases {
		if c.Aliases[i].Alias == a.Alias {
			c.Aliases[i] = cloned
			return
		}
	}
	c.Aliases = append(c.Aliases, cloned)
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
		if !ProtocolsMatch(alias.Protocol, provider.Protocol) {
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
	return c.availableAliasNamesLocked("")
}

// AvailableAliasNamesForProtocol returns enabled alias names for one protocol.
func (c *Config) AvailableAliasNamesForProtocol(protocol string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.availableAliasNamesLocked(protocol)
}

func (c *Config) availableAliasNamesLocked(protocol string) []string {
	names := make([]string, 0, len(c.Aliases))
	for _, a := range c.Aliases {
		if !a.Enabled {
			continue
		}
		if protocol != "" && NormalizeAliasProtocol(a.Protocol) != NormalizeAliasProtocol(protocol) {
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
		p.Protocol = NormalizeProviderProtocol(p.Protocol)
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
		if err := ValidateProtocol(p.Protocol); err != nil {
			errs = append(errs, fmt.Errorf("provider %q %s", p.ID, err))
		}
		if err := ValidateProviderBaseURL(p.Protocol, p.BaseURL); err != nil {
			errs = append(errs, fmt.Errorf("provider %q %s", p.ID, err))
		}
		seenModels := map[string]bool{}
		for _, model := range p.Models {
			trimmed := strings.TrimSpace(model)
			if trimmed == "" {
				errs = append(errs, fmt.Errorf("provider %q has empty model entry", p.ID))
				continue
			}
			if seenModels[trimmed] {
				errs = append(errs, fmt.Errorf("provider %q has duplicate model %q", p.ID, trimmed))
				continue
			}
			seenModels[trimmed] = true
		}
		validModelsSource := p.ModelsSource == "" || p.ModelsSource == "discovered" || p.ModelsSource == "imported"
		if !validModelsSource {
			errs = append(errs, fmt.Errorf("provider %q has invalid models_source %q", p.ID, p.ModelsSource))
		}
		if validModelsSource && p.ModelsSource != "" && len(p.Models) == 0 {
			errs = append(errs, fmt.Errorf("provider %q has models_source %q but no models", p.ID, p.ModelsSource))
		}
	}
	seen := map[string]bool{}
	for _, a := range c.Aliases {
		a.Protocol = NormalizeAliasProtocol(a.Protocol)
		if a.Alias == "" {
			errs = append(errs, fmt.Errorf("alias with empty name"))
			continue
		}
		if seen[a.Alias] {
			errs = append(errs, fmt.Errorf("duplicate alias %q", a.Alias))
		}
		seen[a.Alias] = true
		if err := ValidateProtocol(a.Protocol); err != nil {
			errs = append(errs, fmt.Errorf("alias %q %s", a.Alias, err))
		}
		enabled := 0
		for _, t := range a.Targets {
			if t.Provider == "" || t.Model == "" {
				errs = append(errs, fmt.Errorf("alias %q has malformed target", a.Alias))
				continue
			}
			if !ids[t.Provider] {
				errs = append(errs, fmt.Errorf("alias %q references unknown provider %q", a.Alias, t.Provider))
				continue
			}
			provider := c.findProviderLocked(t.Provider)
			if provider != nil && !ProtocolsMatch(a.Protocol, provider.Protocol) {
				errs = append(errs, fmt.Errorf("alias %q target %s/%s protocol %q does not match provider protocol %q", a.Alias, t.Provider, t.Model, a.Protocol, NormalizeProviderProtocol(provider.Protocol)))
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
	if c.Server.APIKey == DefaultLocalAPIKey && !isLoopbackHost(c.Server.Host) {
		errs = append(errs, fmt.Errorf("server.api_key must not use the default value when listening on non-loopback host %q", c.Server.Host))
	}
	if c.Server.ConnectTimeoutMs <= 0 {
		errs = append(errs, fmt.Errorf("server.connect_timeout_ms must be greater than 0"))
	}
	if c.Server.ResponseHeaderTimeoutMs <= 0 {
		errs = append(errs, fmt.Errorf("server.response_header_timeout_ms must be greater than 0"))
	}
	if c.Server.FirstByteTimeoutMs <= 0 {
		errs = append(errs, fmt.Errorf("server.first_byte_timeout_ms must be greater than 0"))
	}
	if c.Server.RequestReadTimeoutMs <= 0 {
		errs = append(errs, fmt.Errorf("server.request_read_timeout_ms must be greater than 0"))
	}
	if c.Server.StreamIdleTimeoutMs <= 0 {
		errs = append(errs, fmt.Errorf("server.stream_idle_timeout_ms must be greater than 0"))
	}
	return errs
}

func normalizeServerTimeouts(server *Server) {
	if server == nil {
		return
	}
	server.ConnectTimeoutMs = normalizeServerTimeoutMs(server.ConnectTimeoutMs, DefaultConnectTimeoutMs)
	server.ResponseHeaderTimeoutMs = normalizeServerTimeoutMs(server.ResponseHeaderTimeoutMs, DefaultResponseHeaderTimeoutMs)
	server.FirstByteTimeoutMs = normalizeServerTimeoutMs(server.FirstByteTimeoutMs, DefaultFirstByteTimeoutMs)
	server.RequestReadTimeoutMs = normalizeServerTimeoutMs(server.RequestReadTimeoutMs, DefaultRequestReadTimeoutMs)
	server.StreamIdleTimeoutMs = normalizeServerTimeoutMs(server.StreamIdleTimeoutMs, DefaultStreamIdleTimeoutMs)
}

func normalizeServerTimeoutMs(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func cloneProvider(p Provider) Provider {
	p.Protocol = NormalizeProviderProtocol(p.Protocol)
	p.Headers = cloneStringMap(p.Headers)
	p.Models = cloneStrings(p.Models)
	return p
}

func cloneAlias(a Alias) Alias {
	a.Protocol = NormalizeAliasProtocol(a.Protocol)
	a.Targets = cloneTargets(a.Targets)
	return a
}

func normalizeProviders(providers []Provider) {
	for i := range providers {
		providers[i].Protocol = NormalizeProviderProtocol(providers[i].Protocol)
		providers[i].BaseURL = NormalizeProviderBaseURL(providers[i].BaseURL)
	}
}

func normalizeAliases(aliases []Alias) {
	for i := range aliases {
		aliases[i].Protocol = NormalizeAliasProtocol(aliases[i].Protocol)
	}
}

func cloneProviders(in []Provider) []Provider {
	out := make([]Provider, len(in))
	for i := range in {
		out[i] = cloneProvider(in[i])
	}
	return out
}

func cloneAliases(in []Alias) []Alias {
	out := make([]Alias, len(in))
	for i := range in {
		out[i] = cloneAlias(in[i])
	}
	return out
}

func cloneTargets(in []Target) []Target {
	out := make([]Target, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
