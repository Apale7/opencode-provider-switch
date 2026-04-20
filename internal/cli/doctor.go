package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate ocswitch config (static checks, no upstream requests)",
		Long: `doctor performs static validation for local ocswitch config and the expected
OpenCode sync result.

It loads local ocswitch config, checks alias/provider consistency, resolves the
	default OpenCode sync target, and validates what provider.ocswitch would look like
there. It does not send any real requests to upstream providers and does not
consume model quota.

Run doctor before opencode sync or serve whenever you changed providers,
aliases, or local server settings.`,
		Example: `  ocswitch doctor
  ocswitch --config /path/to/config.json doctor`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := appService().RunDoctor(cmd.Context())
			if err != nil {
				for _, issue := range report.Issues {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", issue.Message)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  providers: %d\n", report.ProviderCount)
				fmt.Fprintf(cmd.OutOrStdout(), "  aliases:   %d\n", report.AliasCount)
				marker := "(will be created)"
				if report.OpenCodeTargetFound {
					marker = "(exists)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  opencode config target: %s %s\n", report.OpenCodeTargetPath, marker)
				fmt.Fprintf(cmd.OutOrStdout(), "  sync protocols: %s\n", strings.Join(report.SyncProtocols, ", "))
				fmt.Fprintf(cmd.OutOrStdout(), "  synced providers preview: valid=%v\n", report.OK)
				fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s\n", report.ProxyBindAddress)
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ config loaded: %s\n", report.ConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "  providers: %d\n", report.ProviderCount)
			fmt.Fprintf(cmd.OutOrStdout(), "  aliases:   %d\n", report.AliasCount)
			marker := "(will be created)"
			if report.OpenCodeTargetFound {
				marker = "(exists)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  opencode config target: %s %s\n", report.OpenCodeTargetPath, marker)
			fmt.Fprintf(cmd.OutOrStdout(), "  sync protocols: %s\n", strings.Join(report.SyncProtocols, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "  synced providers preview: valid=%v\n", report.OK)
			fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s\n", report.ProxyBindAddress)
			return nil
		},
	}
}
