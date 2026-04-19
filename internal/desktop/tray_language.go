//go:build desktop_wails

package desktop

import (
	"os"
	"strings"
)

var systemTrayLanguage = detectSystemTrayLanguage

func trayLanguage(preference string) string {
	if language := normalizeTrayLanguage(strings.TrimSpace(preference)); language != "" {
		return language
	}
	if language := normalizeTrayLanguage(systemTrayLanguage()); language != "" {
		return language
	}
	for _, value := range []string{os.Getenv("LC_ALL"), os.Getenv("LANG"), os.Getenv("LANGUAGE")} {
		if language := normalizeTrayLanguage(value); language != "" {
			return language
		}
	}
	return "en-US"
}

func normalizeTrayLanguage(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "zh") {
		return "zh-CN"
	}
	if strings.HasPrefix(lower, "en") {
		return "en-US"
	}
	return ""
}
