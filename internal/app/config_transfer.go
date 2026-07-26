package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/configstore"
	"github.com/Apale7/opencode-provider-switch/internal/diagnostics"
)

func (s *Service) ExportConfig(ctx context.Context) (ConfigExportView, error) {
	_ = ctx
	cfg, err := s.loadConfig()
	if err != nil {
		return ConfigExportView{}, err
	}
	content, err := marshalConfigContent(cfg)
	if err != nil {
		return ConfigExportView{}, err
	}
	return ConfigExportView{ConfigPath: cfg.Path(), Content: content}, nil
}

func (s *Service) ImportConfig(ctx context.Context, in ConfigImportInput) (ConfigImportResult, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return ConfigImportResult{}, fmt.Errorf("config content is required")
	}

	data := []byte(content)
	if err := validateImportJSON(data); err != nil {
		return ConfigImportResult{}, err
	}
	topLevel, err := configTopLevelFields(data)
	if err != nil {
		return ConfigImportResult{}, err
	}
	if err := validateFullConfigImport(topLevel); err != nil {
		return ConfigImportResult{}, err
	}

	// Decode through the unified schema codec (v1 migrate / v2 strict).
	// Path is only used as an in-memory anchor; import never Save()s this object
	// and never changes disk until commitConfig applies fields onto the live config.
	imported, err := config.LoadFromBytes(s.configPath, data)
	if err != nil {
		return ConfigImportResult{}, fmt.Errorf("parse config: %w", err)
	}
	if err := validateImportedProviderGroupAPIKeys(imported); err != nil {
		return ConfigImportResult{}, err
	}

	var result ConfigImportResult
	_, err = s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
		// Preserve local-only / optional sections when the payload omits them.
		if _, ok := topLevel["admin"]; !ok {
			imported.Admin = cfg.Admin
		}
		if imported.Admin.APIKey == "" {
			imported.Admin.APIKey = cfg.Admin.APIKey
		}
		if _, ok := topLevel["desktop"]; !ok {
			imported.Desktop = cfg.Desktop
		}
		if _, ok := topLevel["request_rewrite_rules"]; !ok {
			imported.RequestRewriteRules = cfg.RequestRewriteRulesSnapshot()
		}
		if _, ok := topLevel["provider_priority"]; !ok {
			imported.ProviderPriority = append([]string(nil), cfg.ProviderPriority...)
		}
		if _, ok := topLevel["auto_alias_enabled"]; !ok {
			imported.AutoAliasEnabled = cfg.AutoAliasEnabled
		}
		// Apply imported fields onto the live config so path / store identity stay on cfg.
		cfg.Server = imported.Server
		cfg.Admin = imported.Admin
		cfg.Desktop = imported.Desktop
		cfg.Providers = append([]config.Provider(nil), imported.Providers...)
		cfg.Aliases = append([]config.Alias(nil), imported.Aliases...)
		cfg.RequestRewriteRules = append([]config.RequestRewriteRule(nil), imported.RequestRewriteRules...)
		cfg.ProviderPriority = append([]string(nil), imported.ProviderPriority...)
		cfg.AutoAliasEnabled = imported.AutoAliasEnabled
		cfg.SchemaVersion = config.CurrentSchemaVersion
		issues, err := diagnostics.ScanConfig(cfg, diagnostics.ScanOptions{})
		if err != nil {
			return configstore.Mutation[*config.Config]{}, fmt.Errorf("scan imported config: %w", err)
		}
		for _, issue := range issues {
			if isIdentityAmbiguity(issue.Code) {
				return configstore.Mutation[*config.Config]{}, fmt.Errorf("import candidate has ambiguous identity at %s (%s)", issue.Path, issue.Code)
			}
		}
		if errs := cfg.ValidateForPersist(); len(errs) > 0 {
			return configstore.Mutation[*config.Config]{}, errs[0]
		}
		result = ConfigImportResult{ConfigPath: cfg.Path()}
		if s.currentProxyStatus(proxyBindAddress(cfg)).Running {
			result.Warnings = append(result.Warnings, "proxy is still running with the previous in-memory config; restart it to apply imported settings")
		}
		return configstore.Mutation[*config.Config]{Value: cfg, Changed: true}, nil
	})
	if err != nil {
		return ConfigImportResult{}, err
	}
	return result, nil
}

func validateImportedProviderGroupAPIKeys(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	for _, provider := range cfg.Providers {
		for _, group := range provider.Groups {
			for _, key := range group.EffectiveAPIKeys() {
				if isMaskedAPIKeyPlaceholder(key) {
					return fmt.Errorf("provider %q group %q contains a masked API key placeholder", provider.ID, group.ID)
				}
			}
		}
	}
	return nil
}

func isIdentityAmbiguity(code diagnostics.Code) bool {
	switch code {
	case diagnostics.CodeProviderIdentityAmbiguous,
		diagnostics.CodeAliasIdentityAmbiguous,
		diagnostics.CodeAliasTargetIdentityAmbiguous,
		diagnostics.CodeRewriteIdentityAmbiguous:
		return true
	default:
		return false
	}
}

// marshalConfigContent encodes the live config as canonical schema v2 JSON.
// Prefer Config.MarshalPersistent so export matches Save field set and schema_version.
func marshalConfigContent(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("marshal config: nil config")
	}
	data, err := cfg.MarshalPersistent()
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(data), nil
}

