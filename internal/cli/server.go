package cli

import (
	"github.com/Apale7/opencode-provider-switch/internal/server"
	"github.com/spf13/cobra"
)

func newServerCmd(version string) *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the ocswitch server web admin",
		Long: `server starts the web administration UI and API for remote/browser use.

It reuses the desktop GUI web pages, stores data in the local ocswitch config
and SQLite trace database, and does not use desktop-only tray, notification, or

On first start, server generates a strong admin API key, stores it as plaintext
in admin.api_key in the local config, and prints it in the log. Keep the config
file private because it also contains upstream provider keys.`,
		Example: `  ocswitch server
  ocswitch server --host 127.0.0.1 --port 9983
  ocswitch --config /path/to/config.json server --host 0.0.0.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return server.Run(server.RunOptions{
				ConfigPath: configPath,
				Version:    version,
				Host:       host,
				Port:       port,
			})
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "admin web listen host (default: admin.host or 127.0.0.1)")
	cmd.Flags().IntVar(&port, "port", 0, "admin web listen port (default: admin.port or 9983)")
	return cmd
}
