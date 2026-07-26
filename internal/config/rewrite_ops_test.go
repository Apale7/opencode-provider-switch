package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rewriteMarkOp(path string, value any) RequestRewriteOperation {
	return RequestRewriteOperation{Op: RequestRewriteOpSet, Path: path, Value: value, ValueSet: true}
}

func rewriteDeleteOp(path string) RequestRewriteOperation {
	return RequestRewriteOperation{Op: RequestRewriteOpDelete, Path: path}
}

func TestRequestRewriteOperationPreservesLargeJSONInteger(t *testing.T) {
	t.Parallel()
	const raw = `{"op":"set","path":"metadata/id","value":9007199254740993}`
	var op RequestRewriteOperation
	if err := json.Unmarshal([]byte(raw), &op); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != raw {
		t.Fatalf("round trip = %s, want %s", encoded, raw)
	}
}

func TestApplyRequestRewriteRules_V2ExactProviderGroupMatch(t *testing.T) {
	t.Parallel()

	rules := []RequestRewriteRule{{
		Name:     "premium-only",
		Alias:    "multi-group-chat",
		Enabled:  true,
		Override: true,
		ProviderGroups: []ProviderGroupSelector{
			{Provider: "vendor-a", Group: "premium"},
		},
		Ops: []RequestRewriteOperation{rewriteMarkOp("$.service_tier", "priority")},
	}}

	// Exact (provider, group) match.
	premium := map[string]any{"service_tier": "default"}
	ApplyRequestRewriteRules(premium, "multi-group-chat", "vendor-a", "premium", "model-a", rules)
	if premium["service_tier"] != "priority" {
		t.Fatalf("premium match: %#v", premium)
	}

	// Same provider, sibling group must not match.
	def := map[string]any{"service_tier": "default"}
	ApplyRequestRewriteRules(def, "multi-group-chat", "vendor-a", DefaultGroupID, "model-a", rules)
	if def["service_tier"] != "default" {
		t.Fatalf("default sibling must not match premium selector: %#v", def)
	}

	// Same group name under another provider must not match.
	other := map[string]any{"service_tier": "default"}
	ApplyRequestRewriteRules(other, "multi-group-chat", "vendor-b", "premium", "model-a", rules)
	if other["service_tier"] != "default" {
		t.Fatalf("other provider must not match: %#v", other)
	}
}

func TestApplyRequestRewriteRules_V2EmptyProviderGroupsWildcard(t *testing.T) {
	t.Parallel()

	rules := []RequestRewriteRule{{
		Name:           "all-groups",
		Alias:          "multi-group-chat",
		Enabled:        true,
		Override:       true,
		ProviderGroups: []ProviderGroupSelector{}, // explicit empty wildcard
		Ops:            []RequestRewriteOperation{rewriteMarkOp("$.store", false)},
	}}

	for _, group := range []string{DefaultGroupID, "premium", "future-lane"} {
		payload := map[string]any{"store": true}
		ApplyRequestRewriteRules(payload, "multi-group-chat", "vendor-a", group, "model-a", rules)
		if payload["store"] != false {
			t.Fatalf("wildcard should match group %q: %#v", group, payload)
		}
	}

	// Alias still required.
	wrongAlias := map[string]any{"store": true}
	ApplyRequestRewriteRules(wrongAlias, "other-alias", "vendor-a", "premium", "model-a", rules)
	if wrongAlias["store"] != true {
		t.Fatalf("wrong alias must not match: %#v", wrongAlias)
	}
}

