//go:build desktop_wails

package desktop

import "testing"

func TestTrayLanguageHonorsExplicitPreference(t *testing.T) {
	original := systemTrayLanguage
	systemTrayLanguage = func() string { return "en-US" }
	t.Cleanup(func() { systemTrayLanguage = original })

	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "")
	t.Setenv("LANGUAGE", "")

	if got := trayLanguage("zh-CN"); got != "zh-CN" {
		t.Fatalf("trayLanguage(zh-CN) = %q, want zh-CN", got)
	}
}

func TestTrayLanguageUsesSystemLanguageBeforeEnv(t *testing.T) {
	original := systemTrayLanguage
	systemTrayLanguage = func() string { return "zh-CN" }
	t.Cleanup(func() { systemTrayLanguage = original })

	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "")
	t.Setenv("LANGUAGE", "")

	if got := trayLanguage("system"); got != "zh-CN" {
		t.Fatalf("trayLanguage(system) = %q, want zh-CN", got)
	}
}

func TestTrayLanguageFallsBackToEnv(t *testing.T) {
	original := systemTrayLanguage
	systemTrayLanguage = func() string { return "" }
	t.Cleanup(func() { systemTrayLanguage = original })

	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LANGUAGE", "")

	if got := trayLanguage("system"); got != "zh-CN" {
		t.Fatalf("trayLanguage(system) = %q, want zh-CN", got)
	}
}
