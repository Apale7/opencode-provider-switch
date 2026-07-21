package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func newAliasCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "alias",
		Short: "Manage logical aliases routed by ocswitch",
		Long: `Alias commands manage the user-facing model names that OpenCode sees as
ocswitch/<alias>.

Each alias contains an ordered target chain of provider/model pairs. Target
order is operational: ocswitch tries targets in order and only fails over before any
response bytes are sent downstream.

Common workflow: create an alias, bind primary and fallback targets, inspect the
result with alias list, then run doctor and opencode sync.`,
		Example: `  ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
	  ocswitch alias bind --alias gpt-5.4 --model su8/gpt-5.4
	  ocswitch alias bind --alias gpt-5.4 --model codex/GPT-5.4
	  ocswitch alias list`,
	}
	c.AddCommand(newAliasAddCmd(), newAliasListCmd(), newAliasBindCmd(), newAliasUnbindCmd(), newAliasRemoveCmd())
	return c
}

func newAliasAddCmd() *cobra.Command {
	var name, display, protocol string
	var disabled bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create or update an alias (without targets)",
		Long: `alias add creates or updates alias metadata in local ocswitch config via Service.

It writes the alias record itself, but it does not add or validate targets.
Auto-generated aliases must be upgraded before mutation.

When updating an existing alias, omitted display-name preserves the current
value and existing targets stay attached. Typical next step: add targets with
ocswitch alias bind.`,
		Example: `  ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
  ocswitch alias add --name gpt-5.4-mini --disabled
  ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4 Reasoning"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would upsert alias %q via Service/ConfigStore\n", name)
				return nil
			}
			view, err := appService().UpsertAlias(cmd.Context(), app.AliasUpsertInput{
				Alias:       name,
				DisplayName: display,
				Protocol:    protocol,
				Disabled:    disabled,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved alias %q [%s] (enabled=%v)\n", view.Alias, view.Protocol, view.Enabled)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "alias name exposed as ocswitch/<name> in OpenCode (required)")
	cmd.Flags().StringVar(&display, "display-name", "", "human-friendly display name")
	cmd.Flags().StringVar(&protocol, "protocol", config.ProtocolOpenAIResponses, "alias protocol")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create in disabled state")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List aliases and their target chains",
		Long: `alias list prints aliases from local ocswitch config together with their target
chains.

Output shows alias enabled state, target order, target enabled markers, and a
note when a referenced provider is missing or disabled. This is the easiest way
to verify failover order before running doctor or opencode sync.

This command does not modify config and does not contact upstream providers.`,
		Example: `  ocswitch alias list
  ocswitch --config /path/to/config.json alias list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := appService()
			aliases, err := svc.ListAliases(cmd.Context())
			if err != nil {
				return err
			}
			providers, err := svc.ListProviders(cmd.Context())
			if err != nil {
				return err
			}
			providerByID := make(map[string]app.ProviderView, len(providers))
			for _, p := range providers {
				providerByID[p.ID] = p
			}
			if len(aliases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no aliases)")
				return nil
			}
			for _, a := range aliases {
				state := "enabled"
				if !a.Enabled {
					state = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%s] [%s]\n", a.Alias, state, config.NormalizeAliasProtocol(a.Protocol))
				for i, t := range a.Targets {
					mark := "x"
					if !t.Enabled {
						mark = " "
					}
					note := ""
					p, ok := providerByID[t.Provider]
					switch {
					case !ok:
						note = " (missing provider)"
					case p.Disabled:
						note = " (provider disabled)"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %d. %s/%s%s\n", mark, i+1, t.Provider, t.Model, note)
				}
			}
			return nil
		},
	}
}