func TestApplyRequestRewriteRules_DefaultGroupSelectorsOnly(t *testing.T) {
	t.Parallel()

	// Non-empty provider_groups with default group only (duplicates/whitespace cleaned by normalize).
	rules := []RequestRewriteRule{{
		Name:     "provider-scoped",
		Alias:    "rewrite-chat",
		Enabled:  true,
		Override: true,
		ProviderGroups: []ProviderGroupSelector{
			{Provider: "vendor-a", Group: DefaultGroupID},
			{Provider: "vendor-a", Group: DefaultGroupID},
			{Provider: " vendor-b ", Group: DefaultGroupID},
		},
		Ops: []RequestRewriteOperation{rewriteMarkOp("$.service_tier", "priority")},
	}}

	for _, tc := range []struct {
		provider, group string
		wantTier        any
	}{
		{"vendor-a", DefaultGroupID, "priority"},
		{"vendor-b", DefaultGroupID, "priority"},
		{"vendor-a", "premium", "default"},
		{"vendor-b", "premium", "default"},
		{"vendor-c", DefaultGroupID, "default"},
	} {
		payload := map[string]any{"service_tier": "default"}
		ApplyRequestRewriteRules(payload, "rewrite-chat", tc.provider, tc.group, "model-a", rules)
		if payload["service_tier"] != tc.wantTier {
			t.Fatalf("provider=%s group=%s: service_tier=%#v want %#v", tc.provider, tc.group, payload["service_tier"], tc.wantTier)
		}
	}

	// Normalize side-effect: first-seen order + default group selectors.
	normalized := normalizeRequestRewriteRule(rules[0])
	want := []ProviderGroupSelector{
		{Provider: "vendor-a", Group: DefaultGroupID},
		{Provider: "vendor-b", Group: DefaultGroupID},
	}
	if !reflect.DeepEqual(normalized.ProviderGroups, want) {
		t.Fatalf("ProviderGroups normalize = %#v, want %#v", normalized.ProviderGroups, want)
	}
}

func TestApplyRequestRewriteRules_EmptyProviderGroupsWildcard(t *testing.T) {
	t.Parallel()

	rules := []RequestRewriteRule{
		{
			Name:           "empty-provider-groups",
			Alias:          "wildcard-chat",
			Enabled:        true,
			Override:       true,
			ProviderGroups: []ProviderGroupSelector{},
			Ops:            []RequestRewriteOperation{rewriteMarkOp("$.store", false)},
		},
		{
			Name:     "omitted-provider-groups",
			Alias:    "wildcard-chat",
			Enabled:  true,
			Override: true,
			Ops:      []RequestRewriteOperation{rewriteDeleteOp("$.parallel_tool_calls")},
		},
	}

	for _, group := range []string{DefaultGroupID, "premium", "future"} {
		payload := map[string]any{"store": true, "parallel_tool_calls": true}
		ApplyRequestRewriteRules(payload, "wildcard-chat", "vendor-a", group, "model-a", rules)
		if payload["store"] != false {
			t.Fatalf("empty provider_groups wildcard group=%s: %#v", group, payload)
		}
		if _, ok := payload["parallel_tool_calls"]; ok {
			t.Fatalf("omitted provider_groups wildcard should delete field group=%s: %#v", group, payload)
		}
	}
}

func TestApplyRequestRewriteRules_MissingSelectorNoFallback(t *testing.T) {
	t.Parallel()

	rules := []RequestRewriteRule{{
		Name:     "specific",
		Alias:    "chat",
		Enabled:  true,
		Override: true,
		ProviderGroups: []ProviderGroupSelector{
			{Provider: "vendor-a", Group: "premium"},
		},
		Ops: []RequestRewriteOperation{rewriteMarkOp("$.x", 1)},
	}}

	// Missing/wrong group for the listed provider: no match, no default fallback.
	payload := map[string]any{"x": 0}
	ApplyRequestRewriteRules(payload, "chat", "vendor-a", "missing-group", "m", rules)
	if payload["x"] != 0 {
		t.Fatalf("missing group must not match or fallback: %#v", payload)
	}

	// Empty groupID defaults to default, still not premium.
	payload = map[string]any{"x": 0}
	ApplyRequestRewriteRules(payload, "chat", "vendor-a", "", "m", rules)
	if payload["x"] != 0 {
		t.Fatalf("empty groupID→default must not match premium selector: %#v", payload)
	}

	// Missing provider entirely.
	payload = map[string]any{"x": 0}
	ApplyRequestRewriteRules(payload, "chat", "gone", "premium", "m", rules)
	if payload["x"] != 0 {
		t.Fatalf("missing provider must not match: %#v", payload)
	}
}

