package cli

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
)

func newProviderCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "provider",
		Short: "Manage upstream providers",
		Long: `Provider commands manage upstream OpenAI-compatible endpoints stored in the
local ocswitch config file.

Providers are separate from aliases: a provider defines connection details such
as base URL, API key, and extra headers, while aliases decide failover order by
binding one or more provider/model targets.

Common workflow: add or import providers first, inspect them with provider list,
then bind them to aliases with ocswitch alias bind.`,
		Example: `  ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  ocswitch provider import-opencode
  ocswitch provider list`,
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
	var clearHeaders bool
	var disabled bool
	var skipModels bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update an upstream provider",
		Long: `provider add creates or updates one upstream provider entry in local ocswitch
config.

It writes only the ocswitch config file. --base-url must point at an
OpenAI-compatible /v1 root. By default the command also calls the upstream
/v1/models endpoint with the supplied credentials and stores the discovered
model list so later bind operations can catch typos early. Discovery failures
only emit warnings and do not block saving connection settings. Use
--skip-models when the upstream blocks model discovery or you only want to save
connection settings.

When updating an existing provider, omitted mutable fields keep their current
values: name, api key, headers, and disabled state are preserved unless the
corresponding flag is explicitly passed. Use --clear-headers to remove all saved
extra headers before storing the updated provider. Discovered model catalogs are refreshed when
possible. If connection details changed but discovery was skipped or failed, any
existing model catalog is kept only as untrusted metadata so later validation no
longer relies on stale entries. Repeated --header KEY=VALUE entries replace the
stored header map for this command invocation.

Typical next step: run ocswitch provider list or bind the provider to an alias.`,
		Example: `  ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1
  ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  ocswitch provider add --id relay --base-url https://example.com/v1 --api-key sk-example --header X-Token=abc --header X-Workspace=my-team
  ocswitch provider add --id relay --base-url https://example.com/v1 --skip-models
  ocswitch provider add --id su8 --base-url https://new.example.com/v1`,
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
			apiKeyChanged := cmd.Flags().Changed("api-key")
			headersChanged := cmd.Flags().Changed("header")
			clearHeadersRequested := cmd.Flags().Changed("clear-headers") && clearHeaders
			disabledChanged := cmd.Flags().Changed("disabled")
			var hdrs map[string]string
			for _, h := range headers {
				k, v, ok := strings.Cut(h, "=")
				if !ok {
					return fmt.Errorf("invalid --header %q (want KEY=VALUE)", h)
				}
				key := strings.ToLower(strings.TrimSpace(k))
				if key == "" {
					return fmt.Errorf("invalid --header %q (header name must not be empty)", h)
				}
				if hdrs == nil {
					hdrs = make(map[string]string, len(headers))
				}
				hdrs[key] = strings.TrimSpace(v)
			}
			p := config.Provider{
				ID:       id,
				Name:     name,
				BaseURL:  config.NormalizeProviderBaseURL(baseURL),
				APIKey:   apiKey,
				Headers:  normalizeProviderHeaders(hdrs),
				Disabled: disabled,
			}
			connectionChanged := false
			if existing := cfg.FindProvider(id); existing != nil {
				if p.Name == "" {
					p.Name = existing.Name
				}
				if !apiKeyChanged {
					p.APIKey = existing.APIKey
				}
				if !headersChanged && !clearHeadersRequested && len(existing.Headers) > 0 {
					p.Headers = cloneHeaders(existing.Headers)
				}
				if !disabledChanged {
					p.Disabled = existing.Disabled
				}
				p.Models = append([]string(nil), existing.Models...)
				p.ModelsSource = existing.ModelsSource
				connectionChanged = !providerConnectionEqual(*existing, p)
			}
			if !skipModels {
				models, err := opencode.FetchProviderModels(p.BaseURL, p.APIKey, p.Headers)
				if err != nil {
					if connectionChanged {
						p.Models = append([]string(nil), p.Models...)
						p.ModelsSource = ""
						fmt.Fprintln(cmd.ErrOrStderr(), "warning: provider connection changed and model discovery failed; keeping existing model catalog as untrusted")
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not discover provider models: %v\n", err)
				} else if normalized := config.NormalizeProviderModels(models); len(normalized) > 0 {
					p.Models = normalized
					p.ModelsSource = "discovered"
				} else {
					if connectionChanged {
						p.Models = append([]string(nil), p.Models...)
						p.ModelsSource = ""
						fmt.Fprintln(cmd.ErrOrStderr(), "warning: provider connection changed and model discovery returned no models; keeping existing model catalog as untrusted")
					} else {
						fmt.Fprintln(cmd.ErrOrStderr(), "warning: provider model discovery returned no models; keeping existing model catalog")
					}
				}
			} else if connectionChanged {
				p.Models = append([]string(nil), p.Models...)
				p.ModelsSource = ""
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: provider connection changed with --skip-models; keeping existing model catalog as untrusted")
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
			if !skipModels && p.ModelsSource == "discovered" {
				fmt.Fprintf(cmd.OutOrStdout(), "  discovered %d model(s)\n", len(p.Models))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "provider id (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL, including /v1 (required)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "upstream API key")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "extra header KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&clearHeaders, "clear-headers", false, "remove all saved extra headers before applying updates")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save provider in disabled state")
	cmd.Flags().BoolVar(&skipModels, "skip-models", false, "skip provider /v1/models discovery")
	if err := cmd.Flags().SetAnnotation("skip-models", cobra.BashCompOneRequiredFlag, []string{"false"}); err != nil {
		panic(err)
	}
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
		Long: `provider list prints the providers currently stored in local ocswitch config.

Output is inspection-oriented: provider ids, enabled state, base URLs, and
redacted API keys are shown so you can confirm what was saved or imported before
binding aliases.

This command does not modify config and does not contact upstream providers.`,
		Example: `  ocswitch provider list
  ocswitch --config /path/to/config.json provider list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providers, err := appService().ListProviders(cmd.Context())
			if err != nil {
				return err
			}
			if len(providers) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no providers)")
				return nil
			}
			for _, p := range providers {
				key := "(none)"
				if p.APIKeySet {
					key = p.APIKeyMasked
				}
				state := "enabled"
				if p.Disabled {
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
		Long: fmt.Sprintf(`provider %s flips one provider's disabled state in local ocswitch config.

It changes routing eligibility for every alias target that references this
provider, but it does not rewrite alias target enabled flags. This matters when
the same provider is shared across multiple aliases.

This command writes only the ocswitch config file and does not test upstream
reachability. Typical next step: run ocswitch doctor to confirm routable aliases.`, use),
		Example: fmt.Sprintf(`  ocswitch provider %s <id>
  ocswitch doctor`, use),
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
		Long: `provider remove deletes one provider from local ocswitch config.

It does not automatically clean alias target references that still point at the
removed provider. If aliases still reference it, doctor will report invalid
config and those aliases will not be routable.

Typical follow-up: inspect aliases, unbind stale targets, then run ocswitch doctor.`,
		Example: `  ocswitch provider remove su8
  ocswitch alias list
  ocswitch doctor`,
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
custom providers into local ocswitch config.

By default it reads the global user OpenCode config resolved in precedence order
opencode.jsonc > opencode.json > config.json under ~/.config/opencode (XDG
aware). It does not follow OPENCODE_CONFIG_DIR for this default source; use
--from when you want a different file.

Only config-defined @ai-sdk/openai custom providers with baseURL are imported.
Imported baseURL values must still satisfy the local /v1 requirement. Providers
with an empty apiKey are allowed and kept as-is. Unsupported provider shapes are
skipped by design. Existing ocswitch providers are skipped unless --overwrite is
given.
Typical next step: run ocswitch provider list, then create aliases and bindings.`,
		Example: `  ocswitch provider import-opencode
  ocswitch provider import-opencode --from /path/to/opencode.jsonc
  ocswitch provider import-opencode --overwrite`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromChanged := cmd.Flags().Changed("from")
			if srcPath == "" {
				p, existed := opencode.ResolveGlobalConfigPath()
				if !existed {
					return fmt.Errorf("no OpenCode config found at %s; use --from to specify", p)
				}
				srcPath = p
			} else if fromChanged {
				if _, err := os.Stat(srcPath); err != nil {
					return fmt.Errorf("read %s: %w", srcPath, err)
				}
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
				baseURL := config.NormalizeProviderBaseURL(ip.BaseURL)
				if err := config.ValidateProviderBaseURL(baseURL); err != nil {
					skipped++
					fmt.Fprintf(cmd.OutOrStdout(), "skip %q (invalid baseURL %q: %v)\n", ip.ID, ip.BaseURL, err)
					continue
				}
				existing := cfg.FindProvider(ip.ID)
				merged := mergeImportedProvider(existing, opencode.ImportableProvider{
					ID:      ip.ID,
					Name:    ip.Name,
					BaseURL: baseURL,
					APIKey:  ip.APIKey,
					Models:  ip.Models,
				})
				cfg.UpsertProvider(merged)
				imported++
				fmt.Fprintf(cmd.OutOrStdout(), "import %q → %s (models: %s)\n", ip.ID, baseURL, strings.Join(merged.Models, ","))
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

func normalizeProviderHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerConnectionEqual(a, b config.Provider) bool {
	return config.NormalizeProviderBaseURL(a.BaseURL) == config.NormalizeProviderBaseURL(b.BaseURL) &&
		a.APIKey == b.APIKey &&
		reflect.DeepEqual(normalizeProviderHeaders(a.Headers), normalizeProviderHeaders(b.Headers))
}

func providerCatalogEndpointEqual(a, b config.Provider) bool {
	return config.NormalizeProviderBaseURL(a.BaseURL) == config.NormalizeProviderBaseURL(b.BaseURL) &&
		a.APIKey == b.APIKey &&
		reflect.DeepEqual(normalizeProviderHeaders(a.Headers), normalizeProviderHeaders(b.Headers))
}

func mergeImportedProvider(existing *config.Provider, ip opencode.ImportableProvider) config.Provider {
	importedModels := config.NormalizeProviderModels(ip.Models)
	merged := config.Provider{
		ID:           ip.ID,
		Name:         ip.Name,
		BaseURL:      config.NormalizeProviderBaseURL(ip.BaseURL),
		APIKey:       ip.APIKey,
		Models:       importedModels,
		ModelsSource: "imported",
	}
	if len(importedModels) == 0 {
		merged.ModelsSource = ""
	}
	if existing == nil {
		return merged
	}
	merged.Headers = cloneHeaders(existing.Headers)
	merged.Disabled = existing.Disabled
	if merged.Name == "" {
		merged.Name = existing.Name
	}
	if existing.ModelsSource == "discovered" {
		prospective := merged
		prospective.Headers = cloneHeaders(existing.Headers)
		prospective.Disabled = existing.Disabled
		if providerCatalogEndpointEqual(*existing, prospective) {
			merged.Models = append([]string(nil), existing.Models...)
			merged.ModelsSource = existing.ModelsSource
			return merged
		}
		if len(importedModels) == 0 {
			merged.Models = append([]string(nil), existing.Models...)
			merged.ModelsSource = ""
			return merged
		}
	}
	if len(importedModels) == 0 {
		merged.Models = nil
		merged.ModelsSource = ""
	}
	return merged
}

// maskKey redacts middle characters of an API key for display.
func maskKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "…" + k[len(k)-4:]
}
