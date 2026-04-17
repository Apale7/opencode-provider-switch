package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/config"
	"github.com/anomalyco/opencode-provider-switch/internal/opencode"
)

func newProviderCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "provider",
		Short: "Manage upstream providers",
	}
	c.AddCommand(
		newProviderAddCmd(),
		newProviderListCmd(),
		newProviderEnableCmd(),
		newProviderDisableCmd(),
		newProviderRemoveCmd(),
		newProviderImportCmd(),
	)
	return c
}

func newProviderAddCmd() *cobra.Command {
	var id, name, baseURL, apiKey string
	var headers []string
	var disabled bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update an upstream provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" || baseURL == "" {
				return fmt.Errorf("--id and --base-url are required")
			}
			if err := config.ValidateProviderBaseURL(baseURL); err != nil {
				return fmt.Errorf("invalid --base-url: %w", err)
			}
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			hdrs := map[string]string{}
			for _, h := range headers {
				k, v, ok := strings.Cut(h, "=")
				if !ok {
					return fmt.Errorf("invalid --header %q (want KEY=VALUE)", h)
				}
				hdrs[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
			p := config.Provider{
				ID:       id,
				Name:     name,
				BaseURL:  baseURL,
				APIKey:   apiKey,
				Headers:  hdrs,
				Disabled: disabled,
			}
			if existing := cfg.FindProvider(id); existing != nil {
				if p.Name == "" {
					p.Name = existing.Name
				}
				if p.APIKey == "" {
					p.APIKey = existing.APIKey
				}
				if len(headers) == 0 && len(existing.Headers) > 0 {
					p.Headers = cloneHeaders(existing.Headers)
				}
				if !disabled {
					p.Disabled = existing.Disabled
				}
			}
			cfg.UpsertProvider(p)
			if err := cfg.Save(); err != nil {
				return err
			}
			state := "enabled"
			if p.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved provider %q [%s] → %s\n", id, state, baseURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "provider id (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL, including /v1 (required)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "upstream API key")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "extra header KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save provider in disabled state")
	return cmd
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func newProviderListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			providers := append([]config.Provider(nil), cfg.Providers...)
			sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
			if len(providers) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no providers)")
				return nil
			}
			for _, p := range providers {
				key := "(none)"
				if p.APIKey != "" {
					key = maskKey(p.APIKey)
				}
				state := "enabled"
				if !p.IsEnabled() {
					state = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s [%s] %s  apiKey=%s\n", p.ID, state, p.BaseURL, key)
			}
			return nil
		},
	}
}

func newProviderEnableCmd() *cobra.Command {
	return newProviderStateCmd("enable", false)
}

func newProviderDisableCmd() *cobra.Command {
	return newProviderStateCmd("disable", true)
}

func newProviderStateCmd(use string, disabled bool) *cobra.Command {
	action := "enabled"
	if disabled {
		action = "disabled"
	}
	return &cobra.Command{
		Use:   use + " <id>",
		Args:  cobra.ExactArgs(1),
		Short: strings.Title(action[:len(action)-1]) + " a provider without changing alias target state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			existing := cfg.FindProvider(args[0])
			if existing == nil {
				return fmt.Errorf("provider %q not found", args[0])
			}
			updated := *existing
			updated.Disabled = disabled
			cfg.UpsertProvider(updated)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s provider %q\n", action, args[0])
			return nil
		},
	}
}

func newProviderRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Remove a provider (targets referencing it must be removed first or will fail doctor)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if !cfg.RemoveProvider(args[0]) {
				return fmt.Errorf("provider %q not found", args[0])
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed provider %q\n", args[0])
			return nil
		},
	}
}

func newProviderImportCmd() *cobra.Command {
	var srcPath string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "import-opencode",
		Short: "Import @ai-sdk/openai custom providers from an OpenCode config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcPath == "" {
				p, existed := opencode.ResolveGlobalConfigPath()
				if !existed {
					return fmt.Errorf("no OpenCode config found at %s; use --from to specify", p)
				}
				srcPath = p
			}
			raw, err := opencode.Load(srcPath)
			if err != nil {
				return err
			}
			imports := opencode.ImportCustomProviders(raw)
			if len(imports) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no importable @ai-sdk/openai providers found in %s\n", srcPath)
				return nil
			}
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			imported := 0
			skipped := 0
			for _, ip := range imports {
				if !overwrite && cfg.FindProvider(ip.ID) != nil {
					skipped++
					fmt.Fprintf(cmd.OutOrStdout(), "skip %q (already exists, use --overwrite)\n", ip.ID)
					continue
				}
				cfg.UpsertProvider(config.Provider{
					ID:       ip.ID,
					Name:     ip.Name,
					BaseURL:  ip.BaseURL,
					APIKey:   ip.APIKey,
					Disabled: false,
				})
				imported++
				fmt.Fprintf(cmd.OutOrStdout(), "import %q → %s (models: %s)\n", ip.ID, ip.BaseURL, strings.Join(ip.Models, ","))
			}
			if imported > 0 {
				if err := cfg.Save(); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported=%d skipped=%d\n", imported, skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&srcPath, "from", "", "OpenCode config to read (default: global user config)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing provider entries")
	return cmd
}

// maskKey redacts middle characters of an API key for display.
func maskKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "…" + k[len(k)-4:]
}
