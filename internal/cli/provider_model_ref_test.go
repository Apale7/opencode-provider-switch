package cli

import (
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestParseProviderModelRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantProvider string
		wantModel    string
		wantOK       bool
	}{
		{name: "simple", input: "codex/GPT-5.4", wantProvider: "codex", wantModel: "GPT-5.4", wantOK: true},
		{name: "trimmed", input: "  codex / GPT-5.4  ", wantProvider: "codex", wantModel: "GPT-5.4", wantOK: true},
		{name: "model contains slash", input: "relay/openrouter/google/gemini-2.5-pro", wantProvider: "relay", wantModel: "openrouter/google/gemini-2.5-pro", wantOK: true},
		{name: "missing slash", input: "gpt-5.4", wantOK: false},
		{name: "missing provider", input: "/gpt-5.4", wantOK: false},
		{name: "missing model", input: "codex/", wantOK: false},
		{name: "nested provider", input: "a/b/c", wantProvider: "a", wantModel: "b/c", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, model, ok := parseProviderModelRef(tt.input)
			if ok != tt.wantOK || provider != tt.wantProvider || model != tt.wantModel {
				t.Fatalf("parseProviderModelRef(%q) = %q, %q, %v", tt.input, provider, model, ok)
			}
		})
	}
}

func TestResolveAliasTargetFlags(t *testing.T) {
	t.Parallel()

	t.Run("default combined form", func(t *testing.T) {
		provider, model, group, err := resolveAliasTargetFlags("", "", "su8/gpt-5.4")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if provider != "su8" || model != "gpt-5.4" || group != config.DefaultGroupID {
			t.Fatalf("got %q %q %q", provider, model, group)
		}
	})

	t.Run("default explicit provider keeps slash model", func(t *testing.T) {
		provider, model, group, err := resolveAliasTargetFlags("relay", "", "openrouter/google/gemini")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if provider != "relay" || model != "openrouter/google/gemini" || group != config.DefaultGroupID {
			t.Fatalf("got %q %q %q", provider, model, group)
		}
	})

	t.Run("non-default requires provider", func(t *testing.T) {
		_, _, _, err := resolveAliasTargetFlags("", "premium", "su8/gpt-5.4")
		if err == nil || !strings.Contains(err.Error(), "--provider is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("non-default explicit slash model literal", func(t *testing.T) {
		provider, model, group, err := resolveAliasTargetFlags("relay", "premium", "openrouter/google/gemini")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if provider != "relay" || model != "openrouter/google/gemini" || group != "premium" {
			t.Fatalf("got %q %q %q", provider, model, group)
		}
	})
}
