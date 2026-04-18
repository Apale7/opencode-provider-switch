//go:build desktop_wails

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
	flag.Parse()

	if err := desktop.RunWails(*configPath, version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
