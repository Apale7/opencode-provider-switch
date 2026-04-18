package cli

import "testing"

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
