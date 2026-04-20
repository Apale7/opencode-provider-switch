package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/config"
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
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create or update an alias (without targets)",
		Long: `alias add creates or updates alias metadata in local ocswitch config.

It writes the alias record itself, but it does not add or validate targets.
Enabled aliases still need at least one routable target before doctor and
opencode sync will treat them as usable.

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
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			existing := cfg.FindAlias(name)
			a := config.Alias{Alias: name, DisplayName: display, Protocol: config.NormalizeAliasProtocol(protocol), Enabled: !disabled}
			if existing != nil {
				if display == "" {
					a.DisplayName = existing.DisplayName
				}
				if strings.TrimSpace(protocol) == "" {
					a.Protocol = existing.Protocol
				}
				a.Enabled = existing.Enabled
				if disabled {
					a.Enabled = false
				}
				a.Targets = existing.Targets
			}
			cfg.UpsertAlias(a)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved alias %q [%s] (enabled=%v)\n", name, a.Protocol, a.Enabled)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "alias name exposed as ocswitch/<name> in OpenCode (required)")
	cmd.Flags().StringVar(&display, "display-name", "", "human-friendly display name")
	cmd.Flags().StringVar(&protocol, "protocol", config.ProtocolOpenAIResponses, "alias protocol")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create in disabled state")
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
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			aliases := append([]config.Alias(nil), cfg.Aliases...)
			sort.Slice(aliases, func(i, j int) bool { return aliases[i].Alias < aliases[j].Alias })
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
					provider := cfg.FindProvider(t.Provider)
					switch {
					case provider == nil:
						note = " (missing provider)"
					case !provider.IsEnabled():
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
	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Append a target (provider/model) to an alias in failover order",
		Long: `alias bind appends one provider/model target to an alias's ordered failover
chain in local ocswitch config.

The provider must already exist. If the alias does not exist yet, this command
auto-creates an enabled alias for convenience. You can pass the target either as
--provider <id> --model <name> or in the more natural combined form --model
<provider>/<model> when --provider is omitted; the combined form is recommended
and the explicit --provider flag remains as fallback compatibility. If the
provider has a stored model catalog discovered from /v1/models, bind validates
the model name against that discovered list.

Order matters: the first bound target is tried first, the second is fallback,
and so on. Typical next step: inspect with alias list, then run doctor.`,
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
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			p := cfg.FindProvider(provider)
			if p == nil {
				return fmt.Errorf("provider %q does not exist; add it first", provider)
			}
			providerProtocol := config.NormalizeProviderProtocol(p.Protocol)
			if err := validateProviderModelKnown(provider, p.Models, p.ModelsSource, model); err != nil {
				return err
			}
			currentAlias := cfg.FindAlias(alias)
			if currentAlias == nil {
				cfg.UpsertAlias(config.Alias{Alias: alias, Protocol: providerProtocol, Enabled: true})
			} else if !config.ProtocolsMatch(currentAlias.Protocol, providerProtocol) {
				return fmt.Errorf("alias %q protocol %q does not match provider %q protocol %q", alias, config.NormalizeAliasProtocol(currentAlias.Protocol), provider, providerProtocol)
			}
			if err := cfg.AddTarget(alias, config.Target{Provider: provider, Model: model, Enabled: !disabled}); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bound %s [%s] → %s/%s\n", alias, providerProtocol, provider, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "alias name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (fallback; prefer --model provider/model)")
	cmd.Flags().StringVar(&model, "model", "", "upstream target model, or provider/model when --provider is omitted (required)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "add target in disabled state")
	return cmd
}
func newAliasUnbindCmd() *cobra.Command {
	var alias, provider, model string
	cmd := &cobra.Command{
		Use:   "unbind",
		Short: "Remove a target from an alias",
		Long: `alias unbind removes one concrete provider/model target tuple from an alias in
local ocswitch config.

It does not delete the alias itself. Removing a target can leave the alias with
no routable targets, which doctor and opencode sync will then treat as invalid
or unavailable.

You can identify the target either as --provider <id> --model <name> or in the
recommended combined form --model <provider>/<model> when --provider is omitted.
The explicit --provider flag remains available as a compatibility fallback.

Typical next step: run alias list or doctor to confirm the remaining target
chain.`,
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
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if err := cfg.RemoveTarget(alias, provider, model); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unbound %s → %s/%s\n", alias, provider, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "alias name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (fallback; prefer --model provider/model)")
	cmd.Flags().StringVar(&model, "model", "", "upstream target model, or provider/model when --provider is omitted (required)")
	return cmd
}

func newAliasRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <alias>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete an alias entirely",
		Long: `alias remove deletes one alias and all of its target bindings from local ocswitch
config.

Future opencode sync runs will stop exposing that alias in provider.ocswitch.models.
This command does not directly clear top-level model selections that may still
reference the old alias in OpenCode config.

Typical next step: run ocswitch opencode sync if OpenCode exposure should be updated.`,
		Example: `  ocswitch alias remove gpt-5.4
  ocswitch opencode sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if !cfg.RemoveAlias(args[0]) {
				return fmt.Errorf("alias %q not found", args[0])
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed alias %q\n", args[0])
			return nil
		},
	}
}

// unused helper kept for clarity if future commands want to normalize lists
var _ = strings.TrimSpace
