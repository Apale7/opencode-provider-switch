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
		Long: `serve starts the long-running local olpx proxy using the current local config.

It reads local alias/provider configuration, validates that config, and then
accepts OpenAI Responses traffic at the configured local base URL. With default
settings the proxy listens on http://127.0.0.1:9982/v1 and expects the local API
key olpx-local.

serve does not rewrite config files. Run doctor and opencode sync first so
OpenCode can see the same aliases that the proxy can route.`,
		Example: `  olpx serve
  olpx --config /path/to/config.json serve`,
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
