// Command ocswitch: local alias + failover proxy for OpenCode.
package main

import (
	"fmt"
	"os"

	"github.com/Apale7/opencode-provider-switch/internal/cli"
	appversion "github.com/Apale7/opencode-provider-switch/internal/version"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.NewRootCmd(appversion.Resolve(version)).Execute(); err != nil {
		// cobra already prints the error; ensure non-zero exit
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
}
