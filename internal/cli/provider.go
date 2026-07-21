package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func newProviderCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "provider",
		Short: "Manage upstream providers",
		Long: `Provider commands manage upstream protocol-aware endpoints stored in the
local ocswitch config file.

Providers are separate from aliases: a provider defines connection details such
as base URL, API key, and extra headers, while aliases decide failover order by
binding one or more provider/model targets.

Common workflow: add or import providers first, inspect them with provider list,
then bind them to aliases with ocswitch alias bind.`,
		Example: `  ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  ocswitch provider add --id claude --protocol anthropic-messages --base-url https://api.anthropic.com/v1 --api-key sk-ant-example
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
	var protocol string
	var headers []string
	var clearHeaders bool
	var disabled bool
	var skipModels bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update an upstream provider",
		Long: `provider add creates or updates one upstream provider entry in local ocswitch
config.

It writes only the ocswitch config file via the shared Service/ConfigStore path.
--protocol defaults to openai-responses. By default the command also calls the
upstream
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
  ocswitch provider add --id claude --protocol anthropic-messages --base-url https://api.anthropic.com/v1 --api-key sk-ant-example
  ocswitch provider add --id relay --base-url https://example.com/v1 --api-key sk-example --header X-Token=abc --header X-Workspace=my-team
  ocswitch provider add --id relay --base-url https://example.com/v1 --skip-models
  ocswitch provider add --id su8 --base-url https://new.example.com/v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" || baseURL == "" {
				return fmt.Errorf("--id and --base-url are required")
			}
			protocol = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
			if err := config.ValidateProviderBaseURL(protocol, baseURL); err != nil {
				return fmt.Errorf("invalid --base-url: %w", err)
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

			// Load existing only to resolve omit-semantics for dry-run messaging and flag defaults.
			existingProviders, err := appService().ListProviders(cmd.Context())
			if err != nil {
				return err
			}
			var existing *app.ProviderView
			for i := range existingProviders {
				if existingProviders[i].ID == id {
					existing = &existingProviders[i]
					break
				}
			}

			in := app.ProviderUpsertInput{
				ID:           id,
				Name:         name,
				Protocol:     protocol,
				BaseURL:      config.NormalizeProviderBaseURL(baseURL),
				SkipModels:   skipModels,
				ClearHeaders: clearHeadersRequested,
			}
			if apiKeyChanged {
				in.APIKey = apiKey
				if strings.TrimSpace(apiKey) == "" {
					in.ClearAPIKeys = true
				}
			}
			if headersChanged || clearHeadersRequested {
				in.Headers = normalizeProviderHeaders(hdrs)
			}
			if disabledChanged {
				in.Disabled = disabled
			} else if existing != nil {
				in.Disabled = existing.Disabled
			}

			if dryRun {
				action := "create"
				if existing != nil {
					action = "update"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would %s provider %q via Service/ConfigStore (skip-models=%v)\n", action, id, skipModels)
				return nil
			}

			result, err := appService().UpsertProvider(cmd.Context(), in)
			if err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", rewriteProviderWarning(warning))
			}
			state := "enabled"
			if result.Provider.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved provider %q [%s] %s → %s\n", result.Provider.ID, state, result.Provider.Protocol, result.Provider.BaseURL)
			if !skipModels && result.Provider.ModelsSource == "discovered" {
				fmt.Fprintf(cmd.OutOrStdout(), "  discovered %d model(s)\n", len(result.Provider.Models))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "provider id (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&protocol, "protocol", config.ProtocolOpenAIResponses, "provider protocol")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL, including /v1 (required)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "upstream API key")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "extra header KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&clearHeaders, "clear-headers", false, "remove all saved extra headers before applying updates")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save provider in disabled state")
	cmd.Flags().BoolVar(&skipModels, "skip-models", false, "skip provider /v1/models discovery")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	if err := cmd.Flags().SetAnnotation("skip-models", cobra.BashCompOneRequiredFlag, []string{"false"}); err != nil {
		panic(err)
	}
	return cmd
}

func newProviderListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured providers",
		Long: `provider list prints the providers currently stored in local ocswitch config.

Output is inspection-oriented: provider ids, protocol, enabled state, base URLs, and
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
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s [%s] %-18s %s  apiKey=%s\n", p.ID, state, p.Protocol, p.BaseURL, key)
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
	var dryRun bool
	cmd := &cobra.Command{
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
			id := strings.TrimSpace(args[0])
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would set provider %q disabled=%v via Service/ConfigStore\n", id, disabled)
				return nil
			}
			if _, err := appService().SetProviderDisabled(cmd.Context(), app.ProviderStateInput{ID: id, Disabled: disabled}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s provider %q\n", action, id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newProviderRemoveCmd() *cobra.Command {
	var dryRun bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Remove a provider through the lifecycle planner",
		Long: `provider remove deletes one provider from local ocswitch config via Service.

