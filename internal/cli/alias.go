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
  ocswitch alias bind --alias gpt-5.4 --provider su8 --model gpt-5.4
  ocswitch alias bind --alias gpt-5.4 --provider codex --model GPT-5.4
  ocswitch alias list`,
	}
	c.AddCommand(newAliasAddCmd(), newAliasListCmd(), newAliasBindCmd(), newAliasUnbindCmd(), newAliasRemoveCmd())
	return c
}

func newAliasAddCmd() *cobra.Command {
	var name, display string
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
			a := config.Alias{Alias: name, DisplayName: display, Enabled: !disabled}
			if existing != nil {
				if display == "" {
					a.DisplayName = existing.DisplayName
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
			fmt.Fprintf(cmd.OutOrStdout(), "saved alias %q (enabled=%v)\n", name, a.Enabled)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "alias name exposed as ocswitch/<name> in OpenCode (required)")
	cmd.Flags().StringVar(&display, "display-name", "", "human-friendly display name")
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
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%s]\n", a.Alias, state)
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
auto-creates an enabled alias for convenience. Binding does not test upstream
health or credentials.

Order matters: the first bound target is tried first, the second is fallback,
and so on. Typical next step: inspect with alias list, then run doctor.`,
		Example: `  ocswitch alias bind --alias gpt-5.4 --provider su8 --model gpt-5.4
  ocswitch alias bind --alias gpt-5.4 --provider codex --model GPT-5.4
  ocswitch alias bind --alias gpt-5.4 --provider relay --model gpt-5.4 --disabled`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias == "" || provider == "" || model == "" {
				return fmt.Errorf("--alias, --provider and --model are required")
			}
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if cfg.FindProvider(provider) == nil {
				return fmt.Errorf("provider %q does not exist; add it first", provider)
			}
			if cfg.FindAlias(alias) == nil {
				// auto-create enabled alias for ergonomics
				cfg.UpsertAlias(config.Alias{Alias: alias, Enabled: true})
			}
			if err := cfg.AddTarget(alias, config.Target{Provider: provider, Model: model, Enabled: !disabled}); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bound %s → %s/%s\n", alias, provider, model)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "alias name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (required)")
	cmd.Flags().StringVar(&model, "model", "", "upstream model id (required)")
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

Typical next step: run alias list or doctor to confirm the remaining target
chain.`,
		Example: `  ocswitch alias unbind --alias gpt-5.4 --provider codex --model GPT-5.4
  ocswitch doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias == "" || provider == "" || model == "" {
				return fmt.Errorf("--alias, --provider and --model are required")
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
	cmd.Flags().StringVar(&provider, "provider", "", "upstream provider id (required)")
	cmd.Flags().StringVar(&model, "model", "", "upstream model id (required)")
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
