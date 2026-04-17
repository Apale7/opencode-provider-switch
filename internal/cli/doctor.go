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

			path, existed := opencode.ResolveGlobalConfigPath()
			raw, err := opencode.Load(path)
			if err != nil {
				issues = append(issues, fmt.Errorf("load opencode config target: %w", err))
			} else {
				aliasNames := []string{}
				for _, a := range cfg.Aliases {
					if !a.Enabled {
						continue
					}
					aliasNames = append(aliasNames, a.Alias)
				}
				baseURL := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
				opencode.EnsureOpsProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
				if err := opencode.ValidateOpsProvider(raw, baseURL, cfg.Server.APIKey, aliasNames); err != nil {
					issues = append(issues, fmt.Errorf("opencode provider.ops invalid: %w", err))
				}
			}
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
			marker := "(will be created)"
			if existed {
				marker = "(exists)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  opencode config target: %s %s\n", path, marker)
			fmt.Fprintf(cmd.OutOrStdout(), "  provider.ops preview: valid=%v\n", ok)

			fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
			if !ok {
				return fmt.Errorf("%d config issue(s)", len(issues))
			}
			return nil
		},
	}
}
