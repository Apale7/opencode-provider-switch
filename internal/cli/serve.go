package cli

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the local ocswitch proxy (alias -> failover upstream)",
		Long: `serve starts the long-running local ocswitch proxy using the current local config.

It reads local alias/provider configuration, validates that config, and then
accepts OpenAI Responses traffic at the configured local base URL. With default
settings the proxy listens on http://127.0.0.1:9982/v1 and expects the local API
key ocswitch-local.

serve does not rewrite config files. Run doctor and opencode sync first so
OpenCode can see the same aliases that the proxy can route.`,
		Example: `  ocswitch serve
  ocswitch --config /path/to/config.json serve`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			svc := appService()
			if err := svc.StartProxy(context.Background()); err != nil {
				return err
			}
			go func() {
				<-ctx.Done()
				_ = svc.StopProxy(context.Background())
			}()
			return svc.WaitProxy(context.Background())
		},
	}
}
