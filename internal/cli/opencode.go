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
		Long: `OpenCode commands manage the narrow integration boundary between olpx and
OpenCode config.

These commands do not attempt full OpenCode config takeover. They are limited to
the provider.olpx sync path and optional top-level model fields when you opt in
explicitly.

Common workflow: validate with doctor first, inspect sync help, then run
opencode sync.`,
		Example: `  olpx opencode sync --dry-run
  olpx opencode sync --set-model olpx/gpt-5.4`,
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
		Short: "Update provider.olpx in the global OpenCode config to match current aliases",
		Long: `olpx opencode sync writes provider.olpx into the target OpenCode config.

By default it targets the global user config (~/.config/opencode), picking the
existing file in precedence order opencode.jsonc > opencode.json > config.json,
or creating opencode.jsonc if none exists. It does NOT touch the top-level
"model" or "small_model" unless --set-model / --set-small-model are given.

The default target scope is only the global user config path; it does not follow
OPENCODE_CONFIG_DIR unless you pass --target yourself. The command writes alias
exposure into provider.olpx.models using only aliases that are currently
routable.

Use --dry-run to preview the resolved target file without writing it. Typical
workflow: run olpx doctor first, then sync, then start or restart olpx serve if
needed.`,
		Example: `  olpx opencode sync
  olpx opencode sync --dry-run
  olpx opencode sync --set-model olpx/gpt-5.4
  olpx opencode sync --set-model olpx/gpt-5.4 --set-small-model olpx/gpt-5.4-mini
  olpx opencode sync --target /path/to/opencode.jsonc`,
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
			aliasNames := cfg.AvailableAliasNames()
			baseURL := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
			changed := opencode.EnsureOLPXProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
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
			fmt.Fprintf(cmd.OutOrStdout(), "synced provider.olpx into %s (%d alias(es))\n", path, len(aliasNames))
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
