package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

func newOpencodeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "opencode",
		Short: "OpenCode integration commands",
		Long: `OpenCode commands manage the narrow integration boundary between ocswitch and
OpenCode config.

These commands do not attempt full OpenCode config takeover. They are limited to
the provider.ocswitch sync path and optional top-level model fields when you opt in
explicitly.

Common workflow: validate with doctor first, inspect sync help, then run
opencode sync.`,
		Example: `  ocswitch opencode sync --dry-run
  ocswitch opencode sync --set-model ocswitch/gpt-5.4`,
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
		Short: "Update provider.ocswitch in the global OpenCode config to match current aliases",
		Long: `ocswitch opencode sync writes provider.ocswitch into the target OpenCode config.

By default it targets the global user config (~/.config/opencode), picking the
existing file in precedence order opencode.jsonc > opencode.json > config.json,
or creating opencode.jsonc if none exists. It does NOT touch the top-level
"model" or "small_model" unless --set-model / --set-small-model are given.

If the target file is JSONC, sync rewrites it as normalized plain JSON and any
comments/trailing commas are lost. Existing provider.ocswitch model metadata is
preserved when alias names stay the same, but this command is still a writing
operation rather than a comment-preserving patcher.

The default target scope is only the global user config path; it does not follow
OPENCODE_CONFIG_DIR unless you pass --target yourself. The command writes alias
exposure into provider.ocswitch.models using only aliases that are currently
routable.
Use --dry-run to preview the resolved target file without writing it. Typical
workflow: run ocswitch doctor first, then sync, then start or restart ocswitch serve if
needed.`,
		Example: `  ocswitch opencode sync
  ocswitch opencode sync --dry-run
  ocswitch opencode sync --set-model ocswitch/gpt-5.4
  ocswitch opencode sync --set-model ocswitch/gpt-5.4 --set-small-model ocswitch/gpt-5.4-mini
  ocswitch opencode sync --target /path/to/opencode.jsonc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := appService().ApplyOpenCodeSync(cmd.Context(), app.SyncInput{
				Target:        target,
				SetModel:      setModel,
				SetSmallModel: setSmallModel,
				DryRun:        dryRun,
			})
			if err != nil {
				return err
			}
			if !result.Changed {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ no changes required at %s [%s]\n", result.TargetPath, result.Protocol)
				return nil
			}
			if result.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "would write %s [%s] (dry-run)\n", result.TargetPath, result.Protocol)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced provider.ocswitch into %s [%s] (%d alias(es))\n", result.TargetPath, result.Protocol, len(result.AliasNames))
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