func TestApplyRequestRewriteRules_DuplicateSelectorsFirstOrder(t *testing.T) {
	t.Parallel()

	raw := RequestRewriteRule{
		Name:    "dedupe",
		Alias:   "chat",
		Enabled: true,
		ProviderGroups: []ProviderGroupSelector{
			{Provider: "p1", Group: "premium"},
			{Provider: "p1", Group: DefaultGroupID},
			{Provider: "p1", Group: "premium"}, // duplicate
			{Provider: " p2 ", Group: " premium "},
			{Provider: "p2", Group: "premium"}, // duplicate after trim
		},
	}
	got := normalizeRequestRewriteRule(raw)
	want := []ProviderGroupSelector{
		{Provider: "p1", Group: "premium"},
		{Provider: "p1", Group: DefaultGroupID},
		{Provider: "p2", Group: "premium"},
	}
	if !reflect.DeepEqual(got.ProviderGroups, want) {
		t.Fatalf("dedupe order = %#v, want %#v", got.ProviderGroups, want)
	}

	// Apply still matches each unique selector exactly once (behaviorally).
	rules := []RequestRewriteRule{{
		Name:           "dedupe-apply",
		Alias:          "chat",
		Enabled:        true,
		Override:       true,
		ProviderGroups: raw.ProviderGroups,
		Ops:            []RequestRewriteOperation{rewriteMarkOp("$.hit", true)},
	}}
	for _, group := range []string{"premium", DefaultGroupID} {
		payload := map[string]any{}
		ApplyRequestRewriteRules(payload, "chat", "p1", group, "m", rules)
		if payload["hit"] != true {
			t.Fatalf("p1/%s should match after dedupe: %#v", group, payload)
		}
	}
	payload := map[string]any{}
	ApplyRequestRewriteRules(payload, "chat", "p2", "premium", "m", rules)
	if payload["hit"] != true {
		t.Fatalf("p2/premium should match: %#v", payload)
	}
	payload = map[string]any{}
	ApplyRequestRewriteRules(payload, "chat", "p2", DefaultGroupID, "m", rules)
	if payload["hit"] != nil {
		t.Fatalf("p2/default must not match: %#v", payload)
	}
}

func TestApplyRequestRewriteRules_DefaultSelectorsWithoutNormalizeSafety(t *testing.T) {
	t.Parallel()

	// Direct matcher must not expand default-group selectors to sibling groups
	// even if normalize has not run yet.
	rule := RequestRewriteRule{
		Name:    "scoped",
		Alias:   "chat",
		Enabled: true,
		ProviderGroups: []ProviderGroupSelector{
			{Provider: "vendor-a", Group: DefaultGroupID},
		},
	}
	if !rewriteRuleMatchesResolvedTarget(rule, "chat", "vendor-a", DefaultGroupID) {
		t.Fatal("default group selector should match default group")
	}
	if rewriteRuleMatchesResolvedTarget(rule, "chat", "vendor-a", "premium") {
		t.Fatal("default group selector must not match sibling group")
	}
	if rewriteRuleMatchesResolvedTarget(rule, "chat", "vendor-b", DefaultGroupID) {
		t.Fatal("default group selector must not match other provider")
	}
}

