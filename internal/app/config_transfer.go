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
	serverAPIKeyExplicit := configContentHasExplicitServerAPIKey([]byte(content))
	imported := config.Default()
	if err := json.Unmarshal([]byte(content), imported); err != nil {
		return ConfigImportResult{}, fmt.Errorf("parse config: %w", err)
	}
	if imported.Server.Host == "" {
		imported.Server.Host = "127.0.0.1"
	}
	if imported.Server.Port == 0 {
		imported.Server.Port = 9982
	}
	if imported.Server.APIKey == "" && !serverAPIKeyExplicit {
		imported.Server.APIKey = config.DefaultLocalAPIKey
	}
	if imported.Admin.Host == "" {
		imported.Admin.Host = "127.0.0.1"
	}
	if imported.Admin.Port == 0 {
		imported.Admin.Port = 9983
	}

	var result ConfigImportResult
	_, err = s.commitConfig(ctx, "", func(_ context.Context, cfg *config.Config) (configstore.Mutation[*config.Config], error) {
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
		cfg.Server = imported.Server
		cfg.Admin = imported.Admin
		cfg.Desktop = imported.Desktop
		cfg.Providers = append([]config.Provider(nil), imported.Providers...)
		cfg.Aliases = append([]config.Alias(nil), imported.Aliases...)
		cfg.RequestRewriteRules = append([]config.RequestRewriteRule(nil), imported.RequestRewriteRules...)
		cfg.ProviderPriority = append([]string(nil), imported.ProviderPriority...)
		cfg.AutoAliasEnabled = imported.AutoAliasEnabled
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

func marshalConfigContent(cfg *config.Config) (string, error) {
	providers := append([]config.Provider{}, cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	aliases := append([]config.Alias{}, cfg.Aliases...)
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
	rewriteRules := append([]config.RequestRewriteRule{}, cfg.RequestRewriteRules...)
	snapshot := struct {
		Server              config.Server               `json:"server"`
		Admin               config.Admin                `json:"admin"`
		Desktop             config.Desktop              `json:"desktop"`
		Providers           []config.Provider           `json:"providers"`
		Aliases             []config.Alias              `json:"aliases"`
		RequestRewriteRules []config.RequestRewriteRule `json:"request_rewrite_rules"`
		ProviderPriority    []string                    `json:"provider_priority"`
		AutoAliasEnabled    bool                        `json:"auto_alias_enabled"`
	}{
		Server:              cfg.Server,
		Admin:               cfg.Admin,
		Desktop:             cfg.Desktop,
		Providers:           providers,
		Aliases:             aliases,
		RequestRewriteRules: rewriteRules,
		ProviderPriority:    append([]string{}, cfg.ProviderPriority...),
		AutoAliasEnabled:    cfg.AutoAliasEnabled,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(append(data, '\n')), nil
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
		case "server", "admin", "desktop", "providers", "aliases",
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
		return fmt.Errorf("import contains unknown top-level field %q", unknownFields[0])
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

	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	var document config.Config
	if err := strict.Decode(&document); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return nil
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

func configContentHasExplicitServerAPIKey(data []byte) bool {
	var raw struct {
		Server *struct {
			APIKey *string `json:"api_key"`
		} `json:"server"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	return raw.Server != nil && raw.Server.APIKey != nil
}