func configTopLevelFields(data []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return raw, nil
}

func validateFullConfigImport(raw map[string]json.RawMessage) error {
	var unknownFields []string
	var nullFields []string
	for field, value := range raw {
		switch field {
		case "schema_version", "server", "admin", "desktop", "providers", "aliases",
			"request_rewrite_rules", "provider_priority", "auto_alias_enabled":
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				nullFields = append(nullFields, field)
			}
		default:
			unknownFields = append(unknownFields, field)
		}
	}
	sort.Strings(unknownFields)
	if len(unknownFields) > 0 {
		// Keep the stable "unknown field" substring for wire/compat tests and clients.
		return fmt.Errorf("import contains unknown field %q", unknownFields[0])
	}
	sort.Strings(nullFields)
	if len(nullFields) > 0 {
		return fmt.Errorf("import top-level field %q must not be null", nullFields[0])
	}
	for _, field := range []string{"server", "providers", "aliases"} {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("import requires a full ocswitch config with %q", field)
		}
	}
	return nil
}

func validateImportJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := validateJSONValue(decoder, "/config"); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse config: unexpected trailing token %v", token)
		}
		return fmt.Errorf("parse config: %w", err)
	}

	// Identity ambiguity is a stable import contract and must outrank later
	// schema codec rejection (e.g. mixed/legacy fields) so error priority stays stable.
	if err := rejectImportIdentityAmbiguity(data); err != nil {
		return err
	}

	// Unified schema codec: supports v1 migrate and v2 strict (incl. mixed fail-closed).
	// Use a non-disk validation path so this precheck never implies a write target.
	if _, err := config.LoadFromBytes("import-validation", data); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return nil
}

// rejectImportIdentityAmbiguity is a read-only raw-JSON precheck at the import
// boundary. It never mutates runtime config and never writes disk. It only
// surfaces the same identity codes used by diagnostics.ScanConfig so error
// priority stays stable even when schema decode rejects mixed/legacy fields
// before ScanConfig can run.
func rejectImportIdentityAmbiguity(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}

	if raw, ok := root["providers"]; ok {
		var items []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &items); err == nil {
			if path, found := firstDuplicateIdentityPath(len(items), func(i int) string { return items[i].ID }, "/config/providers"); found {
				return ambiguousImportIdentityError(path, diagnostics.CodeProviderIdentityAmbiguous)
			}
		}
	}

	if raw, ok := root["aliases"]; ok {
		var items []struct {
			Alias   string `json:"alias"`
			Targets []struct {
				Provider string `json:"provider"`
				Group    string `json:"group"`
				Model    string `json:"model"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(raw, &items); err == nil {
			for ai, alias := range items {
				if path, found := firstDuplicateIdentityPath(len(alias.Targets), func(ti int) string {
					return alias.Targets[ti].Provider + "\x00" + effectiveImportGroupID(alias.Targets[ti].Group) + "\x00" + alias.Targets[ti].Model
				}, fmt.Sprintf("/config/aliases/%d/targets", ai)); found {
					return ambiguousImportIdentityError(path, diagnostics.CodeAliasTargetIdentityAmbiguous)
				}
			}
			if path, found := firstDuplicateIdentityPath(len(items), func(i int) string { return items[i].Alias }, "/config/aliases"); found {
				return ambiguousImportIdentityError(path, diagnostics.CodeAliasIdentityAmbiguous)
			}
		}
	}

	if raw, ok := root["request_rewrite_rules"]; ok {
		var items []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &items); err == nil {
			if path, found := firstDuplicateIdentityPath(len(items), func(i int) string { return items[i].Name }, "/config/request_rewrite_rules"); found {
				return ambiguousImportIdentityError(path, diagnostics.CodeRewriteIdentityAmbiguous)
			}
		}
	}

	return nil
}

// effectiveImportGroupID matches diagnostics identity: missing/blank group maps to default.
func effectiveImportGroupID(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return config.DefaultGroupID
	}
	return group
}

func firstDuplicateIdentityPath(n int, keyAt func(int) string, root string) (string, bool) {
	indexes := make(map[string][]int, n)
	for i := 0; i < n; i++ {
		key := keyAt(i)
		indexes[key] = append(indexes[key], i)
	}
	// Stable report: first array index that participates in a duplicate identity.
	for i := 0; i < n; i++ {
		key := keyAt(i)
		if len(indexes[key]) > 1 {
			return fmt.Sprintf("%s/%d", root, i), true
		}
	}
	return "", false
}

func ambiguousImportIdentityError(path string, code diagnostics.Code) error {
	return fmt.Errorf("import candidate has ambiguous identity at %s (%s)", path, code)
}

func validateJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("field %q must not be null", path)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key at %q is not a string", path)
			}
			childPath := path + "/" + escapeJSONPointerToken(key)
			if seen[key] {
				return fmt.Errorf("duplicate field %q", childPath)
			}
			seen[key] = true
			if err := validateJSONValue(decoder, childPath); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := validateJSONValue(decoder, fmt.Sprintf("%s/%d", path, index)); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter %q at %q", delim, path)
	}
}

func escapeJSONPointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
