package desktop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/app"
)

func TestAutoStartSyncLinuxWritesDesktopEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only autostart behavior")
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	auto := NewAutoStart(nil)
	if err := auto.Sync(context.Background(), app.DesktopPrefsView{LaunchAtLogin: true}); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	entry, err := auto.entryPath()
	if err != nil {
		t.Fatalf("entryPath() error = %v", err)
	}
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[Desktop Entry]") {
		t.Fatalf("desktop entry missing header: %q", text)
	}
	if !strings.Contains(text, "Name=ocswitch desktop") {
		t.Fatalf("desktop entry missing app name: %q", text)
	}

	if err := auto.Sync(context.Background(), app.DesktopPrefsView{}); err != nil {
		t.Fatalf("Sync(remove) error = %v", err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("autostart entry still exists at %s", entry)
	}
}

func TestShellQuoteEscapesDoubleQuotes(t *testing.T) {
	t.Parallel()
	quoted := shellQuote(filepath.Join(`/tmp/demo"path`, `bin`))
	if !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `"`) {
		t.Fatalf("shellQuote() = %q", quoted)
	}
	if !strings.Contains(quoted, `\\"`) {
		t.Fatalf("shellQuote() did not escape quotes: %q", quoted)
	}
}

func TestEntryPathForWindowsUsesStartupFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)

	auto := NewAutoStart(nil)
	path, err := auto.entryPathForOS("windows")
	if err != nil {
		t.Fatalf("entryPathForOS(windows) error = %v", err)
	}
	want := filepath.Join(root, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "ocswitch-desktop.cmd")
	if path != want {
		t.Fatalf("entryPathForOS(windows) = %q, want %q", path, want)
	}
}

func TestWindowsAutostartScriptQuotesArgs(t *testing.T) {
	t.Parallel()
	script := windowsAutostartScript(`C:\Program Files\ocswitch\ocswitch-desktop.exe`, `C:\Users\demo\.config\ocswitch\config.json`)
	if !strings.Contains(script, `start "" "C:\Program Files\ocswitch\ocswitch-desktop.exe" --config "C:\Users\demo\.config\ocswitch\config.json"`) {
		t.Fatalf("windowsAutostartScript() missing command: %q", script)
	}
}
