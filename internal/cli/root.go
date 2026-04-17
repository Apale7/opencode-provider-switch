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
		Use:   "olpx",
		Short: "OpenCode LocalProxy CLI: local alias + failover proxy for OpenCode",
		Long: `olpx is a local OpenCode proxy that exposes stable aliases as olpx/<alias>
while routing each alias to one or more upstream provider/model targets.

Typical workflow:
1. add or import upstream providers into local olpx config
2. create aliases and bind ordered targets
3. run doctor for static validation
4. run opencode sync to write provider.olpx into OpenCode config
5. run serve to accept local /v1/responses traffic

olpx writes only its own local config unless a command explicitly says it also
writes OpenCode config. For exact command behavior, defaults, and side effects,
prefer command-local --help over README summaries.`,
		Example: `  olpx provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  olpx alias add --name gpt-5.4 --display-name "GPT 5.4"
  olpx alias bind --alias gpt-5.4 --provider su8 --model gpt-5.4
  olpx doctor
  olpx opencode sync
  olpx serve

  olpx provider import-opencode
  olpx provider list
  olpx opencode sync --dry-run`,
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to olpx config.json (default: $OLPX_CONFIG, else $XDG_CONFIG_HOME/olpx/config.json, else ~/.config/olpx/config.json)")

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