Protected manual/locked alias targets block removal until resolved through the
lifecycle preview/execute flow. Unlocked auto targets and priority entries are
cleaned automatically.

Use --dry-run to print the impact plan without persisting. Non-interactive
shells require --yes to execute an executable plan. Use --json for a single
envelope on stdout.`,
		Example: `  ocswitch provider remove su8 --dry-run
  ocswitch provider remove su8 --yes
  ocswitch provider remove su8 --json --yes
  ocswitch doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			rev, plan, err := previewRemoveProvider(cmd, id)
			if err != nil {
				return finishOutcome(cmd, err, nil)
			}
			payload, _ := marshalPayload(lifecycle.ProviderRemovePayload{ProviderID: id})
			op := lifecycle.Operation{Kind: lifecycle.OpProviderRemove, Payload: payload}

			if jsonOutput && dryRun {
				_, env := app.ClassifyOutcome(nil, plan)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			if !jsonOutput {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would remove provider %q\n", id)
				}
				printPlanHuman(cmd.OutOrStdout(), plan)
			}
			if dryRun {
				return nil
			}
			if !plan.Executable || strings.TrimSpace(plan.PlanToken) == "" {
				return finishOutcome(cmd, &app.OutcomeError{
					Code: "plan_not_executable",
					Params: map[string]any{
						"operationKind": lifecycle.OpProviderRemove,
						"providerId":    id,
						"blockerCount":  len(plan.Blockers),
						"choiceCount":   len(plan.Choices),
					},
				}, plan)
			}
			ok, cerr := confirmExecute(cmd, yes)
			if cerr != nil {
				return finishOutcome(cmd, cerr, plan)
			}
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "aborted")
				return nil
			}
			result, err := executeLifecyclePlan(cmd, rev, plan, op, nil)
			if err != nil {
				return finishOutcome(cmd, err, result)
			}
			if jsonOutput {
				_, env := app.ClassifyOutcome(nil, result)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed provider %q\n", id)
			printExecuteHuman(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview impact plan without persisting")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute without interactive confirmation")
	return cmd
}

func newProviderImportCmd() *cobra.Command {
	var srcPath string
	var overwrite bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import-opencode",
		Short: "Import supported custom providers from OpenCode",
		Long: `provider import-opencode reads an OpenCode config file and copies supported
custom providers into local ocswitch config through Service/ConfigStore.

By default it reads the global user OpenCode config. Use --from when you want a
different file. Existing ocswitch providers are skipped unless --overwrite is
given.`,
		Example: `  ocswitch provider import-opencode
  ocswitch provider import-opencode --from /path/to/opencode.jsonc
  ocswitch provider import-opencode --overwrite`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromChanged := cmd.Flags().Changed("from")
			if srcPath != "" && fromChanged {
				if _, err := os.Stat(srcPath); err != nil {
					return fmt.Errorf("read %s: %w", srcPath, err)
				}
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would import OpenCode providers from %q overwrite=%v via Service/ConfigStore\n", srcPath, overwrite)
				return nil
			}
			result, err := appService().ImportProviders(cmd.Context(), app.ProviderImportInput{
				SourcePath: srcPath,
				Overwrite:  overwrite,
			})
			if err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				// Keep legacy "skip ..." lines on stdout for existing CLI tests/scripts.
				if strings.HasPrefix(warning, "skip ") {
					// Service uses "enable overwrite"; CLI historically said "use --overwrite".
					line := strings.ReplaceAll(warning, "enable overwrite to replace it", "use --overwrite")
					fmt.Fprintln(cmd.OutOrStdout(), line)
					continue
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}
			if result.Imported == 0 && result.Skipped == 0 && len(result.Warnings) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no importable supported providers found in %s\n", result.SourcePath)
				return nil
			}
			for _, p := range result.Providers {
				fmt.Fprintf(cmd.OutOrStdout(), "import %q [%s] → %s (models: %s)\n", p.ID, p.Protocol, p.BaseURL, strings.Join(p.Models, ","))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported=%d skipped=%d\n", result.Imported, result.Skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&srcPath, "from", "", "OpenCode config to read (default: global user config)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing provider entries")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
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

func rewriteProviderWarning(warning string) string {
	// Keep CLI phrasing stable for existing scripts/tests.
	replacer := strings.NewReplacer(
		"skip models enabled", "--skip-models",
		"provider connection changed with skip models enabled", "provider connection changed with --skip-models",
	)
	return replacer.Replace(warning)
}
