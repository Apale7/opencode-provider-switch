// Package opencode reads and writes OpenCode config files, including the
// `provider.ocswitch` sync path. Files may be JSON or JSONC; we preserve the
// detected extension on write.
package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/fileutil"
	"github.com/tidwall/jsonc"
)

const (
	ProviderKey           = "ocswitch"
	AnthropicProviderKey  = "ocswitch-anthropic"
	CompatProviderKey     = "ocswitch-compat"
	ProviderName          = "OpenCode Provider Switch CLI"
	AnthropicProviderName = "OpenCode Provider Switch Anthropic"
	CompatProviderName    = "OpenCode Provider Switch Compat"
)

// ConfigFileCandidates is the precedence order inside the global config dir.
var ConfigFileCandidates = []string{"opencode.jsonc", "opencode.json", "config.json"}

// GlobalConfigDir returns the default user-global OpenCode config directory.
// MVP intentionally ignores OPENCODE_CONFIG_DIR for default sync scope; callers
// that want another file must pass --target explicitly.
func GlobalConfigDir() string {
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

// Save writes provider.ocswitch back to path. Existing files are normalized to plain
// JSON and only the provider.ocswitch subtree is patched so unrelated key order stays
// stable. New files are still written from the full Raw object. Writes are
// atomic.
func Save(path string, raw Raw) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return fileutil.WithLockedFile(path, func() error {
		data, err := renderSaveData(path, raw)
		if err != nil {
			return err
		}
		if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
			return err
		}
		return nil
	})
}
func renderSaveData(path string, raw Raw) ([]byte, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		return marshalRaw(raw)
	}
	if len(bytes.TrimSpace(original)) == 0 {
		return marshalRaw(raw)
	}
	patched, err := patchProviderDocument(original, raw)
	if err != nil {
		return nil, fmt.Errorf("patch %s: %w", path, err)
	}
	if !json.Valid(patched) {
		return nil, fmt.Errorf("patch %s: produced invalid json", path)
	}
	patched = append(bytes.TrimRight(patched, "\n"), '\n')
	return patched, nil
}

