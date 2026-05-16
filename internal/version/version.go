package version

import (
	"runtime/debug"
	"strings"
)

// Resolve returns linker-injected version, then module build info, then dev.
func Resolve(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && value != "dev" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				return "dev-" + setting.Value[:7]
			}
		}
	}
	return "dev"
}
