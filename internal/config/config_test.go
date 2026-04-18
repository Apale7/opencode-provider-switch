package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestNormalizeProviderBaseURL(t *testing.T) {
	t.Parallel()

	if got := NormalizeProviderBaseURL("  https://example.com/v1/  "); got != "https://example.com/v1" {
		t.Fatalf("NormalizeProviderBaseURL() = %q", got)
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

func TestValidateRejectsInvalidModelsSource(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Server{Host: "127.0.0.1", Port: 9982, APIKey: DefaultLocalAPIKey},
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", ModelsSource: "manual"}},
	}

	err := cfg.Validate()
	if len(err) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", err)
	}
	if got := err[0].Error(); got != `provider "p1" has invalid models_source "manual"` {
		t.Fatalf("Validate() error = %q", got)
	}
}

func TestValidateRejectsModelsSourceWithoutModels(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server:    Server{Host: "127.0.0.1", Port: 9982, APIKey: DefaultLocalAPIKey},
		Providers: []Provider{{ID: "p1", BaseURL: "https://p1.example.com/v1", ModelsSource: "discovered"}},
	}

	err := cfg.Validate()
	if len(err) != 1 {
		t.Fatalf("Validate() errors = %v, want 1 error", err)
	}
	if got := err[0].Error(); got != `provider "p1" has models_source "discovered" but no models` {
		t.Fatalf("Validate() error = %q", got)
	}
}

func TestSavePreservesEmptyCollectionsAsArrays(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = t.TempDir() + "/config.json"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Providers == nil || loaded.Aliases == nil {
		t.Fatalf("round-trip nil slices: providers=%#v aliases=%#v", loaded.Providers, loaded.Aliases)
	}

	var raw map[string]any
	data, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := raw["providers"].([]any); !ok {
		t.Fatalf("providers JSON = %#v, want array", raw["providers"])
	}
	if _, ok := raw["aliases"].([]any); !ok {
		t.Fatalf("aliases JSON = %#v, want array", raw["aliases"])
	}
}

func TestSaveLinearizesConcurrentWriters(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.path = filepath.Join(t.TempDir(), "config.json")
	lockFile, err := os.OpenFile(cfg.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(lock): %v", err)
	}
	defer lockFile.Close()
	if err := lockTestFile(lockFile); err != nil {
		t.Fatalf("Flock(lock): %v", err)
	}

	startedFirst := make(chan struct{})
	secondDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		cfg.UpsertProvider(Provider{ID: "first", BaseURL: "https://first.example.com/v1"})
		close(startedFirst)
		if err := cfg.Save(); err != nil {
			t.Errorf("first Save() error = %v", err)
		}
	}()

	<-startedFirst
	time.Sleep(20 * time.Millisecond)

	go func() {
		defer wg.Done()
		cfg.UpsertProvider(Provider{ID: "second", BaseURL: "https://second.example.com/v1"})
		if err := cfg.Save(); err != nil {
			t.Errorf("second Save() error = %v", err)
		}
		close(secondDone)
	}()

	select {
	case <-secondDone:
		t.Fatal("second Save() completed before external file lock was released")
	case <-time.After(20 * time.Millisecond):
	}

	if err := unlockTestFile(lockFile); err != nil {
		t.Fatalf("Flock(unlock): %v", err)
	}

	wg.Wait()

	loaded, err := Load(cfg.path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.FindProvider("first") == nil {
		t.Fatalf("final config = %#v, want first provider persisted", loaded.Providers)
	}
	if loaded.FindProvider("second") == nil {
		t.Fatalf("final config = %#v, want latest provider persisted", loaded.Providers)
	}
}