func marshalRaw(raw Raw) ([]byte, error) {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

func patchProviderDocument(original []byte, raw Raw) ([]byte, error) {
	providerValues := syncedProviderValues(raw)
	if len(providerValues) == 0 {
		return marshalRaw(raw)
	}
	normalized := bytes.TrimSpace(jsonc.ToJSON(original))
	if len(normalized) == 0 {
		return marshalRaw(raw)
	}
	if !json.Valid(normalized) {
		return nil, fmt.Errorf("source config is not valid json/jsonc")
	}
	rootStart := skipWhitespace(normalized, 0)
	root, end, err := parseObjectSpan(normalized, rootStart)
	if err != nil {
		return nil, err
	}
	if skipWhitespace(normalized, end) != len(normalized) {
		return nil, fmt.Errorf("source config must be a single top-level object")
	}
	provider := root.findMember("provider")
	if provider == nil {
		return insertObjectMember(normalized, root, "provider", providerValues)
	}
	if normalized[provider.valueStart] != '{' {
		return nil, fmt.Errorf("top-level provider must be an object")
	}
	providerObj, _, err := parseObjectSpan(normalized, provider.valueStart)
	if err != nil {
		return nil, err
	}
	for _, key := range syncedProviderKeys() {
		providerValue, ok := providerValues[key]
		if !ok {
			continue
		}
		providerEntry := providerObj.findMember(key)
		if providerEntry == nil {
			var insertErr error
			normalized, insertErr = insertObjectMember(normalized, providerObj, key, providerValue)
			if insertErr != nil {
				return nil, insertErr
			}
			providerObj, _, err = parseObjectSpan(normalized, provider.valueStart)
			if err != nil {
				return nil, err
			}
			continue
		}
		var replaceErr error
		normalized, replaceErr = replaceObjectMember(normalized, *providerEntry, providerValue)
		if replaceErr != nil {
			return nil, replaceErr
		}
		providerObj, _, err = parseObjectSpan(normalized, provider.valueStart)
		if err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

func syncedProviderValues(raw Raw) map[string]any {
	providerRaw, _ := raw["provider"].(map[string]any)
	if providerRaw == nil {
		return nil
	}
	out := map[string]any{}
	for _, key := range syncedProviderKeys() {
		providerEntry, _ := providerRaw[key].(map[string]any)
		if providerEntry != nil {
			out[key] = providerEntry
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func syncedProviderKeys() []string {
	return []string{ProviderKey, AnthropicProviderKey, CompatProviderKey}
}

type objectSpan struct {
	start   int
	end     int
	members []objectMember
}

type objectMember struct {
	key        string
	start      int
	valueStart int
	valueEnd   int
}

func (o objectSpan) findMember(key string) *objectMember {
	for i := range o.members {
		if o.members[i].key == key {
			return &o.members[i]
		}
	}
	return nil
}

func replaceObjectMember(data []byte, member objectMember, value any) ([]byte, error) {
	memberIndent := lineIndent(data, member.start)
	replacement, err := formatObjectMember(member.key, value, memberIndent)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, data[:member.start]...)
	out = append(out, replacement...)
	out = append(out, data[member.valueEnd:]...)
	return out, nil
}

func insertObjectMember(data []byte, obj objectSpan, key string, value any) ([]byte, error) {
	objIndent := lineIndent(data, obj.start)
	childIndent := objIndent + "  "
	if len(obj.members) > 0 {
		childIndent = lineIndent(data, obj.members[0].start)
	}
	memberText, err := formatObjectMember(key, value, childIndent)
	if err != nil {
		return nil, err
	}
	if len(obj.members) == 0 {
		out := append([]byte{}, data[:obj.start+1]...)
		out = append(out, '\n')
		out = append(out, childIndent...)
		out = append(out, memberText...)
		out = append(out, '\n')
		out = append(out, objIndent...)
		out = append(out, data[obj.end-1:]...)
		return out, nil
	}
	insertAt := obj.members[len(obj.members)-1].valueEnd
	out := append([]byte{}, data[:insertAt]...)
	out = append(out, []byte(",\n")...)
	out = append(out, childIndent...)
	out = append(out, memberText...)
	out = append(out, '\n')
	out = append(out, objIndent...)
	out = append(out, data[obj.end-1:]...)
	return out, nil
}

func formatObjectMember(key string, value any, indent string) ([]byte, error) {
	valueJSON, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal member %q: %w", key, err)
	}
	lines := bytes.Split(valueJSON, []byte("\n"))
	quotedKey, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key %q: %w", key, err)
	}
	out := append([]byte{}, quotedKey...)
	out = append(out, []byte(": ")...)
	out = append(out, lines[0]...)
	for _, line := range lines[1:] {
		out = append(out, '\n')
		out = append(out, indent...)
		out = append(out, line...)
	}
	return out, nil
}

func parseObjectSpan(data []byte, start int) (objectSpan, int, error) {
	if start >= len(data) || data[start] != '{' {
		return objectSpan{}, 0, fmt.Errorf("expected object at byte %d", start)
	}
	obj := objectSpan{start: start}
	i := skipWhitespace(data, start+1)
	if i >= len(data) {
		return objectSpan{}, 0, fmt.Errorf("unterminated object")
	}
	if data[i] == '}' {
		obj.end = i + 1
		return obj, obj.end, nil
	}
	for {
		memberStart := skipWhitespace(data, i)
		key, next, err := parseJSONString(data, memberStart)
		if err != nil {
			return objectSpan{}, 0, err
		}
		i = skipWhitespace(data, next)
		if i >= len(data) || data[i] != ':' {
			return objectSpan{}, 0, fmt.Errorf("expected ':' after key %q", key)
		}
		valueStart := skipWhitespace(data, i+1)
		valueEnd, err := parseValueEnd(data, valueStart)
		if err != nil {
			return objectSpan{}, 0, err
		}
		obj.members = append(obj.members, objectMember{key: key, start: memberStart, valueStart: valueStart, valueEnd: valueEnd})
		i = skipWhitespace(data, valueEnd)
		if i >= len(data) {
			return objectSpan{}, 0, fmt.Errorf("unterminated object")
		}
		if data[i] == '}' {
			obj.end = i + 1
			return obj, obj.end, nil
		}
		if data[i] != ',' {
			return objectSpan{}, 0, fmt.Errorf("expected ',' or '}' in object")
		}
		i++
	}
}

func parseValueEnd(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, fmt.Errorf("missing value at byte %d", start)
	}
	switch data[start] {
	case '{':
		_, end, err := parseObjectSpan(data, start)
		return end, err
	case '[':
		return parseArrayEnd(data, start)
	case '"':
		_, end, err := parseJSONString(data, start)
		return end, err
	default:
		end := start
		for end < len(data) {
			switch data[end] {
			case ' ', '\n', '\r', '\t', ',', '}', ']':
				return end, nil
			default:
				end++
			}
		}
		return end, nil
	}
}

func parseArrayEnd(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '[' {
		return 0, fmt.Errorf("expected array at byte %d", start)
	}
	i := skipWhitespace(data, start+1)
	if i >= len(data) {
		return 0, fmt.Errorf("unterminated array")
	}
	if data[i] == ']' {
		return i + 1, nil
	}
	for {
		valueStart := skipWhitespace(data, i)
		valueEnd, err := parseValueEnd(data, valueStart)
		if err != nil {
			return 0, err
		}
		i = skipWhitespace(data, valueEnd)
		if i >= len(data) {
			return 0, fmt.Errorf("unterminated array")
		}
		if data[i] == ']' {
			return i + 1, nil
		}
		if data[i] != ',' {
			return 0, fmt.Errorf("expected ',' or ']' in array")
		}
		i++
	}
}

func parseJSONString(data []byte, start int) (string, int, error) {
	if start >= len(data) || data[start] != '"' {
		return "", 0, fmt.Errorf("expected string at byte %d", start)
	}
	i := start + 1
	for i < len(data) {
		if data[i] == '\\' {
			i += 2
			continue
		}
		if data[i] == '"' {
			var out string
			if err := json.Unmarshal(data[start:i+1], &out); err != nil {
				return "", 0, fmt.Errorf("parse string at byte %d: %w", start, err)
			}
			return out, i + 1, nil
		}
		i++
	}
	return "", 0, fmt.Errorf("unterminated string at byte %d", start)
}

func skipWhitespace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\n', '\r', '\t':
			i++
		default:
			return i
		}
	}
	return i
}

