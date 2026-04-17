package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/config"
)

func newAliasCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "alias",
		Short: "Manage logical aliases routed by ops",
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
	cmd.Flags().StringVar(&name, "name", "", "alias name exposed as ops/<name> in OpenCode (required)")
	cmd.Flags().StringVar(&display, "display-name", "", "human-friendly display name")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create in disabled state")
	return cmd
}

func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List aliases and their target chains",
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
					fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %d. %s/%s\n", mark, i+1, t.Provider, t.Model)
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
