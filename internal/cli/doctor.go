package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/opencode"
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
				aliasNames := cfg.AvailableAliasNames()
				baseURL := fmt.Sprintf("http://%s:%d/v1", cfg.Server.Host, cfg.Server.Port)
				opencode.EnsureOcswitchProvider(raw, baseURL, cfg.Server.APIKey, aliasNames)
				if err := opencode.ValidateOcswitchProvider(raw, baseURL, cfg.Server.APIKey, aliasNames); err != nil {
					issues = append(issues, fmt.Errorf("opencode provider.ocswitch invalid: %w", err))
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
			fmt.Fprintf(cmd.OutOrStdout(), "  provider.ocswitch preview: valid=%v\n", ok)

			fmt.Fprintf(cmd.OutOrStdout(), "  proxy bind: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
			if !ok {
				return fmt.Errorf("%d config issue(s)", len(issues))
			}
			return nil
		},
	}
}