func lineIndent(data []byte, pos int) string {
	lineStart := pos
	for lineStart > 0 && data[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := lineStart
	for lineEnd < len(data) {
		if data[lineEnd] == ' ' || data[lineEnd] == '\t' {
			lineEnd++
			continue
		}
		break
	}
	return string(data[lineStart:lineEnd])
}

// EnsureOcswitchProvider updates (or creates) the provider.ocswitch entry with the given
// local base URL, local api key and alias set. Existing keys on provider.ocswitch
// are preserved unless they conflict with the sync intent. For model entries,
// sync owns only the alias set: same-name model objects are left untouched so
// OpenCode-only metadata survives round-trips. Returns true if the file would
// actually change.
func EnsureOcswitchProvider(protocol string, raw Raw, baseURL, apiKey string, aliases []string) bool {
	providerKey, providerName, providerNPM, optionsPatch, err := syncedProviderContract(protocol)
	if err != nil {
		return false
	}
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
	providerEntry, _ := provRaw[providerKey].(map[string]any)
	if providerEntry == nil {
		providerEntry = map[string]any{}
		provRaw[providerKey] = providerEntry
		changed = true
	}
	if setIfDiff(providerEntry, "npm", providerNPM) {
		changed = true
	}
	if setIfDiff(providerEntry, "name", providerName) {
		changed = true
	}
	opts, _ := providerEntry["options"].(map[string]any)
	if opts == nil {
		opts = map[string]any{}
		providerEntry["options"] = opts
		changed = true
	}
	if setIfDiff(opts, "baseURL", baseURL) {
		changed = true
	}
	if setIfDiff(opts, "apiKey", apiKey) {
		changed = true
	}
	for key, value := range optionsPatch {
		if setIfDiff(opts, key, value) {
			changed = true
		}
	}
	// Build models map from alias list. Preserve any existing per-model objects
	// verbatim if the alias key matches; drop aliases removed locally.
	existingModels, _ := providerEntry["models"].(map[string]any)
	newModels := map[string]any{}
	aliasSet := map[string]bool{}
	for _, a := range aliases {
		aliasSet[a] = true
		if existing, ok := existingModels[a].(map[string]any); ok {
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
		providerEntry["models"] = newModels
	}
	return changed
}

// ValidateOcswitchProvider checks that provider.ocswitch matches current sync contract.
func ValidateOcswitchProvider(protocol string, raw Raw, baseURL, apiKey string, aliases []string) error {
	providerKey, providerName, providerNPM, optionsPatch, err := syncedProviderContract(protocol)
	if err != nil {
		return err
	}
	provRaw, _ := raw["provider"].(map[string]any)
	if provRaw == nil {
		return fmt.Errorf("missing provider object")
	}
	providerEntry, _ := provRaw[providerKey].(map[string]any)
	if providerEntry == nil {
		return fmt.Errorf("missing provider.%s", providerKey)
	}
	if npm, _ := providerEntry["npm"].(string); npm != providerNPM {
		return fmt.Errorf("provider.%s.npm must be %s", providerKey, providerNPM)
	}
	if name, _ := providerEntry["name"].(string); name != providerName {
		return fmt.Errorf("provider.%s.name must be %s", providerKey, providerName)
	}
	opts, _ := providerEntry["options"].(map[string]any)
	if opts == nil {
		return fmt.Errorf("provider.%s.options missing", providerKey)
	}
	if got, _ := opts["baseURL"].(string); got != baseURL {
		return fmt.Errorf("provider.%s.options.baseURL mismatch", providerKey)
	}
	if got, _ := opts["apiKey"].(string); got != apiKey {
		return fmt.Errorf("provider.%s.options.apiKey mismatch", providerKey)
	}
	for key, want := range optionsPatch {
		if got, ok := opts[key]; !ok || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("provider.%s.options.%s mismatch", providerKey, key)
		}
	}
	models, _ := providerEntry["models"].(map[string]any)
	if models == nil {
		return fmt.Errorf("provider.%s.models missing", providerKey)
	}
	expected := append([]string(nil), aliases...)
	sort.Strings(expected)
	actual := make([]string, 0, len(models))
	for alias, v := range models {
		modelCfg, _ := v.(map[string]any)
		if modelCfg == nil {
			return fmt.Errorf("provider.%s.models.%s must be an object", providerKey, alias)
		}
		actual = append(actual, alias)
	}
	sort.Strings(actual)
	if len(actual) != len(expected) {
		return fmt.Errorf("provider.%s.models alias set mismatch", providerKey)
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return fmt.Errorf("provider.%s.models alias set mismatch", providerKey)
		}
	}
	return nil
}

