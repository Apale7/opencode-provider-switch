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
	cfg.Providers = []config.Provider{{ID: "p1", BaseURL: "https://example.com/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}}
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
	if string(fields["schema_version"]) != "2" {
		t.Fatalf("schema_version=%s want 2", fields["schema_version"])
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
	for _, field := range []string{"schema_version", "server", "providers", "aliases", "provider_priority", "auto_alias_enabled"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("export omitted typed top-level field %q", field)
		}
	}
	for _, field := range []string{"providers", "aliases"} {
		if strings.TrimSpace(string(fields[field])) == "null" {
			t.Errorf("exported array field %q as null", field)
		}
	}
	// Canonical export must not use deprecated provider top-level group fields.
	var root map[string]any
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		t.Fatal(err)
	}
	providers, _ := root["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("providers len=%d", len(providers))
	}
	prov, _ := providers[0].(map[string]any)
	for _, banned := range []string{"protocol", "api_key", "api_keys", "models", "models_source"} {
		if _, ok := prov[banned]; ok {
			t.Errorf("export leaked legacy provider field %q", banned)
		}
	}
	groups, _ := prov["groups"].([]any)
	if len(groups) == 0 {
		t.Fatal("export missing provider groups")
	}
}

func TestImportConfigRejectsMaskedProviderGroupAPIKey(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Providers = []config.Provider{{
		ID: "p1", BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKeys: []string{"***"}}},
	}}
	raw, err := cfg.MarshalPersistent()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	_, err = NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: string(raw)})
	if err == nil || !strings.Contains(err.Error(), "masked API key placeholder") {
		t.Fatalf("ImportConfig() error = %v, want masked placeholder rejection", err)
	}
}

