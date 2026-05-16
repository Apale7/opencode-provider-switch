package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
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
	_ = ctx
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return ConfigImportResult{}, fmt.Errorf("config content is required")
	}

	topLevel, err := configTopLevelFields([]byte(content))
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

	cfg, err := s.loadConfig()
	if err != nil {
		return ConfigImportResult{}, err
	}
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
	cfg.Server = imported.Server
	cfg.Admin = imported.Admin
	cfg.Desktop = imported.Desktop
	cfg.Providers = append([]config.Provider(nil), imported.Providers...)
	cfg.Aliases = append([]config.Alias(nil), imported.Aliases...)
	cfg.RequestRewriteRules = append([]config.RequestRewriteRule(nil), imported.RequestRewriteRules...)
	if errs := cfg.Validate(); len(errs) > 0 {
		return ConfigImportResult{}, errs[0]
	}
	if err := cfg.Save(); err != nil {
		return ConfigImportResult{}, err
	}

	result := ConfigImportResult{ConfigPath: cfg.Path()}
	if s.currentProxyStatus(proxyBindAddress(cfg)).Running {
		result.Warnings = append(result.Warnings, "proxy is still running with the previous in-memory config; restart it to apply imported settings")
	}
	return result, nil
}

func marshalConfigContent(cfg *config.Config) (string, error) {
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	aliases := append([]config.Alias(nil), cfg.Aliases...)
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
	rewriteRules := append([]config.RequestRewriteRule(nil), cfg.RequestRewriteRules...)
	snapshot := struct {
		Server              config.Server               `json:"server"`
		Admin               config.Admin                `json:"admin,omitempty"`
		Desktop             config.Desktop              `json:"desktop,omitempty"`
		Providers           []config.Provider           `json:"providers"`
		Aliases             []config.Alias              `json:"aliases"`
		RequestRewriteRules []config.RequestRewriteRule `json:"request_rewrite_rules,omitempty"`
	}{
		Server:              cfg.Server,
		Admin:               cfg.Admin,
		Desktop:             cfg.Desktop,
		Providers:           providers,
		Aliases:             aliases,
		RequestRewriteRules: rewriteRules,
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
	for _, field := range []string{"server", "providers", "aliases"} {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("import requires a full ocswitch config with %q", field)
		}
	}
	return nil
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