func newAliasBindCmd() *cobra.Command {
	var alias, provider, model string
	var disabled bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Append a target (provider/model) to an alias in failover order",
		Long: `alias bind appends one provider/model target to an alias's ordered failover
chain in local ocswitch config via Service.

The provider must already exist. If the alias does not exist yet, this command
auto-creates an enabled alias for convenience. You can pass the target either as
--provider <id> --model <name> or in the more natural combined form --model
<provider>/<model> when --provider is omitted; the combined form is recommended
and the explicit --provider flag remains as fallback compatibility. If the
provider has a stored model catalog discovered from /v1/models, bind validates
the model name against that discovered list.

Order matters: the first bound target is tried first, the second is fallback,
and so on. Auto aliases must be upgraded before bind. Typical next step: inspect
with alias list, then run doctor.`,
		Example: `  ocswitch alias bind --alias gpt-5.4 --model su8/gpt-5.4
  ocswitch alias bind --alias gpt-5.4 --model codex/GPT-5.4
  ocswitch alias bind --alias gpt-5.4 --provider relay --model gpt-5.4 --disabled`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias == "" || model == "" {
				return fmt.Errorf("--alias and --model are required")
			}
			combinedProvider, combinedModel, combined := parseProviderModelRef(model)
			if provider == "" {
				if !combined {
					return fmt.Errorf("--model must use <provider>/<model> when --provider is omitted")
				}
				provider = combinedProvider
				model = combinedModel
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would bind %s → %s/%s via Service/ConfigStore\n", alias, provider, model)
				return nil
			}
			view, err := appService().BindAliasTarget(cmd.Context(), app.AliasTargetInput{
				Alias:    alias,
				Provider: provider,
				Model:    model,
				Disabled: disabled,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bound %s [%s] → %s/%s\n", view.Alias, view.Protocol, provider, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "alias name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (fallback; prefer --model provider/model)")
	cmd.Flags().StringVar(&model, "model", "", "upstream target model, or provider/model when --provider is omitted (required)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "add target in disabled state")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newAliasUnbindCmd() *cobra.Command {
	var alias, provider, model string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "unbind",
		Short: "Remove a target from an alias",
		Long: `alias unbind removes one concrete provider/model target tuple from an alias in
local ocswitch config via Service.

It does not delete the alias itself. Removing a target can leave the alias with
no routable targets, which doctor and opencode sync will then treat as invalid
or unavailable.

You can identify the target either as --provider <id> --model <name> or in the
recommended combined form --model <provider>/<model> when --provider is omitted.`,
		Example: `  ocswitch alias unbind --alias gpt-5.4 --model codex/GPT-5.4
  ocswitch alias unbind --alias gpt-5.4 --provider codex --model GPT-5.4
  ocswitch doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias == "" || model == "" {
				return fmt.Errorf("--alias and --model are required")
			}
			combinedProvider, combinedModel, combined := parseProviderModelRef(model)
			if provider == "" {
				if !combined {
					return fmt.Errorf("--model must use <provider>/<model> when --provider is omitted")
				}
				provider = combinedProvider
				model = combinedModel
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would unbind %s → %s/%s via Service/ConfigStore\n", alias, provider, model)
				return nil
			}
			if _, err := appService().UnbindAliasTarget(cmd.Context(), app.AliasTargetInput{
				Alias:    alias,
				Provider: provider,
				Model:    model,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unbound %s → %s/%s\n", alias, provider, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "alias name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (fallback; prefer --model provider/model)")
	cmd.Flags().StringVar(&model, "model", "", "upstream target model, or provider/model when --provider is omitted (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newAliasRemoveCmd() *cobra.Command {
	var dryRun bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <alias>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete an alias entirely",
		Long: `alias remove deletes one alias and all of its target bindings from local ocswitch
config via Service lifecycle planning.

Rewrite selector impacts default to keep_rule on this convenience path.
Use --dry-run to print the impact plan without persisting. Non-interactive
shells require --yes to execute. Use --json for a single envelope on stdout.`,
		Example: `  ocswitch alias remove gpt-5.4 --dry-run
  ocswitch alias remove gpt-5.4 --yes
  ocswitch opencode sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			rev, plan, selections, err := previewRemoveAliasWithSelections(cmd, name)
			if err != nil {
				return finishOutcome(cmd, err, nil)
			}
			payload, _ := marshalPayload(lifecycle.AliasRemovePayload{Alias: name})
			op := lifecycle.Operation{Kind: lifecycle.OpAliasRemove, Payload: payload}

			if jsonOutput && dryRun {
				_, env := app.ClassifyOutcome(nil, plan)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			if !jsonOutput {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would remove alias %q\n", name)
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
						"operationKind": lifecycle.OpAliasRemove,
						"alias":         name,
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
			result, err := executeLifecyclePlan(cmd, rev, plan, op, selections)
			if err != nil {
				return finishOutcome(cmd, err, result)
			}
			if jsonOutput {
				_, env := app.ClassifyOutcome(nil, result)
				return writeJSONEnvelope(cmd.OutOrStdout(), env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed alias %q\n", name)
			printExecuteHuman(cmd.OutOrStdout(), result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview impact plan without persisting")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute without interactive confirmation")
	return cmd
}
