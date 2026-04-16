package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/opencode"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate ops config (static checks, no upstream requests)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			issues := cfg.Validate()
			ok := len(issues) == 0
			if ok {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ config loaded: %s\n", cfg.Path())
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "✗ config has %d issue(s):\n", len(issues))
				for _, e := range issues {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  providers: %d\n", len(cfg.Providers))
			fmt.Fprintf(cmd.OutOrStdout(), "  aliases:   %d\n", len(cfg.Aliases))

			// Preview resolved opencode config target
			path, existed := opencode.ResolveGlobalConfigPath()
			marker := "(will be created)"
			if existed {
				marker = "(exists)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  opencode config target: %s %s\n", path, marker)

			fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
			if !ok {
				return fmt.Errorf("%d config issue(s)", len(issues))
			}
			return nil
		},
	}
}
