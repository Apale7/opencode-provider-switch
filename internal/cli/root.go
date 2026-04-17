// Package cli wires the olpx cobra command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anomalyco/opencode-provider-switch/internal/config"
)

// configPath is populated from the global --config flag.
var configPath string

// loadCfg opens the active olpx config, with the selected path.
func loadCfg() (*config.Config, error) {
	return config.Load(configPath)
}

// NewRootCmd builds the root olpx command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "olpx",
		Short:         "OpenCode LocalProxy CLI: local alias + failover proxy for OpenCode",
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to olpx config.json (default: $XDG_CONFIG_HOME/olpx/config.json)")

	root.AddCommand(newServeCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newProviderCmd())
	root.AddCommand(newAliasCmd())
	root.AddCommand(newOpencodeCmd())
	return root
}

// fail prints to stderr and exits 1.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
