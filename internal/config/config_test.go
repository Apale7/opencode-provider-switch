package config

import (
	"strings"
	"testing"
)

func TestValidateProviderBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "valid exact", input: "https://example.com/v1"},
		{name: "valid trailing slash", input: "https://example.com/v1/"},
		{name: "valid trimmed", input: "  https://example.com/v1/  "},
		{name: "missing", input: "", wantErr: "missing base_url"},
		{name: "missing v1", input: "https://example.com/api", wantErr: "base_url must end with /v1"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProviderBaseURL(tt.input)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestAvailableTargetsSkipsDisabledAndMissingProviders(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true},
		},
	}
	alias := Alias{
		Alias:   "gpt-5.4",
		Enabled: true,
		Targets: []Target{
			{Provider: "p1", Model: "up-1", Enabled: true},
			{Provider: "p2", Model: "up-2", Enabled: true},
			{Provider: "missing", Model: "up-3", Enabled: true},
			{Provider: "p1", Model: "up-4", Enabled: false},
		},
	}

	targets := cfg.AvailableTargets(alias)
	if len(targets) != 1 {
		t.Fatalf("available targets = %#v, want exactly one", targets)
	}
	if targets[0].Provider != "p1" || targets[0].Model != "up-1" {
		t.Fatalf("available target = %#v, want p1/up-1", targets[0])
	}
}

func TestAvailableAliasNamesOnlyReturnsRoutableAliases(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{
			{ID: "p1", BaseURL: "https://p1.example.com/v1"},
			{ID: "p2", BaseURL: "https://p2.example.com/v1", Disabled: true},
		},
		Aliases: []Alias{
			{Alias: "ok", Enabled: true, Targets: []Target{{Provider: "p1", Model: "up-1", Enabled: true}}},
			{Alias: "provider-disabled", Enabled: true, Targets: []Target{{Provider: "p2", Model: "up-2", Enabled: true}}},
			{Alias: "alias-disabled", Enabled: false, Targets: []Target{{Provider: "p1", Model: "up-3", Enabled: true}}},
		},
	}

	names := cfg.AvailableAliasNames()
	if len(names) != 1 || names[0] != "ok" {
		t.Fatalf("available alias names = %#v, want [ok]", names)
	}
}

func TestValidateRejectsDefaultKeyOnNonLoopbackHost(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: Server{Host: "0.0.0.0", Port: 9982, APIKey: DefaultLocalAPIKey},
	}

	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", errs)
	}
	if !strings.Contains(errs[0].Error(), "must not use the default value") {
		t.Fatalf("Validate() error = %q", errs[0].Error())
	}
}

func TestValidateReportsAliasWithoutAvailableTargets(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Server{Host: "127.0.0.1", Port: 9982, APIKey: DefaultLocalAPIKey},
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", Disabled: true}},
		Aliases: []Alias{{
			Alias:   "gpt-5.4",
			Enabled: true,
			Targets: []Target{{Provider: "p1", Model: "up-1", Enabled: true}},
		}},
	}

	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", errs)
	}
	if got := errs[0].Error(); got != `alias "gpt-5.4" has no available targets` {
		t.Fatalf("Validate() error = %q", got)
	}
}
