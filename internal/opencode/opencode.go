// Package opencode reads and writes OpenCode config files, including the
// `provider.ops` sync path. Files may be JSON or JSONC; we preserve the
// detected extension on write.
package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

// ConfigFileCandidates is the precedence order inside the global config dir.
var ConfigFileCandidates = []string{"opencode.jsonc", "opencode.json", "config.json"}

// GlobalConfigDir returns the default user-global OpenCode config directory.
func GlobalConfigDir() string {
	if dir := os.Getenv("OPENCODE_CONFIG_DIR"); dir != "" {
		return dir
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config", "opencode")
}

// ResolveGlobalConfigPath returns the existing global config file if any,
// otherwise the default target path (opencode.jsonc).
func ResolveGlobalConfigPath() (path string, existed bool) {
	dir := GlobalConfigDir()
	for _, name := range ConfigFileCandidates {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return filepath.Join(dir, "opencode.jsonc"), false
}

// Raw is the JSON object form of an OpenCode config. We treat it as a generic
// map so unknown fields pass through untouched on write.
type Raw map[string]any

// Load reads an OpenCode config file. Missing files yield an empty Raw.
// JSONC (line/block comments, trailing commas) is supported on read, but the
// write side always emits plain JSON (comments are not preserved).
func Load(path string) (Raw, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Raw{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return Raw{}, nil
	}
	stripped := jsonc.ToJSON(data)
	out := Raw{}
	if err := json.Unmarshal(stripped, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

// Save writes Raw back to path. Parent dirs are created. Writes are atomic.
func Save(path string, raw Raw) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// ensure $schema first; tidwall/jsonc indent not needed — plain JSON is fine.
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// EnsureOpsProvider updates (or creates) the provider.ops entry with the given
// local base URL, local api key and alias set. Existing keys on provider.ops
// are preserved unless they conflict with the sync intent. Returns true if the
// file would actually change.
func EnsureOpsProvider(raw Raw, baseURL, apiKey string, aliases []string) bool {
	changed := false
	if _, ok := raw["$schema"]; !ok {
		raw["$schema"] = "https://opencode.ai/config.json"
		changed = true
	}
	provRaw, _ := raw["provider"].(map[string]any)
	if provRaw == nil {
		provRaw = map[string]any{}
		raw["provider"] = provRaw
		changed = true
	}
	opsRaw, _ := provRaw["ops"].(map[string]any)
	if opsRaw == nil {
		opsRaw = map[string]any{}
		provRaw["ops"] = opsRaw
		changed = true
	}
	if setIfDiff(opsRaw, "npm", "@ai-sdk/openai") {
		changed = true
	}
	if setIfDiff(opsRaw, "name", "OPS") {
		changed = true
	}
	opts, _ := opsRaw["options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
		opsRaw["options"] = opts
		changed = true
	}
	if setIfDiff(opts, "baseURL", baseURL) {
		changed = true
	}
	if setIfDiff(opts, "apiKey", apiKey) {
		changed = true
	}
	// Build models map from alias list. Preserve any existing per-model extras
	// if the alias key matches; drop aliases removed locally.
	existingModels, _ := opsRaw["models"].(map[string]any)
	newModels := map[string]any{}
	aliasSet := map[string]bool{}
	for _, a := range aliases {
		aliasSet[a] = true
		if existing, ok := existingModels[a].(map[string]any); ok {
			// make sure "name" stays consistent with alias key
			if setIfDiff(existing, "name", a) {
				changed = true
			}
			newModels[a] = existing
		} else {
			newModels[a] = map[string]any{"name": a}
			changed = true
		}
	}
	// removed entries?
	for k := range existingModels {
		if !aliasSet[k] {
			changed = true
		}
	}
	if !mapsEqualShallow(existingModels, newModels) {
		opsRaw["models"] = newModels
	}
	return changed
}

// setIfDiff assigns key=val and returns true if the value actually changed.
func setIfDiff(m map[string]any, key string, val any) bool {
	cur, ok := m[key]
	if ok && cur == val {
		return false
	}
	m[key] = val
	return true
}

// ImportableProvider is a subset extracted from an OpenCode custom provider.
type ImportableProvider struct {
	ID      string
	Name    string
	BaseURL string
	APIKey  string
	Models  []string
}

// ImportCustomProviders scans raw for @ai-sdk/openai custom providers that
// declare baseURL and an apiKey-compatible setting. The `ops` id itself is
// skipped so sync output is not re-imported.
func ImportCustomProviders(raw Raw) []ImportableProvider {
	out := []ImportableProvider{}
	provRaw, _ := raw["provider"].(map[string]any)
	for id, v := range provRaw {
		if id == "ops" {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		npm, _ := m["npm"].(string)
		if npm != "@ai-sdk/openai" {
			continue
		}
		opts, _ := m["options"].(map[string]any)
		if opts == nil {
			continue
		}
		baseURL, _ := opts["baseURL"].(string)
		apiKey, _ := opts["apiKey"].(string)
		if baseURL == "" {
			continue
		}
		ip := ImportableProvider{
			ID:      id,
			BaseURL: baseURL,
			APIKey:  apiKey,
		}
		if n, ok := m["name"].(string); ok {
			ip.Name = n
		}
		if models, ok := m["models"].(map[string]any); ok {
			for k := range models {
				ip.Models = append(ip.Models, k)
			}
		}
		out = append(out, ip)
	}
	return out
}

// mapsEqualShallow compares two string-keyed maps for structural equality.
func mapsEqualShallow(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		am, amOk := v.(map[string]any)
		bm, bmOk := bv.(map[string]any)
		if amOk && bmOk {
			if !mapsEqualShallow(am, bm) {
				return false
			}
			continue
		}
		if v != bv {
			return false
		}
	}
	return true
}
