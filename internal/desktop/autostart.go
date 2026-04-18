package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
)

// AutoStart manages real launch-at-login integration where the platform permits
// it. Linux uses XDG autostart; Windows uses Startup folder scripts.
type AutoStart struct {
	service *app.Service
	ctx     context.Context
}

func NewAutoStart(service *app.Service) *AutoStart {
	return &AutoStart{service: service}
}

func (a *AutoStart) Attach(ctx context.Context) {
	a.ctx = ctx
}

func (a *AutoStart) Detach() {
	a.ctx = nil
}

func (a *AutoStart) Sync(ctx context.Context, prefs app.DesktopPrefsView) error {
	_ = ctx
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil
	}
	entryPath, err := a.entryPathForOS(runtime.GOOS)
	if err != nil {
		return err
	}
	if prefs.LaunchAtLogin {
		return a.writeEntry(entryPath, runtime.GOOS)
	}
	if err := os.Remove(entryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove autostart entry: %w", err)
	}
	return nil
}

func (a *AutoStart) entryPath() (string, error) {
	return a.entryPathForOS(runtime.GOOS)
}

func (a *AutoStart) entryPathForOS(goos string) (string, error) {
	switch goos {
	case "linux":
		return a.linuxEntryPath()
	case "windows":
		return a.windowsEntryPath()
	default:
		return "", fmt.Errorf("autostart unsupported on %s", goos)
	}
}

func (a *AutoStart) linuxEntryPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for autostart: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "autostart", "ocswitch-desktop.desktop"), nil
}

func (a *AutoStart) windowsEntryPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("APPDATA"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for autostart: %w", err)
		}
		base = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(base, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "ocswitch-desktop.cmd"), nil
}

func (a *AutoStart) writeEntry(path string, goos string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir autostart dir: %w", err)
	}
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve desktop executable: %w", err)
	}
	configPath := config.DefaultPath()
	if a.service != nil {
		configPath = a.service.ConfigPath()
	}
	var content string
	switch goos {
	case "linux":
		content = linuxAutostartEntry(execPath, configPath)
	case "windows":
		content = windowsAutostartScript(execPath, configPath)
	default:
		return fmt.Errorf("autostart unsupported on %s", goos)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write autostart entry: %w", err)
	}
	return nil
}

func linuxAutostartEntry(execPath string, configPath string) string {
	content := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=ocswitch desktop",
		"Comment=OpenCode provider switch desktop shell",
		fmt.Sprintf("Exec=%s --config %s", shellQuote(execPath), shellQuote(configPath)),
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"Categories=Network;Development;",
		"StartupNotify=false",
		"",
	}, "\n")
	return content
}

func shellQuote(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", `"`, `\\"`)
	return `"` + replacer.Replace(value) + `"`
}

func windowsAutostartScript(execPath string, configPath string) string {
	return strings.Join([]string{
		"@echo off",
		fmt.Sprintf("start \"\" %s --config %s", cmdQuote(execPath), cmdQuote(configPath)),
		"",
	}, "\r\n")
}

func cmdQuote(value string) string {
	replacer := strings.NewReplacer(`"`, `""`)
	return `"` + replacer.Replace(value) + `"`
}
