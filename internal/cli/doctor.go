package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
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
			printDoctorSummary(cmd, report)
			if err != nil {
				return err
			}
			return nil
		},
	}
}

func printDoctorSummary(cmd *cobra.Command, report app.DoctorReport) {
	marker := "(will be created)"
	if report.OpenCodeTargetFound {
		marker = "(exists)"
	}
	status := "✓"
	if !report.OK {
		status = "!"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s config loaded: %s\n", status, report.ConfigPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  providers: %d\n", report.ProviderCount)
	fmt.Fprintf(cmd.OutOrStdout(), "  aliases:   %d\n", report.AliasCount)
	fmt.Fprintf(cmd.OutOrStdout(), "  opencode config target: %s %s\n", report.OpenCodeTargetPath, marker)
	fmt.Fprintf(cmd.OutOrStdout(), "  runtime: %s", report.RuntimeBaseURL)
	if report.RuntimeDirectory != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " [dir=%s]", report.RuntimeDirectory)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "  sync protocols: %s\n", strings.Join(report.SyncProtocols, ", "))
	fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s\n", report.ProxyBindAddress)
	if len(report.Issues) == 0 {
		return
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(cmd.OutOrStdout(), "  - [%s/%s] %s\n", issue.Severity, issue.Code, issue.Message)
		if issue.Protocol != "" || issue.ProviderKey != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    scope: protocol=%s provider=%s alias=%s\n", issue.Protocol, issue.ProviderKey, issue.Alias)
		}
		if issue.Path != "" || issue.Directory != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    location: path=%s dir=%s\n", issue.Path, issue.Directory)
		}
		if issue.Expected != "" || issue.Actual != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    expected=%s actual=%s\n", issue.Expected, issue.Actual)
		}
		if issue.ActionHint != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    hint: %s\n", issue.ActionHint)
		}
	}
}
