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
		// Classified CLI outcomes carry stable exit codes; others remain 1.
		code := cli.ExitCode(err)
		if code <= 0 {
			code = 1
		}
		// Avoid blank line noise for already-printed classified errors.
		if code == 1 {
			fmt.Fprintln(os.Stderr)
		}
		os.Exit(code)
	}
}
