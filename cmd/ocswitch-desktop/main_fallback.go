//go:build !desktop_wails

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/desktop"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", fmt.Sprintf("path to %s config.json (default: %s)", config.AppName, config.DefaultPath()))
	listenAddr := flag.String("listen", "127.0.0.1:0", "listen address for the local desktop control panel")
	noOpen := flag.Bool("no-open", false, "do not open the control panel in the default browser")
	flag.Parse()

	err := desktop.Run(desktop.RunOptions{
		ConfigPath:  *configPath,
		Version:     version,
		ListenAddr:  *listenAddr,
		OpenBrowser: !*noOpen,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
