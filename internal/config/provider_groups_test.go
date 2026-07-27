package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func providerGroupsFixture(name string) string {
	return filepath.Join("testdata", "provider_groups", name)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(providerGroupsFixture(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestV2ProviderGroupRequiresExplicitProtocol(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"schema_version": 2,
		"server": {"host":"127.0.0.1","port":9982,"api_key":"test"},
		"providers": [{"id":"p1","base_url":"https://example.com/v1","groups":[{"id":"default"}]}],
		"aliases": []
	}`)
	if _, err := LoadFromBytes(filepath.Join(t.TempDir(), "config.json"), raw); err == nil || !strings.Contains(err.Error(), "missing protocol") {
		t.Fatalf("LoadFromBytes() error = %v, want missing protocol", err)
	}
}

func TestLegacyProvidersMergeBySharedConnectionAndRemapReferences(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"server": {"host":"127.0.0.1","port":9982,"api_key":"test"},
		"providers": [
		{"id":"p1","name":"Primary","protocol":"openai-responses","base_url":"https://shared.example/v1/","base_urls":["https://shared.example/alt/"],"base_url_strategy":"latency","headers":{"X-Tenant":"a"},"models":["m1"],"models_source":"manual","disabled":true},
		{"id":"p2","name":"Secondary","protocol":"anthropic-messages","base_url":" https://shared.example/v1 ","base_urls":["https://shared.example/alt"],"base_url_strategy":" latency ","headers":{"x-tenant":"a"},"api_key":"sk-2","models":["m2"]},
			{"id":"p3","name":"Different Headers","protocol":"openai-responses","base_url":"https://shared.example/v1","base_urls":["https://shared.example/alt"],"base_url_strategy":"latency","headers":{"X-Tenant":"b"},"models":["m3"]},
			{"id":"p4","name":"Different URLs","protocol":"openai-responses","base_url":"https://other.example/v1","headers":{"X-Tenant":"a"},"models":["m4"]}
		],
		"aliases": [{"alias":"chat","enabled":true,"targets":[{"provider":"p2","model":"m2","enabled":true},{"provider":"p1","model":"m1","enabled":true},{"provider":"p3","model":"m3","enabled":true}]}],
		"request_rewrite_rules": [{"name":"rewrite","alias":"chat","providers":["p2","p1","p2","p3"],"enabled":true}],
		"provider_priority": ["p2","p1","p3","p2","p4"]
	}`)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read legacy config: %v", err)
	}
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before load: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after load: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after load: %v", err)
	}
	if !bytes.Equal(before, after) || infoBefore.ModTime() != infoAfter.ModTime() {
		t.Fatal("legacy Load() modified the on-disk config")
	}

	if len(cfg.Providers) != 3 {
		t.Fatalf("providers = %#v, want merged p1 plus p3 and p4", cfg.Providers)
	}
	merged := cfg.FindProvider("p1")
	if merged == nil || len(merged.Groups) != 2 {
		t.Fatalf("merged provider = %#v, want two groups", merged)
	}
	if merged.Disabled {
		t.Fatal("merged provider should keep legacy disabled state at the group level")
	}
	if merged.Groups[0].ID != DefaultGroupID || merged.Groups[0].Name != "Primary" || !merged.Groups[0].Disabled {
		t.Fatalf("first merged group = %#v", merged.Groups[0])
	}
	if merged.Groups[1].ID != "p2" || merged.Groups[1].Name != "Secondary" || merged.Groups[1].Disabled {
		t.Fatalf("second merged group = %#v", merged.Groups[1])
	}
	if cfg.FindProvider("p2") != nil {
		t.Fatal("merged provider p2 should no longer be retained")
	}
	if cfg.FindProvider("p3") == nil || cfg.FindProvider("p4") == nil {
		t.Fatal("incompatible providers must remain independent")
	}

	targets := cfg.Aliases[0].Targets
	wantTargets := []Target{{Provider: "p1", Group: "p2", Model: "m2", Enabled: true}, {Provider: "p1", Group: DefaultGroupID, Model: "m1", Enabled: true}, {Provider: "p3", Group: DefaultGroupID, Model: "m3", Enabled: true}}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("alias targets = %#v, want %#v", targets, wantTargets)
	}
	rule := cfg.RequestRewriteRules[0]
	wantSelectors := []ProviderGroupSelector{{Provider: "p1", Group: "p2"}, {Provider: "p1", Group: DefaultGroupID}, {Provider: "p3", Group: DefaultGroupID}}
	if !reflect.DeepEqual(rule.ProviderGroups, wantSelectors) {
		t.Fatalf("rewrite selectors = %#v, want %#v", rule.ProviderGroups, wantSelectors)
	}
	if !reflect.DeepEqual(cfg.ProviderPriority, []string{"p1", "p3", "p4"}) {
		t.Fatalf("provider priority = %#v, want [p1 p3 p4]", cfg.ProviderPriority)
	}
}

func TestProviderGroupsFixturesMigrationAndRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []string{
		"v1_single_upstream_key",
		"v1_multi_upstream_keys",
		"v1_multi_base_urls",
		"v1_legacy_alias_target_default",
		"v1_legacy_rewrite_providers_to_default",
		"v1_legacy_rewrite_empty_providers_wildcard",
		"v2_canonical_multi_group",
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := readFixture(t, name+".input.json")
			wantMemory := readFixture(t, name+".expected_memory.json")
			wantSave := readFixture(t, name+".expected_save.json")

			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, input, 0o600); err != nil {
				t.Fatalf("write input: %v", err)
			}
			infoBefore, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat before load: %v", err)
			}
			time.Sleep(5 * time.Millisecond)

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			infoAfter, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat after load: %v", err)
			}
			if infoAfter.ModTime() != infoBefore.ModTime() || infoAfter.Size() != infoBefore.Size() {
				t.Fatal("Load() modified on-disk file")
			}
			rawAfter, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read after load: %v", err)
			}
			if !bytes.Equal(rawAfter, input) {
				t.Fatal("Load() changed file bytes")
			}
			if cfg.SchemaVersion != CurrentSchemaVersion {
				t.Fatalf("SchemaVersion = %d, want %d", cfg.SchemaVersion, CurrentSchemaVersion)
			}

			assertConfigMatchesMemoryFixture(t, cfg, wantMemory)

			saved, err := cfg.MarshalPersistent()
			if err != nil {
				t.Fatalf("MarshalPersistent() error = %v", err)
			}
			assertJSONSemanticSubset(t, saved, wantSave)

			// First Save of legacy input should create .bak and write v2.
			if strings.HasPrefix(name, "v1_") || name == "v1_legacy_rewrite_empty_providers_wildcard" {
				if err := cfg.Save(); err != nil {
					t.Fatalf("Save() error = %v", err)
				}
				bak := BackupPathForConfig(path)
				if _, err := os.Stat(bak); err != nil {
					t.Fatalf("expected backup at %s: %v", bak, err)
				}
				bakRaw, _ := os.ReadFile(bak)
				if !bytes.Equal(bakRaw, input) {
					t.Fatal("backup content does not match original legacy bytes")
				}
				// Second load/save round-trip is idempotent.
				cfg2, err := Load(path)
				if err != nil {
					t.Fatalf("reload after save: %v", err)
				}
				saved2, err := cfg2.MarshalPersistent()
				if err != nil {
					t.Fatalf("second marshal: %v", err)
				}
				if err := cfg2.Save(); err != nil {
					t.Fatalf("second Save: %v", err)
				}
				cfg3, err := Load(path)
				if err != nil {
					t.Fatalf("reload after second save: %v", err)
				}
				saved3, err := cfg3.MarshalPersistent()
				if err != nil {
					t.Fatalf("third marshal: %v", err)
				}
				assertJSONSemanticEqual(t, saved2, saved3)
			} else {
				// v2 round-trip
				if err := cfg.Save(); err != nil {
					t.Fatalf("Save() error = %v", err)
				}
				cfg2, err := Load(path)
				if err != nil {
					t.Fatalf("reload: %v", err)
				}
				saved2, err := cfg2.MarshalPersistent()
				if err != nil {
					t.Fatalf("re-marshal: %v", err)
				}
				assertJSONSemanticEqual(t, saved, saved2)
			}
		})
	}
}

