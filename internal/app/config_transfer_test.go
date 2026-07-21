package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestConfigExportIncludesRoutingPolicyFields(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{{ID: "p1", BaseURL: "https://example.com/v1"}}
	cfg.ProviderPriority = []string{"p1"}
	cfg.AutoAliasEnabled = false

	content, err := marshalConfigContent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["auto_alias_enabled"]) != "false" {
		t.Fatalf("auto_alias_enabled=%s", fields["auto_alias_enabled"])
	}
	var priority []string
	if err := json.Unmarshal(fields["provider_priority"], &priority); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(priority, []string{"p1"}) {
		t.Fatalf("provider_priority=%v", priority)
	}
	for _, field := range []string{"server", "admin", "desktop", "providers", "aliases", "request_rewrite_rules", "provider_priority", "auto_alias_enabled"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("export omitted typed top-level field %q", field)
		}
	}
	if len(fields) != 8 {
		t.Fatalf("export fields=%v", fields)
	}
	for _, field := range []string{"providers", "aliases", "request_rewrite_rules", "provider_priority"} {
		if strings.TrimSpace(string(fields[field])) == "null" {
			t.Errorf("exported array field %q as null", field)
		}
	}
}

func TestConfigImportRejectsAmbiguousIdentityBeforePersist(t *testing.T) {
	tests := []struct {
		name    string
		content string
		code    string
	}{
		{"provider", `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"dup","base_url":"https://one.example/v1"},{"id":"dup","base_url":"https://two.example/v1"}],"aliases":[]}`, "provider_identity_ambiguous"},
		{"alias", `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1"}],"aliases":[{"alias":"dup","enabled":true,"targets":[]},{"alias":"dup","enabled":false,"targets":[]}]}`, "alias_identity_ambiguous"},
		{"alias target", `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1"}],"aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","model":"m","enabled":true},{"provider":"p","model":"m","enabled":true}]}]}`, "alias_target_identity_ambiguous"},
		{"rewrite rule", `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1"}],"aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","model":"m","enabled":true}]}],"request_rewrite_rules":[{"name":"dup","alias":"a","providers":[]},{"name":"dup","alias":"a","providers":[]}]}`, "rewrite_rule_identity_ambiguous"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			_, err := NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: test.content})
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("error=%v want code=%q", err, test.code)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("rejected import wrote file: %v", statErr)
			}
		})
	}
}

func TestConfigImportRoutingPolicyPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers = []config.Provider{
		{ID: "p1", BaseURL: "https://p1.example/v1"},
		{ID: "p2", BaseURL: "https://p2.example/v1"},
	}
	cfg.ProviderPriority = []string{"p2", "p1"}
	cfg.AutoAliasEnabled = false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	withoutPolicy := `{
  "server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "providers":[
    {"id":"p1","base_url":"https://p1.example/v1"},
    {"id":"p2","base_url":"https://p2.example/v1"}
  ],
  "aliases":[]
}`
	service := NewService(path)
	if _, err := service.ImportConfig(context.Background(), ConfigImportInput{Content: withoutPolicy}); err != nil {
		t.Fatal(err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.ProviderPriority, []string{"p2", "p1"}) || after.AutoAliasEnabled {
		t.Fatalf("missing fields not preserved: priority=%v auto=%v", after.ProviderPriority, after.AutoAliasEnabled)
	}

	withPolicy := strings.TrimSuffix(withoutPolicy, "\n}") + `,
  "provider_priority":[],
  "auto_alias_enabled":false
}`
	if _, err := service.ImportConfig(context.Background(), ConfigImportInput{Content: withPolicy}); err != nil {
		t.Fatal(err)
	}
	after, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.ProviderPriority, []string{"p1", "p2"}) || after.AutoAliasEnabled {
		t.Fatalf("explicit fields not applied: priority=%v auto=%v", after.ProviderPriority, after.AutoAliasEnabled)
	}
}

func TestConfigImportRejectsNullAndUnknownTopLevelFields(t *testing.T) {
	base := map[string]json.RawMessage{
		"server":    json.RawMessage(`{"host":"127.0.0.1","port":9982}`),
		"providers": json.RawMessage(`[]`),
		"aliases":   json.RawMessage(`[]`),
	}
	tests := []struct {
		name, field, value, want string
	}{
		{"null", "provider_priority", `null`, `must not be null`},
		{"unknown", "future_field", `{}`, `unknown field`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := make(map[string]json.RawMessage, len(base)+1)
			for key, value := range base {
				raw[key] = value
			}
			raw[test.field] = json.RawMessage(test.value)
			content, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			_, err = NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: string(content)})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("rejected import wrote file: %v", statErr)
			}
		})
	}
}

func TestConfigImportRejectsDuplicateNestedNullAndUnknownFields(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{
			name:    "duplicate nested key",
			content: `{"server":{"host":"127.0.0.1","host":"0.0.0.0","port":9982},"providers":[],"aliases":[]}`,
			want:    "duplicate field",
		},
		{
			name:    "nested null",
			content: `{"server":{"host":"127.0.0.1","port":9982,"api_key":null},"providers":[],"aliases":[]}`,
			want:    "must not be null",
		},
		{
			name:    "nested unknown",
			content: `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1","future":true}],"aliases":[]}`,
			want:    "unknown field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			_, err := NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: test.content})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("rejected import wrote file: %v", statErr)
			}
		})
	}
}
