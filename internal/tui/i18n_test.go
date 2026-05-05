package tui

import "testing"

func TestMessagesHaveChineseCoverage(t *testing.T) {
	for key := range messages[langEN] {
		if messages[langZH][key] == "" {
			t.Fatalf("missing zh-CN message for %s", key)
		}
	}
}

func TestNormalizeTUILanguageDefaultsToEnglish(t *testing.T) {
	if got := normalizeTUILanguage("zh-CN"); got != langZH {
		t.Fatalf("normalize zh-CN = %q", got)
	}
	if got := normalizeTUILanguage("system"); got != langEN {
		t.Fatalf("normalize system = %q, want %q", got, langEN)
	}
	if got := normalizeTUILanguage(""); got != langEN {
		t.Fatalf("normalize empty = %q, want %q", got, langEN)
	}
}
