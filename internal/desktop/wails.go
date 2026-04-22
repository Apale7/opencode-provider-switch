//go:build desktop_wails

package desktop

import (
	"context"
	"fmt"
	"io/fs"

	frontendassets "github.com/Apale7/opencode-provider-switch/frontend"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func RunWails(configPath string, version string) error {
	assets, err := frontendassets.DistFS()
	if err != nil {
		return err
	}

	instance := New(configPath)
	instance.SetVersion(version)

	return wails.Run(&options.App{
		Title:     "ocswitch desktop",
		Width:     1280,
		Height:    880,
		MinWidth:  1240,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets: mustFS(assets),
		},
		Bind: []any{instance},
		OnStartup: func(ctx context.Context) {
			instance.Startup(ctx)
		},
		OnBeforeClose: func(ctx context.Context) bool {
			return instance.BeforeClose(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			instance.Shutdown(ctx)
		},
		Linux: &linux.Options{
			ProgramName: "ocswitch-desktop",
		},
		Windows: &windows.Options{
			DisableWindowIcon: false,
		},
	})
}

func mustFS(assets fs.FS) fs.FS {
	return assets
}

func WailsProjectName() string {
	return fmt.Sprintf("%s desktop", "ocswitch")
}