func syncedProviderContract(protocol string) (providerKey string, providerName string, npm string, options map[string]any, err error) {
	switch strings.TrimSpace(protocol) {
	case "", "openai-responses":
		return ProviderKey, ProviderName, "@ai-sdk/openai", map[string]any{"setCacheKey": true}, nil
	case "anthropic-messages":
		return AnthropicProviderKey, AnthropicProviderName, "@ai-sdk/anthropic", map[string]any{
			"setCacheKey": true,
			"headers": map[string]any{
				"anthropic-version": "2023-06-01",
			},
		}, nil
	case "openai-compatible":
		return CompatProviderKey, CompatProviderName, "@ai-sdk/openai-compatible", map[string]any{"setCacheKey": true}, nil
	default:
		return "", "", "", nil, fmt.Errorf("unsupported sync protocol %q", protocol)
	}
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
	ID       string
	Name     string
	Protocol string
	BaseURL  string
	APIKey   string
	Headers  map[string]string
	Models   []string
}

// ImportCustomProviders scans raw for supported custom providers that
// declare baseURL and an apiKey-compatible setting. The synced provider id itself is
// skipped so sync output is not re-imported.
func ImportCustomProviders(raw Raw) []ImportableProvider {
	out := []ImportableProvider{}
	provRaw, _ := raw["provider"].(map[string]any)
	for id, v := range provRaw {
		if id == ProviderKey || id == AnthropicProviderKey || id == CompatProviderKey {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		npm, _ := m["npm"].(string)
		protocol, ok := importProtocolForNPM(npm)
		if !ok {
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
			ID:       id,
			Protocol: protocol,
			BaseURL:  baseURL,
			APIKey:   apiKey,
		}
		if n, ok := m["name"].(string); ok {
			ip.Name = n
		}
		if headers, ok := stringMapFromAny(opts["headers"]); ok {
			ip.Headers = headers
		}
		if models, ok := m["models"].(map[string]any); ok {
			for k := range models {
				ip.Models = append(ip.Models, k)
			}
			sort.Strings(ip.Models)
		}
		out = append(out, ip)
	}
	return out
}

func importProtocolForNPM(npm string) (string, bool) {
	switch strings.TrimSpace(npm) {
	case "@ai-sdk/openai":
		return "openai-responses", true
	case "@ai-sdk/anthropic":
		return "anthropic-messages", true
	case "@ai-sdk/openai-compatible":
		return "openai-compatible", true
	default:
		return "", false
	}
}

func stringMapFromAny(value any) (map[string]string, bool) {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		str, ok := item.(string)
		if !ok {
			continue
		}
		out[key] = str
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// mapsEqualShallow compares two string-keyed maps for structural equality.
func mapsEqualShallow(a, b map[string]any) bool {
	return reflect.DeepEqual(a, b)
}
