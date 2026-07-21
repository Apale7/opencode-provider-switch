// Package cli wires the ocswitch cobra command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/tui"
)

// configPath is populated from the global --config flag.
var configPath string

var isInteractiveTerminal = func() bool {
	stdin, err := os.Stdin.Stat()
	if err != nil || stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	stdout, err := os.Stdout.Stat()
	return err == nil && stdout.Mode()&os.ModeCharDevice != 0
}

var runTUI = tui.Run

// loadCfg opens the active ocswitch config, with the selected path.
func loadCfg() (*config.Config, error) {
	return config.Load(configPath)
}

func appService() *app.Service {
	return app.NewService(configPath)
}

// NewRootCmd builds the root ocswitch command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   config.AppName,
		Short: "OpenCode Provider Switch CLI: local alias + failover proxy for OpenCode",
		Long: `ocswitch is a local OpenCode proxy that exposes stable aliases as ocswitch/<alias>
while routing each alias to one or more upstream provider/model targets.

Typical workflow:
1. add or import upstream providers into local ocswitch config
2. create aliases and bind ordered targets
3. run doctor for static validation
4. run opencode sync to write provider.ocswitch into OpenCode config
5. run serve to accept local /v1/responses traffic

ocswitch writes only its own local config unless a command explicitly says it also
writes OpenCode config. For exact command behavior, defaults, and side effects,
prefer command-local --help over README summaries.`,
		Example: `  ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-example
  ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
  ocswitch alias bind --alias gpt-5.4 --model su8/gpt-5.4
  ocswitch doctor
  ocswitch opencode sync
  ocswitch serve

  ocswitch provider import-opencode
  ocswitch provider list
  ocswitch opencode sync --dry-run`,
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractiveTerminal() {
				printNonInteractiveRootHelp(cmd)
				return nil
			}
			return runTUI(configPath)
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", fmt.Sprintf("path to %s config.json (default: $%s, else $XDG_CONFIG_HOME/%s/config.json, else ~/.config/%s/config.json)", config.AppName, config.ConfigEnvVar, config.ConfigDirName, config.ConfigDirName))
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "emit a single machine-readable JSON envelope on stdout for supported commands")

	root.AddCommand(newServeCmd())
	root.AddCommand(newServerCmd(version))
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newProviderCmd())
	root.AddCommand(newAliasCmd())
	root.AddCommand(newRewriteCmd())
	root.AddCommand(newOpencodeCmd())
	return root
}

func printNonInteractiveRootHelp(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "ocswitch config UI requires an interactive terminal.")
	fmt.Fprintln(cmd.OutOrStdout(), "Use explicit commands in scripts, for example:")
	fmt.Fprintln(cmd.OutOrStdout(), "  ocswitch provider list")
	fmt.Fprintln(cmd.OutOrStdout(), "  ocswitch alias list")
	fmt.Fprintln(cmd.OutOrStdout(), "  ocswitch doctor")
	fmt.Fprintln(cmd.OutOrStdout(), "  ocswitch opencode sync --dry-run")
	fmt.Fprintln(cmd.OutOrStdout(), "Run ocswitch --help for full command help.")
}

// fail prints to stderr and exits 1.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
