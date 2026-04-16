package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/opencode"
)

func newOpencodeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "opencode",
		Short: "OpenCode integration commands",
	}
	c.AddCommand(newOpencodeSyncCmd())
	return c
}

func newOpencodeSyncCmd() *cobra.Command {
	var target string
	var setModel string
	var setSmallModel string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Update provider.ops in the global OpenCode config to match current aliases",
		Long: `ops opencode sync writes provider.ops into the target OpenCode config.

By default it targets the global user config (~/.config/opencode), picking the
existing file in precedence order opencode.jsonc > opencode.json > config.json,
or creating opencode.jsonc if none exists. It does NOT touch the top-level
"model" or "small_model" unless --set-model / --set-small-model are given.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if errs := cfg.Validate(); len(errs) > 0 {
				for _, e := range errs {
					cmd.PrintErrln("config error:", e)
				}
				return errs[0]
			}
			path := target
			if path == "" {
				p, _ := opencode.ResolveGlobalConfigPath()
				path = p
			}
			raw, err := opencode.Load(path)
			if err != nil {
				return err
			}
			aliasNames := []string{}
			for _, a := range cfg.Aliases {
				if !a.Enabled {
					continue
				}
				aliasNames = append(aliasNames, a.Alias)
			}
			baseURL := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
			changed := opencode.EnsureOpsProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
			if setModel != "" {
				if raw["model"] != setModel {
					raw["model"] = setModel
					changed = true
				}
			}
			if setSmallModel != "" {
				if raw["small_model"] != setSmallModel {
					raw["small_model"] = setSmallModel
					changed = true
				}
			}
			if !changed {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ no changes required at %s\n", path)
				return nil
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "would write %s (dry-run)\n", path)
				return nil
			}
			if err := opencode.Save(path, raw); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced provider.ops into %s (%d alias(es))\n", path, len(aliasNames))
			if setModel != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  model = %s\n", setModel)
			}
			if setSmallModel != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  small_model = %s\n", setSmallModel)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "explicit opencode config file to write (default: global)")
	cmd.Flags().StringVar(&setModel, "set-model", "", "also set top-level model (opt-in only)")
	cmd.Flags().StringVar(&setSmallModel, "set-small-model", "", "also set top-level small_model (opt-in only)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not write, just report intent")
	return cmd
}
