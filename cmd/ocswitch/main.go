// Command ocswitch: local alias + failover proxy for OpenCode.
package main

import (
	"fmt"
	"os"

	"github.com/Apale7/opencode-provider-switch/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.NewRootCmd(version).Execute(); err != nil {
		// cobra already prints the error; ensure non-zero exit
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
}
