package cli

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/proxy"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the local olpx proxy (alias -> failover upstream)",
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
			srv := proxy.New(cfg)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return srv.ListenAndServe(ctx)
		},
	}
}