func TestConfigExportRoundTripLoadFromBytes(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{
		{
			ID: "p1", BaseURL: "https://example.com/v1",
			Groups: []config.ProviderGroup{
				{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-a", Models: []string{"m1"}},
				{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-b", Models: []string{"m1"}},
			},
		},
	}
	cfg.Aliases = []config.Alias{{
		Alias: "a1", Enabled: true, Protocol: config.ProtocolOpenAIResponses,
		Targets: []config.Target{
			{Provider: "p1", Group: config.DefaultGroupID, Model: "m1", Enabled: true},
			{Provider: "p1", Group: "premium", Model: "m1", Enabled: true},
		},
	}}
	cfg.ProviderPriority = []string{"p1"}
	cfg.AutoAliasEnabled = false

	content, err := marshalConfigContent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFromBytes(filepath.Join(t.TempDir(), "export.json"), []byte(content))
	if err != nil {
		t.Fatalf("LoadFromBytes(export): %v", err)
	}
	if loaded.SchemaVersion != config.CurrentSchemaVersion {
		t.Fatalf("schema_version=%d", loaded.SchemaVersion)
	}
	if len(loaded.Providers) != 1 || len(loaded.Providers[0].Groups) != 2 {
		t.Fatalf("providers/groups=%+v", loaded.Providers)
	}
	if len(loaded.Aliases) != 1 || len(loaded.Aliases[0].Targets) != 2 {
		t.Fatalf("aliases/targets=%+v", loaded.Aliases)
	}
	if loaded.Aliases[0].Targets[0].Group != config.DefaultGroupID || loaded.Aliases[0].Targets[1].Group != "premium" {
		t.Fatalf("target groups=%+v", loaded.Aliases[0].Targets)
	}
	// Round-trip marshal should be semantically stable.
	again, err := loaded.MarshalPersistent()
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("export marshal not stable after LoadFromBytes\nexport=%s\nagain=%s", content, again)
	}
}

func TestConfigExportImportRoundTripMultiGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	// Seed disk via import of a minimal valid config first.
	initial := `{
  "schema_version": 2,
  "server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "providers":[{"id":"seed","base_url":"https://seed.example/v1","groups":[{"id":"default","protocol":"openai-responses"}]}],
  "aliases":[],
  "provider_priority":["seed"],
  "auto_alias_enabled":false
}`
	service := NewService(path)
	if _, err := service.ImportConfig(context.Background(), ConfigImportInput{Content: initial}); err != nil {
		t.Fatal(err)
	}

	// Build multi-group export payload via marshalConfigContent.
	cfg := config.Default()
	cfg.Server.APIKey = "ocswitch-local"
	cfg.Providers = []config.Provider{{
		ID: "p1", BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-a", Models: []string{"m1"}},
			{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-b", Models: []string{"m1"}},
		},
	}}
	cfg.Aliases = []config.Alias{{
		Alias: "shared-model", Enabled: true, Protocol: config.ProtocolOpenAIResponses,
		Targets: []config.Target{
			{Provider: "p1", Group: config.DefaultGroupID, Model: "m1", Enabled: true},
			{Provider: "p1", Group: "premium", Model: "m1", Enabled: true},
		},
	}}
	cfg.ProviderPriority = []string{"p1"}
	cfg.AutoAliasEnabled = false
	content, err := marshalConfigContent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ImportConfig(context.Background(), ConfigImportInput{Content: content}); err != nil {
		t.Fatalf("import export content: %v", err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.SchemaVersion != config.CurrentSchemaVersion {
		t.Fatalf("schema_version=%d", after.SchemaVersion)
	}
	if len(after.Providers) != 1 || len(after.Providers[0].Groups) != 2 {
		t.Fatalf("providers=%+v", after.Providers)
	}
	if len(after.Aliases) != 1 || len(after.Aliases[0].Targets) != 2 {
		t.Fatalf("aliases=%+v", after.Aliases)
	}
	// Same provider+model under different groups must remain distinct after import.
	groups := map[string]bool{}
	for _, tgt := range after.Aliases[0].Targets {
		if tgt.Provider != "p1" || tgt.Model != "m1" {
			t.Fatalf("unexpected target %+v", tgt)
		}
		groups[tgt.Group] = true
	}
	if !groups[config.DefaultGroupID] || !groups["premium"] {
		t.Fatalf("target groups=%v", groups)
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
		{"alias target same group", `{"schema_version":2,"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1","groups":[{"id":"default","protocol":"openai-responses"}]}],"aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","group":"default","model":"m","enabled":true},{"provider":"p","group":"default","model":"m","enabled":true}]}]}`, "alias_target_identity_ambiguous"},
		{"alias target missing group both default", `{"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1"}],"aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","model":"m","enabled":true},{"provider":"p","model":"m","enabled":true}]}]}`, "alias_target_identity_ambiguous"},
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

func TestConfigImportAllowsDistinctGroupsSameProviderModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
  "schema_version": 2,
  "server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "providers":[{
    "id":"p",
    "base_url":"https://example.com/v1",
    "groups":[
      {"id":"default","protocol":"openai-responses","models":["m"]},
      {"id":"premium","protocol":"openai-responses","models":["m"]}
    ]
  }],
  "aliases":[{
    "alias":"a",
    "enabled":true,
    "protocol":"openai-responses",
    "targets":[
      {"provider":"p","group":"default","model":"m","enabled":true},
      {"provider":"p","group":"premium","model":"m","enabled":true}
    ]
  }]
}`
	if _, err := NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: content}); err != nil {
		t.Fatalf("import multi-group same model: %v", err)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Aliases) != 1 || len(after.Aliases[0].Targets) != 2 {
		t.Fatalf("aliases=%+v", after.Aliases)
	}
}

func TestConfigImportRejectsMixedSchema(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{
			name: "v1 provider groups",
			content: `{
  "schema_version":1,
  "server":{"host":"127.0.0.1","port":9982},
  "providers":[{"id":"p","base_url":"https://example.com/v1","protocol":"openai-responses","groups":[{"id":"default","protocol":"openai-responses"}]}],
  "aliases":[]
}`,
			want: "mixed",
		},
		{
			name: "v1 target group",
			content: `{
  "schema_version":1,
  "server":{"host":"127.0.0.1","port":9982},
  "providers":[{"id":"p","base_url":"https://example.com/v1","protocol":"openai-responses","models":["m"]}],
  "aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","group":"default","model":"m","enabled":true}]}]
}`,
			want: "mixed",
		},
		{
			name: "v1 rewrite provider_groups",
			content: `{
  "schema_version":1,
  "server":{"host":"127.0.0.1","port":9982},
  "providers":[{"id":"p","base_url":"https://example.com/v1","protocol":"openai-responses"}],
  "aliases":[{"alias":"a","enabled":true,"targets":[{"provider":"p","model":"m","enabled":true}]}],
  "request_rewrite_rules":[{"name":"r1","alias":"a","provider_groups":[{"provider":"p","group":"default"}],"enabled":true}]
}`,
			want: "mixed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			_, err := NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: test.content})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error=%v want contains %q", err, test.want)
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
		{ID: "p1", BaseURL: "https://p1.example/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		{ID: "p2", BaseURL: "https://p2.example/v1", Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
	}
	cfg.ProviderPriority = []string{"p2", "p1"}
	cfg.AutoAliasEnabled = false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	withoutPolicy := `{
  "server":{"host":"127.0.0.1","port":9982,"api_key":"ocswitch-local"},
  "providers":[
    {"id":"p1","base_url":"https://p1.example/v1","protocol":"openai-responses"},
    {"id":"p2","base_url":"https://p2.example/v1","protocol":"openai-responses"}
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

func TestConfigImportRejectsDuplicateNestedNullAndMixedFields(t *testing.T) {
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
			name:    "mixed v1 groups",
			content: `{"schema_version":1,"server":{"host":"127.0.0.1","port":9982},"providers":[{"id":"p","base_url":"https://example.com/v1","protocol":"openai-responses","groups":[]}],"aliases":[]}`,
			want:    "mixed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			_, err := NewService(path).ImportConfig(context.Background(), ConfigImportInput{Content: test.content})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("rejected import wrote file: %v", statErr)
			}
		})
	}
}