func TestApplyRequestRewriteRules_Step1FixturesSelectorMatrix(t *testing.T) {
	t.Parallel()

	// Load Step 1 golden fixtures and assert apply-time selector matching only.
	// (Fixture ops may omit override; matching is the Task 3.3 contract.)
	type matchCase struct {
		ruleName string
		alias    string
		provider string
		group    string
		want     bool
	}
	cases := []struct {
		file   string
		checks []matchCase
	}{
		{
			file: "v2_canonical_multi_group.input.json",
			checks: []matchCase{
				{ruleName: "premium-only", alias: "multi-group-chat", provider: "vendor-a", group: "premium", want: true},
				{ruleName: "premium-only", alias: "multi-group-chat", provider: "vendor-a", group: DefaultGroupID, want: false},
				{ruleName: "all-groups-wildcard", alias: "multi-group-chat", provider: "vendor-a", group: DefaultGroupID, want: true},
				{ruleName: "all-groups-wildcard", alias: "multi-group-chat", provider: "vendor-a", group: "premium", want: true},
				{ruleName: "all-groups-wildcard", alias: "multi-group-chat", provider: "vendor-a", group: "future-lane", want: true},
			},
		},
		{
			file: "v1_legacy_rewrite_providers_to_default.input.json",
			checks: []matchCase{
				{ruleName: "provider-scoped", alias: "rewrite-chat", provider: "vendor-a", group: DefaultGroupID, want: true},
				{ruleName: "provider-scoped", alias: "rewrite-chat", provider: "vendor-b", group: DefaultGroupID, want: true},
				{ruleName: "provider-scoped", alias: "rewrite-chat", provider: "vendor-a", group: "premium", want: false},
				{ruleName: "provider-scoped", alias: "rewrite-chat", provider: "vendor-c", group: DefaultGroupID, want: false},
			},
		},
		{
			file: "v1_legacy_rewrite_empty_providers_wildcard.input.json",
			checks: []matchCase{
				{ruleName: "empty-providers-wildcard", alias: "wildcard-chat", provider: "vendor-a", group: DefaultGroupID, want: true},
				{ruleName: "empty-providers-wildcard", alias: "wildcard-chat", provider: "vendor-a", group: "future-lane", want: true},
				{ruleName: "omitted-providers-wildcard", alias: "wildcard-chat", provider: "vendor-a", group: "premium", want: true},
				{ruleName: "empty-providers-wildcard", alias: "other-alias", provider: "vendor-a", group: DefaultGroupID, want: false},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("testdata", "provider_groups", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			cfg, err := LoadFromBytes(filepath.Join(t.TempDir(), "config.json"), raw)
			if err != nil {
				t.Fatalf("LoadFromBytes: %v", err)
			}
			byName := map[string]RequestRewriteRule{}
			for _, rule := range cfg.RequestRewriteRulesSnapshot() {
				byName[rule.Name] = rule
			}
			for _, check := range tc.checks {
				rule, ok := byName[check.ruleName]
				if !ok {
					t.Fatalf("rule %q missing in fixture", check.ruleName)
				}
				rule = normalizeRequestRewriteRule(rule)
				got := rewriteRuleMatchesResolvedTarget(rule, check.alias, check.provider, check.group)
				if got != check.want {
					t.Fatalf("rule=%s alias=%s provider=%s group=%s: match=%v want %v (provider_groups=%#v)",
						check.ruleName, check.alias, check.provider, check.group, got, check.want, rule.ProviderGroups)
				}
			}
		})
	}
}

func TestRewriteRuleMatchesResolvedTarget_EmptyAliasNeverMatches(t *testing.T) {
	t.Parallel()

	rule := RequestRewriteRule{
		Alias:          "",
		ProviderGroups: []ProviderGroupSelector{},
	}
	if rewriteRuleMatchesResolvedTarget(rule, "", "p", DefaultGroupID) {
		t.Fatal("empty alias must never match")
	}
}

func TestApplyRequestRewriteRules_NilPayloadAndDisabled(t *testing.T) {
	t.Parallel()

	// Nil payload is a no-op.
	ApplyRequestRewriteRules(nil, "chat", "p1", DefaultGroupID, "m", []RequestRewriteRule{{
		Name: "x", Alias: "chat", Enabled: true,
		ProviderGroups: []ProviderGroupSelector{},
		Ops:            []RequestRewriteOperation{rewriteMarkOp("$.a", 1)},
	}})

	// Disabled rule skipped (override true would rewrite if enabled).
	payload := map[string]any{"a": 0}
	ApplyRequestRewriteRules(payload, "chat", "p1", DefaultGroupID, "m", []RequestRewriteRule{{
		Name: "x", Alias: "chat", Enabled: false, Override: true,
		ProviderGroups: []ProviderGroupSelector{},
		Ops:            []RequestRewriteOperation{rewriteMarkOp("$.a", 1)},
	}})
	if payload["a"] != 0 {
		t.Fatalf("disabled rule applied: %#v", payload)
	}
}

// Ensure fixture JSON still decodes when used as pure rewrite rule lists
// (guards Step 1 golden shape drift for apply tests).
func TestStep1RewriteFixtureJSONShapes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"v2_canonical_multi_group.input.json",
		"v1_legacy_rewrite_providers_to_default.input.json",
		"v1_legacy_rewrite_empty_providers_wildcard.input.json",
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", "provider_groups", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
		if _, ok := root["request_rewrite_rules"]; !ok {
			t.Fatalf("%s missing request_rewrite_rules", name)
		}
	}
}