func TestProviderGroupsFixturesReject(t *testing.T) {
	t.Parallel()

	cases := []string{
		"reject_v1_mixed_groups_present",
		"reject_v1_mixed_groups_empty_array",
		"reject_v1_mixed_target_group",
		"reject_v1_mixed_rewrite_provider_groups",
		"reject_schema_version_unknown",
		"reject_schema_version_null",
		"reject_schema_version_non_integer",
		"reject_v2_missing_groups_field",
		"reject_v2_empty_groups",
		"reject_v2_top_level_legacy_fields",
		"reject_v2_target_missing_group",
		"reject_v2_target_empty_group",
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := readFixture(t, name+".input.json")
			wantErr := readFixture(t, name+".expected_error.json")
			var meta struct {
				ErrorCode       string   `json:"error_code"`
				MessageContains []string `json:"message_contains"`
			}
			if err := json.Unmarshal(wantErr, &meta); err != nil {
				t.Fatalf("parse expected_error: %v", err)
			}
			_, err := LoadFromBytes(filepath.Join(t.TempDir(), "config.json"), input)
			if err == nil {
				t.Fatal("LoadFromBytes() error = nil, want error")
			}
			msg := err.Error()
			for _, part := range meta.MessageContains {
				if !strings.Contains(strings.ToLower(msg), strings.ToLower(part)) {
					t.Fatalf("error %q missing %q (code %s)", msg, part, meta.ErrorCode)
				}
			}
		})
	}
}

func TestProviderGroupsCanonicalPersistHasNoTopLevelLegacyFields(t *testing.T) {
	t.Parallel()

	// Multi-group providers persist only group-scoped protocol/key fields.
	multi := Provider{
		ID: "p2", BaseURL: "https://x.example/v1",
		Groups: []ProviderGroup{
			{ID: "default", Protocol: ProtocolOpenAIResponses, APIKey: "sk-a"},
			{ID: "premium", Protocol: ProtocolOpenAICompatible, APIKey: "sk-b"},
		},
	}
	raw, err := (&Config{SchemaVersion: CurrentSchemaVersion, Server: Default().Server, Providers: []Provider{multi}, Aliases: []Alias{}}).MarshalPersistent()
	if err != nil {
		t.Fatalf("MarshalPersistent: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	providers, _ := root["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("providers len = %d", len(providers))
	}
	prov, _ := providers[0].(map[string]any)
	if _, ok := prov["protocol"]; ok {
		t.Fatalf("persisted top-level protocol: %#v", prov)
	}
	if _, ok := prov["api_key"]; ok {
		t.Fatalf("persisted top-level api_key: %#v", prov)
	}
	if _, ok := prov["api_keys"]; ok {
		t.Fatalf("persisted top-level api_keys: %#v", prov)
	}
	if _, ok := prov["models"]; ok {
		t.Fatalf("persisted top-level models: %#v", prov)
	}
	if _, ok := prov["models_source"]; ok {
		t.Fatalf("persisted top-level models_source: %#v", prov)
	}
}

func TestProviderGroupsSharedAuthHeaderValidation(t *testing.T) {
	t.Parallel()

	// Single default group: compatible (no hard error).
	single := &Config{
		Server: Default().Server,
		Providers: []Provider{{
			ID: "p1", BaseURL: "https://x.example/v1",
			Headers: map[string]string{"Authorization": "Bearer leaked"},
			Groups: []ProviderGroup{{
				ID: DefaultGroupID, Protocol: ProtocolOpenAIResponses, APIKey: "sk",
			}},
		}},
	}
	if errs := single.ValidateForPersist(); len(errs) != 0 {
		t.Fatalf("single-group ValidateForPersist() = %v, want none", errs)
	}

	// Multi-group with auth header: hard error.
	multi := &Config{
		Server: Default().Server,
		Providers: []Provider{{
			ID: "p1", BaseURL: "https://x.example/v1",
			Headers: map[string]string{"Authorization": "Bearer leaked"},
			Groups: []ProviderGroup{
				{ID: "default", Protocol: ProtocolOpenAIResponses, APIKey: "sk-a"},
				{ID: "premium", Protocol: ProtocolOpenAICompatible, APIKey: "sk-b"},
			},
		}},
	}
	errs := multi.ValidateForPersist()
	if len(errs) == 0 {
		t.Fatal("multi-group ValidateForPersist() = nil, want auth header error")
	}
	if !strings.Contains(errs[0].Error(), "Authorization") {
		t.Fatalf("error = %q", errs[0].Error())
	}
}

func TestProviderGroupHelpers(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []Provider{{
			ID: "vendor-a",
			Groups: []ProviderGroup{
				{ID: "default", Protocol: ProtocolOpenAIResponses, APIKey: "sk-a", Disabled: false},
				{ID: "premium", Protocol: ProtocolOpenAICompatible, APIKey: "sk-b", APIKeys: []string{"sk-c"}, Disabled: true},
			},
		}},
	}
	p, g := cfg.FindProviderGroup("vendor-a", "premium")
	if p == nil || g == nil {
		t.Fatal("FindProviderGroup missing")
	}
	if g.IsEnabled() {
		t.Fatal("disabled group IsEnabled() = true")
	}
	if keys := g.EffectiveAPIKeys(); len(keys) != 2 || keys[0] != "sk-b" || keys[1] != "sk-c" {
		t.Fatalf("EffectiveAPIKeys = %#v", keys)
	}
	if cfg.FindProvider("vendor-a").FindGroup("missing") != nil {
		t.Fatal("missing group should be nil")
	}
}

