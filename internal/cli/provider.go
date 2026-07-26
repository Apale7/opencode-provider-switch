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
		newProviderGroupCmd(),
	)
	return c
}

func newProviderGroupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "group",
		Short: "Manage provider groups (protocol, keys, models)",
		Long: `provider group commands manage groups under one provider.

Each group has its own protocol, API keys, and model catalog. Shared connection
settings (base URL, headers) stay on the provider. Group identity is always
explicit: create/update/delete/refresh-models/ping require a concrete group id.

Legacy provider commands that omit --group only map to the default group.`,
		Example: `  ocswitch provider group list --provider su8
  ocswitch provider group create --provider su8 --id premium --protocol openai-responses --api-key sk-premium
  ocswitch provider group refresh-models --provider su8 --group premium
  ocswitch provider group ping --provider su8 --group premium`,
	}
	c.AddCommand(
		newProviderGroupListCmd(),
		newProviderGroupCreateCmd(),
		newProviderGroupUpdateCmd(),
		newProviderGroupDeleteCmd(),
		newProviderGroupRefreshModelsCmd(),
		newProviderGroupPingCmd(),
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
				BaseURL:      config.NormalizeProviderBaseURL(baseURL),
				ClearHeaders: clearHeadersRequested,
			}
			if headersChanged || clearHeadersRequested {
				in.Headers = normalizeProviderHeaders(hdrs)
			}
			if disabledChanged {
				in.Disabled = disabled
			} else if existing != nil {
				in.Disabled = existing.Disabled
			}

			// Group-owned flags always nest under defaultGroup so create/update
			// share one UpsertProvider call (shared fields + default group +
			// optional discovery) instead of multi-stage partial commits.
			defaultGroup := app.ProviderGroupInput{
				ID:       config.DefaultGroupID,
				Name:     config.DefaultGroupName,
				Protocol: protocol,
			}
			if existing != nil {
				if dg := providerDisplayGroup(*existing, config.DefaultGroupID); dg.ID != "" {
					if strings.TrimSpace(dg.Name) != "" {
						defaultGroup.Name = dg.Name
					}
					if !cmd.Flags().Changed("protocol") {
						defaultGroup.Protocol = dg.Protocol
					}
				}
			}
			if apiKeyChanged {
				defaultGroup.APIKeysChanged = true
				if strings.TrimSpace(apiKey) != "" {
					defaultGroup.APIKeys = []string{strings.TrimSpace(apiKey)}
				}
			}
			if skipModels {
				// Non-nil Models skips discovery. Create stores empty; update
				// re-passes the existing catalog so provenance can be kept when
				// auth is unchanged.
				if existing != nil {
					if dg := providerDisplayGroup(*existing, config.DefaultGroupID); len(dg.Models) > 0 {
						defaultGroup.Models = append([]string(nil), dg.Models...)
					} else {
						defaultGroup.Models = []string{}
					}
				} else {
					defaultGroup.Models = []string{}
				}
			}
			// Models stays nil when !skipModels → UpsertProvider discovers.
			in.DefaultGroup = &defaultGroup

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
			group := providerDisplayGroup(result.Provider, config.DefaultGroupID)
			fmt.Fprintf(cmd.OutOrStdout(), "saved provider %q [%s] %s → %s\n", result.Provider.ID, state, group.Protocol, result.Provider.BaseURL)
			if !skipModels && group.ModelsSource == "discovered" {
				fmt.Fprintf(cmd.OutOrStdout(), "  discovered %d model(s)\n", len(group.Models))
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
				group := providerDisplayGroup(p, config.DefaultGroupID)
				key := "(none)"
				if group.APIKeyCount > 0 && len(group.APIKeysMasked) > 0 {
					key = group.APIKeysMasked[0]
				}
				state := "enabled"
				if p.Disabled {
					state = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s [%s] %-18s %s  apiKey=%s\n", p.ID, state, group.Protocol, p.BaseURL, key)
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
				group := providerDisplayGroup(p, config.DefaultGroupID)
				fmt.Fprintf(cmd.OutOrStdout(), "import %q [%s] → %s (models: %s)\n", p.ID, group.Protocol, p.BaseURL, strings.Join(group.Models, ","))
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

func newProviderGroupListCmd() *cobra.Command {
	var providerID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List groups under a provider",
		Long: `provider group list prints groups for one provider from local ocswitch config.

Output shows group id, protocol, enabled state, masked API keys, and model count.
It never prints plaintext keys.`,
		Example: `  ocswitch provider group list --provider su8`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			if providerID == "" {
				return fmt.Errorf("--provider is required")
			}
			groups, err := appService().ListProviderGroups(cmd.Context(), providerID)
			if err != nil {
				return err
			}
			if len(groups) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no groups)")
				return nil
			}
			for _, g := range groups {
				state := "enabled"
				if g.Disabled {
					state = "disabled"
				}
				key := "(none)"
				if g.APIKeyCount > 0 && len(g.APIKeysMasked) > 0 {
					key = strings.Join(g.APIKeysMasked, ",")
				} else if g.APIKeyCount > 0 {
					key = fmt.Sprintf("(%d set)", g.APIKeyCount)
				}
				name := g.Name
				if name == "" {
					name = "-"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s [%s] %-18s name=%s apiKeys=%s models=%d source=%s\n",
					g.ID, state, g.Protocol, name, key, len(g.Models), emptyCLILabel(g.ModelsSource))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	return cmd
}

func newProviderGroupCreateCmd() *cobra.Command {
	var providerID, groupID, name, protocol, apiKey string
	var apiKeys []string
	var models []string
	var disabled, dryRun bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a provider group",
		Long: `provider group create adds one group under an existing provider via Service.

Group identity (--id) is required and must be unique under the provider.
Protocol and API keys are group-scoped; base URL stays on the provider.`,
		Example: `  ocswitch provider group create --provider su8 --id premium --protocol openai-responses --api-key sk-premium
  ocswitch provider group create --provider su8 --id free --protocol openai-compatible --api-key sk-free --disabled`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			groupID = strings.TrimSpace(groupID)
			if providerID == "" || groupID == "" {
				return fmt.Errorf("--provider and --id are required")
			}
			protocol = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
			if err := config.ValidateProtocol(protocol); err != nil {
				return fmt.Errorf("invalid --protocol: %w", err)
			}
			keys := collectAPIKeys(apiKey, apiKeys, cmd.Flags().Changed("api-key") || cmd.Flags().Changed("api-keys"))
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would create provider %q group %q via Service\n", providerID, groupID)
				return nil
			}
			view, err := appService().CreateProviderGroup(cmd.Context(), app.ProviderGroupCreateInput{
				ProviderID: providerID,
				Group: app.ProviderGroupInput{
					ID:             groupID,
					Name:           strings.TrimSpace(name),
					Protocol:       protocol,
					APIKeysChanged: cmd.Flags().Changed("api-key") || cmd.Flags().Changed("api-keys"),
					APIKeys:        keys,
					Models:         models,
					Disabled:       disabled,
				},
			})
			if err != nil {
				return err
			}
			state := "enabled"
			if view.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created provider %q group %q [%s] %s keys=%d models=%d\n",
				providerID, view.ID, state, view.Protocol, view.APIKeyCount, len(view.Models))
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	cmd.Flags().StringVar(&groupID, "id", "", "group id (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&protocol, "protocol", config.ProtocolOpenAIResponses, "group protocol")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "upstream API key (single)")
	cmd.Flags().StringArrayVar(&apiKeys, "api-keys", nil, "upstream API key (repeatable)")
	cmd.Flags().StringArrayVar(&models, "model", nil, "seed model id (repeatable)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create group disabled")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newProviderGroupUpdateCmd() *cobra.Command {
	var providerID, groupID, name, protocol, apiKey, newID string
	var apiKeys []string
	var models []string
	var disabled, dryRun, yes bool
	var onProtected, onRewrite, rebind, rebindProvider, rebindGroup, rebindModel string
	var replaceProviderGroups []string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a provider group",
		Long: `provider group update changes one existing group under a provider via Service.

Identity is explicit: --provider and --group are required. Omitted fields keep
current values except when --api-key/--api-keys are passed (then keys are replaced).
Use --new-id to request a stable group id change. ID changes preview lifecycle
impact and forward selections to UpdateProviderGroup (same safe defaults/flags as
delete: --on-protected, --on-rewrite, --rebind, --replace-provider-group).`,
		Example: `  ocswitch provider group update --provider su8 --group premium --name "Premium pool"
  ocswitch provider group update --provider su8 --group free --api-key sk-new
  ocswitch provider group update --provider su8 --group premium --new-id gold --yes
  ocswitch provider group update --provider su8 --group premium --new-id gold --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			groupID = strings.TrimSpace(groupID)
			if providerID == "" || groupID == "" {
				return fmt.Errorf("--provider and --group are required")
			}
			groups, err := appService().ListProviderGroups(cmd.Context(), providerID)
			if err != nil {
				return err
			}
			var existing *app.ProviderGroupView
			for i := range groups {
				if groups[i].ID == groupID {
					existing = &groups[i]
					break
				}
			}
			if existing == nil {
				return fmt.Errorf("provider %q group %q not found", providerID, groupID)
			}
			desiredID := groupID
			if cmd.Flags().Changed("new-id") {
				desiredID = strings.TrimSpace(newID)
				if desiredID == "" {
					return fmt.Errorf("--new-id must not be empty")
				}
			}
			proto := existing.Protocol
			if cmd.Flags().Changed("protocol") {
				proto = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
				if err := config.ValidateProtocol(proto); err != nil {
					return fmt.Errorf("invalid --protocol: %w", err)
				}
			}
			nameChanged := cmd.Flags().Changed("name")
			displayName := ""
			if nameChanged {
				displayName = strings.TrimSpace(name)
			}
			disabledVal := existing.Disabled
			if cmd.Flags().Changed("disabled") {
				disabledVal = disabled
			}
			keysChanged := cmd.Flags().Changed("api-key") || cmd.Flags().Changed("api-keys")
			keys := collectAPIKeys(apiKey, apiKeys, keysChanged)
			var seedModels []string
			if cmd.Flags().Changed("model") {
				seedModels = models
			}
			groupIn := app.ProviderGroupInput{
				ID:             desiredID,
				Name:           displayName,
				NameChanged:    nameChanged,
				Protocol:       proto,
				APIKeysChanged: keysChanged,
				APIKeys:        keys,
				Models:         seedModels,
				Disabled:       disabledVal,
			}

			// Stable ID change: preview lifecycle, resolve choices (safe defaults/flags), then
			// UpdateProviderGroup with Selections so confirm cannot fail on missing choices.
			if desiredID != groupID {
				opts, perr := parseGroupRemoveSelectionOpts(onProtected, onRewrite, rebind, replaceProviderGroups)
				if perr != nil {
					return perr
				}
				opts, perr = applyExplicitRebindOpts(opts, rebindProvider, rebindGroup, rebindModel)
				if perr != nil {
					return perr
				}
				rev, plan, _, selections, perr := previewGroupIDChangeWithSelections(cmd, providerID, groupID, desiredID, opts)
				if perr != nil {
					return finishOutcome(cmd, perr, nil)
				}
				if jsonOutput && dryRun {
					_, env := app.ClassifyOutcome(nil, plan)
					return writeJSONEnvelope(cmd.OutOrStdout(), env)
				}
				if !jsonOutput {
					if dryRun {
						fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would rename provider %q group %q -> %q via UpdateProviderGroup\n", providerID, groupID, desiredID)
					}
					printPlanHuman(cmd.OutOrStdout(), plan)
				}
				if dryRun {
					return nil
				}
				if !plan.Executable {
					return finishOutcome(cmd, &app.OutcomeError{Code: "plan_not_executable", Params: map[string]any{
						"operationKind": lifecycle.OpGroupIDChange,
						"providerId":    providerID,
						"groupId":       groupID,
						"newGroupId":    desiredID,
						"blockerCount":  len(plan.Blockers),
						"choiceCount":   len(plan.Choices),
					}}, plan)
				}
				ok, cerr := confirmExecute(cmd, yes)
				if cerr != nil {
					return finishOutcome(cmd, cerr, plan)
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
				view, uerr := appService().UpdateProviderGroup(cmd.Context(), app.ProviderGroupUpdateInput{
					ProviderID:       providerID,
					GroupID:          groupID,
					Group:            groupIn,
					Selections:       selections,
					ExpectedRevision: rev,
				})
				if uerr != nil {
					return finishOutcome(cmd, uerr, nil)
				}
				if jsonOutput {
					_, env := app.ClassifyOutcome(nil, view)
					return writeJSONEnvelope(cmd.OutOrStdout(), env)
				}
				state := "enabled"
				if view.Disabled {
					state = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated provider %q group %q [%s] %s keys=%d models=%d\n",
					providerID, view.ID, state, view.Protocol, view.APIKeyCount, len(view.Models))
				return nil
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would update provider %q group %q via Service\n", providerID, groupID)
				return nil
			}
			view, err := appService().UpdateProviderGroup(cmd.Context(), app.ProviderGroupUpdateInput{
				ProviderID: providerID,
				GroupID:    groupID,
				Group:      groupIn,
			})
			if err != nil {
				return err
			}
			state := "enabled"
			if view.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated provider %q group %q [%s] %s keys=%d models=%d\n",
				providerID, view.ID, state, view.Protocol, view.APIKeyCount, len(view.Models))
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	cmd.Flags().StringVar(&groupID, "group", "", "group id (required)")
	cmd.Flags().StringVar(&newID, "new-id", "", "optional new group id")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&protocol, "protocol", "", "group protocol")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "upstream API key (single; replaces keys when set)")
	cmd.Flags().StringArrayVar(&apiKeys, "api-keys", nil, "upstream API key (repeatable; replaces keys when set)")
	cmd.Flags().StringArrayVar(&models, "model", nil, "replace model list (repeatable)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "set disabled state")
	cmd.Flags().StringVar(&onProtected, "on-protected", "", "for --new-id: protected target action (same as group delete)")
	cmd.Flags().StringVar(&onRewrite, "on-rewrite", "", "for --new-id: singleton rewrite action (same as group delete)")
	cmd.Flags().StringVar(&rebind, "rebind", "", "for --new-id: rebind protected targets to provider/group/model")
	cmd.Flags().StringVar(&rebindProvider, "rebind-provider", "", "for --new-id: explicit rebind provider")
	cmd.Flags().StringVar(&rebindGroup, "rebind-group", "", "for --new-id: explicit rebind group")
	cmd.Flags().StringVar(&rebindModel, "rebind-model", "", "for --new-id: explicit rebind model")
	cmd.Flags().StringArrayVar(&replaceProviderGroups, "replace-provider-group", nil, "for --new-id: replacement provider/group for rewrite (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview impact (and for --new-id print lifecycle plan) without persisting")
	cmd.Flags().BoolVar(&yes, "yes", false, "for --new-id: apply without interactive confirmation")
	return cmd
}

func newProviderGroupDeleteCmd() *cobra.Command {
	var providerID, groupID string
	var dryRun, yes bool
	var onProtected, onRewrite, rebind, rebindProvider, rebindGroup, rebindModel string
	var replaceProviderGroups []string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a provider group",
		Long: `provider group delete removes one group under a provider via Service.

Identity is explicit: --provider and --group are required. Protected alias targets
and singleton rewrite selectors require an explicit choice. Select with
--on-protected/--on-rewrite; rebind destinations may use the independent
--rebind-provider/--rebind-group/--rebind-model flags.`,
		Example: `  ocswitch provider group delete --provider su8 --group premium --dry-run
  ocswitch provider group delete --provider su8 --group premium --yes
  ocswitch provider group delete --provider su8 --group premium --rebind-provider su8 --rebind-group default --rebind-model org/model-a --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			groupID = strings.TrimSpace(groupID)
			if providerID == "" || groupID == "" {
				return fmt.Errorf("--provider and --group are required")
			}
			opts, err := parseGroupRemoveSelectionOpts(onProtected, onRewrite, rebind, replaceProviderGroups)
			if err != nil {
				return err
			}
			opts, err = applyExplicitRebindOpts(opts, rebindProvider, rebindGroup, rebindModel)
			if err != nil {
				return err
			}
			rev, plan, op, selections, err := previewRemoveProviderGroupWithSelections(cmd, providerID, groupID, opts)
			if err != nil {
				return finishOutcome(cmd, err, nil)
			}
			if jsonOutput && dryRun {
				_, env := app.ClassifyOutcome(nil, plan)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			if !jsonOutput {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would delete provider %q group %q\n", providerID, groupID)
				}
				printPlanHuman(cmd.OutOrStdout(), plan)
			}
			if dryRun {
				return nil
			}
			if !plan.Executable || strings.TrimSpace(plan.PlanToken) == "" {
				return finishOutcome(cmd, &app.OutcomeError{Code: "plan_not_executable", Params: map[string]any{
					"operationKind": lifecycle.OpGroupRemove,
					"providerId":    providerID,
					"groupId":       groupID,
					"blockerCount":  len(plan.Blockers),
					"choiceCount":   len(plan.Choices),
				}}, plan)
			}
			ok, cerr := confirmExecute(cmd, yes)
			if cerr != nil {
				return finishOutcome(cmd, cerr, plan)
			}
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "aborted")
				return nil
			}
			result, err := executeLifecyclePlan(cmd, rev, plan, op, selections)
			if err != nil {
				return finishOutcome(cmd, err, result)
			}
			if jsonOutput {
				_, env := app.ClassifyOutcome(nil, result)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted provider %q group %q\n", providerID, groupID)
			printExecuteHuman(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	cmd.Flags().StringVar(&groupID, "group", "", "group id (required)")
	cmd.Flags().StringVar(&onProtected, "on-protected", "", "protected target action: remove_target, delete_alias, rebind_target")
	cmd.Flags().StringVar(&onRewrite, "on-rewrite", "", "singleton rewrite action: keep_dormant, disable_rule, delete_rule, replace_provider_groups")
	cmd.Flags().StringVar(&rebind, "rebind", "", "rebind protected targets to provider/group/model (implies rebind_target)")
	cmd.Flags().StringVar(&rebindProvider, "rebind-provider", "", "explicit rebind provider")
	cmd.Flags().StringVar(&rebindGroup, "rebind-group", "", "explicit rebind group")
	cmd.Flags().StringVar(&rebindModel, "rebind-model", "", "explicit rebind model")
	cmd.Flags().StringArrayVar(&replaceProviderGroups, "replace-provider-group", nil, "replacement provider/group for rewrite (repeatable; implies replace_provider_groups)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview impact plan without persisting")
	cmd.Flags().BoolVar(&yes, "yes", false, "delete without interactive confirmation")
	return cmd
}

func parseGroupRemoveSelectionOpts(onProtected, onRewrite, rebind string, replaceGroups []string) (groupRemoveSelectionOpts, error) {
	opts := groupRemoveSelectionOpts{
		OnProtected: strings.TrimSpace(onProtected),
		OnRewrite:   strings.TrimSpace(onRewrite),
	}
	if rebind = strings.TrimSpace(rebind); rebind != "" {
		parts := strings.SplitN(rebind, "/", 3)
		if len(parts) != 3 {
			return opts, fmt.Errorf("--rebind must be provider/group/model")
		}
		opts.RebindProvider = strings.TrimSpace(parts[0])
		opts.RebindGroup = strings.TrimSpace(parts[1])
		opts.RebindModel = strings.TrimSpace(parts[2])
		if opts.RebindProvider == "" || opts.RebindGroup == "" || opts.RebindModel == "" {
			return opts, fmt.Errorf("--rebind must be provider/group/model")
		}
		if opts.OnProtected == "" {
			opts.OnProtected = lifecycle.OptionRebindTarget
		}
	}
	if len(replaceGroups) > 0 {
		selectors := make([]config.ProviderGroupSelector, 0, len(replaceGroups))
		for _, raw := range replaceGroups {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			slash := strings.Index(raw, "/")
			if slash <= 0 || slash == len(raw)-1 {
				return opts, fmt.Errorf("--replace-provider-group must be provider/group, got %q", raw)
			}
			selectors = append(selectors, config.ProviderGroupSelector{
				Provider: strings.TrimSpace(raw[:slash]),
				Group:    strings.TrimSpace(raw[slash+1:]),
			})
		}
		opts.ReplaceProviderGroups = selectors
		if opts.OnRewrite == "" {
			opts.OnRewrite = lifecycle.OptionReplaceProviderGroups
		}
	}
	switch opts.OnProtected {
	case "", lifecycle.OptionRemoveTarget, lifecycle.OptionDeleteAlias, lifecycle.OptionRebindTarget:
	default:
		return opts, fmt.Errorf("invalid --on-protected %q", opts.OnProtected)
	}
	switch opts.OnRewrite {
	case "", lifecycle.OptionKeepDormant, lifecycle.OptionDisableRule, lifecycle.OptionDeleteRule, lifecycle.OptionReplaceProviderGroups:
	default:
		return opts, fmt.Errorf("invalid --on-rewrite %q", opts.OnRewrite)
	}
	return opts, nil
}

func applyExplicitRebindOpts(opts groupRemoveSelectionOpts, provider, group, model string) (groupRemoveSelectionOpts, error) {
	provider = strings.TrimSpace(provider)
	group = strings.TrimSpace(group)
	model = strings.TrimSpace(model)
	if provider == "" && group == "" && model == "" {
		return opts, nil
	}
	if provider == "" || group == "" || model == "" {
		return opts, fmt.Errorf("--rebind-provider, --rebind-group and --rebind-model must be provided together")
	}
	opts.RebindProvider = provider
	opts.RebindGroup = group
	opts.RebindModel = model
	opts.OnProtected = lifecycle.OptionRebindTarget
	return opts, nil
}

func newProviderGroupRefreshModelsCmd() *cobra.Command {
	var providerID, groupID string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "refresh-models",
		Short: "Refresh models for one provider group",
		Long: `provider group refresh-models discovers models for one exact (provider, group).

Empty group is not accepted here: identity must be explicit.`,
		Example: `  ocswitch provider group refresh-models --provider su8 --group default
  ocswitch provider group refresh-models --provider su8 --group premium`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			groupID = strings.TrimSpace(groupID)
			if providerID == "" || groupID == "" {
				return fmt.Errorf("--provider and --group are required")
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would refresh models for provider %q group %q via Service\n", providerID, groupID)
				return nil
			}
			result, err := appService().RefreshProviderGroupModels(cmd.Context(), app.ProviderGroupRefreshModelsInput{
				ProviderID: providerID,
				GroupID:    groupID,
			})
			if err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", rewriteProviderWarning(warning))
			}
			// Prefer the exact group catalog when present.
			selectedGroup := providerDisplayGroup(result.Provider, groupID)
			modelCount := len(selectedGroup.Models)
			source := selectedGroup.ModelsSource
			for _, g := range result.Provider.Groups {
				if g.ID == groupID {
					modelCount = len(g.Models)
					source = g.ModelsSource
					break
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "refreshed provider %q group %q models=%d source=%s\n",
				providerID, groupID, modelCount, emptyCLILabel(source))
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	cmd.Flags().StringVar(&groupID, "group", "", "group id (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without contacting upstream")
	return cmd
}

func providerDisplayGroup(provider app.ProviderView, groupID string) app.ProviderGroupView {
	for _, group := range provider.Groups {
		if group.ID == groupID {
			return group
		}
	}
	return app.ProviderGroupView{}
}

func newProviderGroupPingCmd() *cobra.Command {
	var providerID, groupID, baseURL string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Ping base URL using one provider group's credentials",
		Long: `provider group ping checks reachability of the provider base URL using one
exact group's protocol and API keys. Identity is explicit.`,
		Example: `  ocswitch provider group ping --provider su8 --group default
  ocswitch provider group ping --provider su8 --group premium --base-url https://alt.example/v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providerID = strings.TrimSpace(providerID)
			groupID = strings.TrimSpace(groupID)
			if providerID == "" || groupID == "" {
				return fmt.Errorf("--provider and --group are required")
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would ping provider %q group %q via Service\n", providerID, groupID)
				return nil
			}
			result, err := appService().PingProviderGroupBaseURL(cmd.Context(), app.ProviderGroupPingInput{
				ProviderID: providerID,
				GroupID:    groupID,
				BaseURL:    strings.TrimSpace(baseURL),
			})
			if err != nil {
				return err
			}
			status := "unreachable"
			if result.Reachable {
				status = "reachable"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ping provider %q group %q %s latency=%dms status=%d url=%s\n",
				providerID, groupID, status, result.LatencyMs, result.StatusCode, result.BaseURL)
			if result.Error != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", result.Error)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider id (required)")
	cmd.Flags().StringVar(&groupID, "group", "", "group id (required)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "optional base URL override")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without contacting upstream")
	return cmd
}

func collectAPIKeys(single string, multi []string, changed bool) []string {
	if !changed {
		return nil
	}
	out := make([]string, 0, 1+len(multi))
	if strings.TrimSpace(single) != "" {
		out = append(out, strings.TrimSpace(single))
	}
	for _, k := range multi {
		k = strings.TrimSpace(k)
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func emptyCLILabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
