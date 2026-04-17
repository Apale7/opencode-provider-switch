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
		Long: `Provider commands manage upstream OpenAI-compatible endpoints stored in the
local olpx config file.

Providers are separate from aliases: a provider defines connection details such
as base URL, API key, and extra headers, while aliases decide failover order by
binding one or more provider/model targets.

Common workflow: add or import providers first, inspect them with provider list,
then bind them to aliases with olpx alias bind.`,
		Example: `  olpx provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  olpx provider import-opencode
  olpx provider list`,
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
		Long: `provider add creates or updates one upstream provider entry in local olpx
config.

It writes only the olpx config file. --base-url must point at an
OpenAI-compatible /v1 root. The command validates that shape, but it does not
contact the upstream or test credentials.

When updating an existing provider, omitted mutable fields keep their current
values: name, api key, headers, and disabled state are preserved unless you
explicitly pass new values. Repeated --header KEY=VALUE entries replace the
stored header map for this command invocation.

Typical next step: run olpx provider list or bind the provider to an alias.`,
		Example: `  olpx provider add --id su8 --base-url https://cn2.su8.codes/v1
  olpx provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  olpx provider add --id relay --base-url https://example.com/v1 --api-key sk-example --header X-Token=abc --header X-Workspace=my-team
  olpx provider add --id su8 --base-url https://new.example.com/v1`,
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
		Long: `provider list prints the providers currently stored in local olpx config.

Output is inspection-oriented: provider ids, enabled state, base URLs, and
redacted API keys are shown so you can confirm what was saved or imported before
binding aliases.

This command does not modify config and does not contact upstream providers.`,
		Example: `  olpx provider list
  olpx --config /path/to/config.json provider list`,
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
		Long: fmt.Sprintf(`provider %s flips one provider's disabled state in local olpx config.

It changes routing eligibility for every alias target that references this
provider, but it does not rewrite alias target enabled flags. This matters when
the same provider is shared across multiple aliases.

This command writes only the olpx config file and does not test upstream
reachability. Typical next step: run olpx doctor to confirm routable aliases.`, use),
		Example: fmt.Sprintf(`  olpx provider %s <id>
  olpx doctor`, use),
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
		Long: `provider remove deletes one provider from local olpx config.

It does not automatically clean alias target references that still point at the
removed provider. If aliases still reference it, doctor will report invalid
config and those aliases will not be routable.

Typical follow-up: inspect aliases, unbind stale targets, then run olpx doctor.`,
		Example: `  olpx provider remove su8
  olpx alias list
  olpx doctor`,
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
		Long: `provider import-opencode reads an OpenCode config file and copies supported
custom providers into local olpx config.

By default it reads the global user OpenCode config resolved in precedence order
opencode.jsonc > opencode.json > config.json under ~/.config/opencode (XDG
aware). It does not follow OPENCODE_CONFIG_DIR for this default source; use
--from when you want a different file.

Only config-defined @ai-sdk/openai custom providers with both baseURL and apiKey
are imported. Unsupported provider shapes are skipped by design. Existing olpx
providers are skipped unless --overwrite is given.

Typical next step: run olpx provider list, then create aliases and bindings.`,
		Example: `  olpx provider import-opencode
  olpx provider import-opencode --from /path/to/opencode.jsonc
  olpx provider import-opencode --overwrite`,
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
