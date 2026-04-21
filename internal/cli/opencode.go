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
	var runtimeBaseURL string
	var runtimeDirectory string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Update synced ocswitch providers in the global OpenCode config",
		Long: `ocswitch opencode sync writes protocol-scoped ocswitch providers into the target OpenCode config.

By default it targets the global user config (~/.config/opencode), picking the
existing file in precedence order opencode.jsonc > opencode.json > config.json,
or creating opencode.jsonc if none exists. It does NOT touch the top-level
"model" or "small_model" unless --set-model / --set-small-model are given.

If the target file is JSONC, sync rewrites it as normalized plain JSON and any
		comments/trailing commas are lost. Existing synced provider model metadata is
		preserved when alias names stay the same, but this command is still a writing
		operation rather than a comment-preserving patcher.

		The default target scope is only the global user config path; it does not follow
		OPENCODE_CONFIG_DIR unless you pass --target yourself. The command writes alias
		exposure into provider.ocswitch.models and other protocol-matched provider.<key>.models using only aliases that are currently
		routable.
Use --dry-run to preview the resolved target file without writing it. Typical
workflow: run ocswitch doctor first, then sync, then start or restart ocswitch serve if
needed. Use --runtime-base-url / --runtime-directory when you want reconciliation
against a non-default OpenCode runtime context.`,
		Example: `  ocswitch opencode sync
  ocswitch opencode sync --dry-run
  ocswitch opencode sync --set-model ocswitch/gpt-5.4
  ocswitch opencode sync --set-model ocswitch/gpt-5.4 --set-small-model ocswitch/gpt-5.4-mini
  ocswitch opencode sync --runtime-base-url http://localhost:54321 --runtime-directory /workspace/demo
  ocswitch opencode sync --target /path/to/opencode.jsonc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := appService().ApplyOpenCodeSync(cmd.Context(), app.SyncInput{
				Target:           target,
				SetModel:         setModel,
				SetSmallModel:    setSmallModel,
				DryRun:           dryRun,
				RuntimeBaseURL:   runtimeBaseURL,
				RuntimeDirectory: runtimeDirectory,
			})
			if err != nil {
				return err
			}
			if !result.Changed {
				primaryProtocol := ""
				if len(result.Protocols) > 0 {
					primaryProtocol = result.Protocols[0].Protocol
				}
				if primaryProtocol != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "✓ no changes required at %s [%s]\n", result.TargetPath, primaryProtocol)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "✓ no changes required at %s\n", result.TargetPath)
				}
				return nil
			}
			if result.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "would write %s (dry-run)\n", result.TargetPath)
				for _, provider := range result.Protocols {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] (%d alias(es))\n", provider.Key, provider.Protocol, len(provider.AliasNames))
				}
				printSyncSummary(cmd, result)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced providers into %s\n", result.TargetPath)
			for _, provider := range result.Protocols {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] (%d alias(es))\n", provider.Key, provider.Protocol, len(provider.AliasNames))
			}
			if setModel != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  model = %s\n", setModel)
			}
			if setSmallModel != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  small_model = %s\n", setSmallModel)
			}
			printSyncSummary(cmd, result)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "explicit opencode config file to write (default: global)")
	cmd.Flags().StringVar(&setModel, "set-model", "", "also set top-level model (opt-in only)")
	cmd.Flags().StringVar(&setSmallModel, "set-small-model", "", "also set top-level small_model (opt-in only)")
	cmd.Flags().StringVar(&runtimeBaseURL, "runtime-base-url", "", "OpenCode runtime base URL for reconciliation preview")
	cmd.Flags().StringVar(&runtimeDirectory, "runtime-directory", "", "OpenCode runtime directory for reconciliation preview")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not write, just report intent")
	return cmd
}

func printSyncSummary(cmd *cobra.Command, result app.SyncResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "  runtime: %s", result.RuntimeBaseURL)
	if result.RuntimeDirectory != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " [dir=%s]", result.RuntimeDirectory)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "  file providers: %d | runtime providers: %d\n", len(result.FileSnapshot.SyncedProviders), len(result.RuntimeSnapshot.Providers))
	if len(result.DoctorIssues) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  doctor issues: %d\n", len(result.DoctorIssues))
	}
}