func assertConfigMatchesMemoryFixture(t *testing.T, cfg *Config, wantJSON []byte) {
	t.Helper()
	var want map[string]any
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatalf("want memory json: %v", err)
	}
	got := memoryView(cfg)
	assertMapSubset(t, got, want, "")
}

func memoryView(cfg *Config) map[string]any {
	providers := make([]any, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		groups := make([]any, 0, len(p.Groups))
		for _, g := range p.Groups {
			item := map[string]any{
				"id":       g.ID,
				"name":     g.Name,
				"protocol": g.Protocol,
				"disabled": g.Disabled,
			}
			if g.APIKey != "" {
				item["api_key"] = g.APIKey
			}
			if len(g.APIKeys) > 0 {
				item["api_keys"] = stringsToAny(g.APIKeys)
			}
			if len(g.Models) > 0 {
				item["models"] = stringsToAny(g.Models)
			}
			if g.ModelsSource != "" {
				item["models_source"] = g.ModelsSource
			}
			groups = append(groups, item)
		}
		prov := map[string]any{
			"id":       p.ID,
			"name":     p.Name,
			"base_url": p.BaseURL,
			"disabled": p.Disabled,
			"groups":   groups,
		}
		if len(p.BaseURLs) > 0 {
			prov["base_urls"] = stringsToAny(p.BaseURLs)
		}
		if p.BaseURLStrategy != "" && p.BaseURLStrategy != ProviderBaseURLStrategyOrdered {
			prov["base_url_strategy"] = p.BaseURLStrategy
		} else if len(p.BaseURLs) > 1 {
			prov["base_url_strategy"] = NormalizeProviderBaseURLStrategy(p.BaseURLStrategy)
		}
		if len(p.Headers) > 0 {
			headers := map[string]any{}
			for k, v := range p.Headers {
				headers[k] = v
			}
			prov["headers"] = headers
		}
		if p.AutoAliasEnabled != nil {
			prov["auto_alias_enabled"] = *p.AutoAliasEnabled
		}
		providers = append(providers, prov)
	}
	aliases := make([]any, 0, len(cfg.Aliases))
	for _, a := range cfg.Aliases {
		targets := make([]any, 0, len(a.Targets))
		for _, tg := range a.Targets {
			item := map[string]any{
				"provider": tg.Provider,
				"group":    tg.Group,
				"model":    tg.Model,
				"enabled":  tg.Enabled,
			}
			if tg.AutoGenerated {
				item["auto_generated"] = true
			}
			targets = append(targets, item)
		}
		alias := map[string]any{
			"alias":    a.Alias,
			"protocol": a.Protocol,
			"enabled":  a.Enabled,
			"targets":  targets,
		}
		if a.DisplayName != "" {
			alias["display_name"] = a.DisplayName
		}
		aliases = append(aliases, alias)
	}
	rules := make([]any, 0, len(cfg.RequestRewriteRules))
	for _, r := range cfg.RequestRewriteRules {
		rule := map[string]any{
			"name":    r.Name,
			"alias":   r.Alias,
			"enabled": r.Enabled,
		}
		if r.Override {
			rule["override"] = true
		}
		// Always emit provider_groups including empty wildcard.
		sels := make([]any, 0, len(r.ProviderGroups))
		for _, sel := range r.ProviderGroups {
			sels = append(sels, map[string]any{"provider": sel.Provider, "group": sel.Group})
		}
		rule["provider_groups"] = sels
		if len(r.Ops) > 0 {
			ops := make([]any, 0, len(r.Ops))
			for _, op := range r.Ops {
				item := map[string]any{"op": op.Op, "path": op.Path}
				if op.ValueSet {
					item["value"] = op.Value
				}
				ops = append(ops, item)
			}
			rule["ops"] = ops
		}
		rules = append(rules, rule)
	}
	out := map[string]any{
		"schema_version": CurrentSchemaVersion,
		"server": map[string]any{
			"host":    cfg.Server.Host,
			"port":    float64(cfg.Server.Port),
			"api_key": cfg.Server.APIKey,
		},
		"providers":          providers,
		"aliases":            aliases,
		"auto_alias_enabled": cfg.AutoAliasEnabled,
	}
	if len(rules) > 0 {
		out["request_rewrite_rules"] = rules
	}
	if len(cfg.ProviderPriority) > 0 {
		out["provider_priority"] = stringsToAny(cfg.ProviderPriority)
	}
	return out
}

func stringsToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func assertJSONSemanticSubset(t *testing.T, gotJSON, wantJSON []byte) {
	t.Helper()
	var got, want map[string]any
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatalf("got json: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatalf("want json: %v", err)
	}
	assertMapSubset(t, got, want, "")
}

func assertJSONSemanticEqual(t *testing.T, a, b []byte) {
	t.Helper()
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		t.Fatalf("left: %v", err)
	}
	if err := json.Unmarshal(b, &right); err != nil {
		t.Fatalf("right: %v", err)
	}
	if !jsonDeepEqual(left, right) {
		t.Fatalf("json not equal:\nleft=%s\nright=%s", a, b)
	}
}

func assertMapSubset(t *testing.T, got, want map[string]any, path string) {
	t.Helper()
	for key, wantVal := range want {
		child := path + "/" + key
		gotVal, ok := got[key]
		if !ok {
			t.Fatalf("missing key %s", child)
		}
		assertJSONValueSubset(t, gotVal, wantVal, child)
	}
}

func assertJSONValueSubset(t *testing.T, got, want any, path string) {
	t.Helper()
	switch wantTyped := want.(type) {
	case map[string]any:
		gotMap, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: got %T, want object", path, got)
		}
		assertMapSubset(t, gotMap, wantTyped, path)
	case []any:
		gotArr, ok := got.([]any)
		if !ok {
			t.Fatalf("%s: got %T, want array", path, got)
		}
		if len(gotArr) != len(wantTyped) {
			t.Fatalf("%s: len=%d want %d", path, len(gotArr), len(wantTyped))
		}
		for i := range wantTyped {
			assertJSONValueSubset(t, gotArr[i], wantTyped[i], path+indexPath(i))
		}
	default:
		if !jsonDeepEqual(got, want) {
			t.Fatalf("%s: got %#v want %#v", path, got, want)
		}
	}
}

func indexPath(i int) string {
	return "[" + itoa(i) + "]"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	n := i
	if n < 0 {
		n = -n
	}
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if i < 0 {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func jsonDeepEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var an, bn any
	if err := json.Unmarshal(ab, &an); err != nil {
		return false
	}
	if err := json.Unmarshal(bb, &bn); err != nil {
		return false
	}
	return jsonEqualNormalized(an, bn)
}

func jsonEqualNormalized(a, b any) bool {
	switch at := a.(type) {
	case map[string]any:
		bt, ok := b.(map[string]any)
		if !ok || len(at) != len(bt) {
			return false
		}
		for k, av := range at {
			if !jsonEqualNormalized(av, bt[k]) {
				return false
			}
		}
		return true
	case []any:
		bt, ok := b.([]any)
		if !ok || len(at) != len(bt) {
			return false
		}
		for i := range at {
			if !jsonEqualNormalized(at[i], bt[i]) {
				return false
			}
		}
		return true
	case float64:
		switch bt := b.(type) {
		case float64:
			return at == bt
		case int:
			return at == float64(bt)
		default:
			return false
		}
	default:
		return a == b
	}
}
