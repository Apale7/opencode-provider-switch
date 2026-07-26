package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProviderGroupInputJSONShape(t *testing.T) {
	t.Parallel()

	raw := readProviderGroupFixture(t, "shapes", "provider_group_input.json")
	var in ProviderGroupInput
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatalf("unmarshal ProviderGroupInput: %v", err)
	}
	if in.ID != "premium" || in.Name != "Premium Fake Group" || in.Protocol != "openai" {
		t.Fatalf("ProviderGroupInput identity = %#v", in)
	}
	if !in.APIKeysChanged {
		t.Fatalf("apiKeysChanged = false, want true")
	}
	if !reflect.DeepEqual(in.APIKeys, []string{"sk-fake-primary-aaaa", "sk-fake-backup-bbbb"}) {
		t.Fatalf("apiKeys = %#v", in.APIKeys)
	}
	if !reflect.DeepEqual(in.Models, []string{"fake-model-a", "fake-model-b"}) {
		t.Fatalf("models = %#v", in.Models)
	}
	if in.Disabled {
		t.Fatalf("disabled = true, want false")
	}

	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal ProviderGroupInput: %v", err)
	}
	assertJSONHasKeys(t, encoded, "id", "protocol", "apiKeysChanged", "apiKeys", "models")
	assertJSONTagNames(t, reflect.TypeOf(ProviderGroupInput{}), map[string]string{
		"ID":             "id",
		"Name":           "name",
		"Protocol":       "protocol",
		"APIKeysChanged": "apiKeysChanged",
		"APIKeys":        "apiKeys",
		"Models":         "models",
		"Disabled":       "disabled",
	})
}

func TestProviderGroupViewJSONShape(t *testing.T) {
	t.Parallel()

	raw := readProviderGroupFixture(t, "shapes", "provider_group_view.json")
	var view ProviderGroupView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal ProviderGroupView: %v", err)
	}
	if view.ID != "premium" || view.Protocol != "openai" || view.APIKeyCount != 2 {
		t.Fatalf("ProviderGroupView = %#v", view)
	}
	if !reflect.DeepEqual(view.APIKeysMasked, []string{"sk-f…aaaa", "sk-f…bbbb"}) {
		t.Fatalf("apiKeysMasked = %#v", view.APIKeysMasked)
	}
	if view.ModelsSource != "manual" || view.Disabled {
		t.Fatalf("ProviderGroupView meta = %#v", view)
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal ProviderGroupView: %v", err)
	}
	assertNoPlaintextKeyFields(t, encoded)
	assertJSONHasKeys(t, encoded, "id", "protocol", "apiKeyCount", "apiKeysMasked", "models", "modelsSource", "disabled")
	assertJSONMissingKeys(t, encoded, "apiKey", "apiKeys", "api_key", "api_keys")
	assertJSONTagNames(t, reflect.TypeOf(ProviderGroupView{}), map[string]string{
		"ID":            "id",
		"Name":          "name",
		"Protocol":      "protocol",
		"APIKeyCount":   "apiKeyCount",
		"APIKeysMasked": "apiKeysMasked",
		"Models":        "models",
		"ModelsSource":  "modelsSource",
		"Disabled":      "disabled",
	})
}

func TestProviderViewGroupsJSONShape(t *testing.T) {
	t.Parallel()

	raw := readProviderGroupFixture(t, "provider_view", "with_groups.json")
	var payload struct {
		ID               string              `json:"id"`
		Name             string              `json:"name"`
		BaseURL          string              `json:"baseUrl"`
		BaseURLs         []string            `json:"baseUrls"`
		BaseURLStrategy  string              `json:"baseUrlStrategy"`
		Disabled         bool                `json:"disabled"`
		AutoAliasEnabled bool                `json:"autoAliasEnabled"`
		Groups           []ProviderGroupView `json:"groups"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal provider view fixture: %v", err)
	}
	if payload.ID != "vendor-fake-a" || len(payload.Groups) != 3 {
		t.Fatalf("provider view groups = %#v", payload)
	}
	if payload.Groups[0].ID != "default" || payload.Groups[1].ID != "premium" || payload.Groups[2].ID != "free" {
		t.Fatalf("group ids = %#v", payload.Groups)
	}
	if payload.Groups[2].APIKeyCount != 0 || !payload.Groups[2].Disabled {
		t.Fatalf("free group = %#v", payload.Groups[2])
	}

	view := ProviderView{
		ID:               payload.ID,
		Name:             payload.Name,
		BaseURL:          payload.BaseURL,
		BaseURLs:         payload.BaseURLs,
		BaseURLStrategy:  payload.BaseURLStrategy,
		Disabled:         payload.Disabled,
		AutoAliasEnabled: payload.AutoAliasEnabled,
		Groups:           payload.Groups,
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal ProviderView: %v", err)
	}
	assertNoPlaintextKeyFields(t, encoded)
	assertJSONHasKeys(t, encoded, "groups")
	assertJSONMissingKeys(t, encoded, "apiKeys", "api_keys")

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal ProviderView map: %v", err)
	}
	groupsRaw, ok := decoded["groups"]
	if !ok {
		t.Fatal("ProviderView missing groups")
	}
	assertNoPlaintextKeyFields(t, groupsRaw)
	assertJSONMissingKeys(t, groupsRaw, "apiKey", "apiKeys", "api_key", "api_keys")
	assertJSONTagNames(t, reflect.TypeOf(ProviderView{}), map[string]string{
		"Groups": "groups",
	})
}

func TestAliasTargetGroupJSONShapes(t *testing.T) {
	t.Parallel()

	t.Run("target_view", func(t *testing.T) {
		t.Parallel()
		raw := readProviderGroupFixture(t, "alias", "target_view.json")
		var view AliasTargetView
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatalf("unmarshal AliasTargetView: %v", err)
		}
		if view.Provider != "vendor-fake-a" || view.Group != "premium" || view.Model != "fake-model-a" {
			t.Fatalf("AliasTargetView = %#v", view)
		}
		if !view.Enabled || view.AutoGenerated || !view.Available {
			t.Fatalf("AliasTargetView flags = %#v", view)
		}
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatalf("marshal AliasTargetView: %v", err)
		}
		assertJSONHasKeys(t, encoded, "provider", "group", "model", "enabled", "autoGenerated", "available")
	})

	t.Run("target_input", func(t *testing.T) {
		t.Parallel()
		raw := readProviderGroupFixture(t, "alias", "target_input.json")
		var in AliasTargetInput
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatalf("unmarshal AliasTargetInput: %v", err)
		}
		if in.Alias != "fake-chat" || in.Provider != "vendor-fake-a" || in.Group != "premium" || in.Model != "fake-model-a" {
			t.Fatalf("AliasTargetInput = %#v", in)
		}
		encoded, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal AliasTargetInput: %v", err)
		}
		assertJSONHasKeys(t, encoded, "alias", "provider", "group", "model", "disabled")
	})

	t.Run("target_ref_input", func(t *testing.T) {
		t.Parallel()
		raw := readProviderGroupFixture(t, "alias", "target_ref_input.json")
		var in AliasTargetRefInput
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatalf("unmarshal AliasTargetRefInput: %v", err)
		}
		if in.Provider != "vendor-fake-a" || in.Group != "premium" || in.Model != "fake-model-a" {
			t.Fatalf("AliasTargetRefInput = %#v", in)
		}
		assertJSONTagNames(t, reflect.TypeOf(AliasTargetRefInput{}), map[string]string{
			"Provider": "provider",
			"Group":    "group",
			"Model":    "model",
		})
	})

	t.Run("reorder_input", func(t *testing.T) {
		t.Parallel()
		raw := readProviderGroupFixture(t, "alias", "reorder_input.json")
		var in AliasTargetReorderInput
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatalf("unmarshal AliasTargetReorderInput: %v", err)
		}
		if in.Alias != "fake-chat" || len(in.Targets) != 3 {
			t.Fatalf("AliasTargetReorderInput = %#v", in)
		}
		want := []AliasTargetRefInput{
			{Provider: "vendor-fake-a", Group: "premium", Model: "fake-model-a"},
			{Provider: "vendor-fake-a", Group: "default", Model: "fake-model-a"},
			{Provider: "vendor-fake-b", Group: "default", Model: "fake-model-z"},
		}
		if !reflect.DeepEqual(in.Targets, want) {
			t.Fatalf("targets = %#v, want %#v", in.Targets, want)
		}
		encoded, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal AliasTargetReorderInput: %v", err)
		}
		assertJSONHasKeys(t, encoded, "alias", "targets")
		for _, target := range in.Targets {
			if strings.TrimSpace(target.Group) == "" {
				t.Fatalf("reorder target missing group: %#v", target)
			}
		}
	})
}

func TestRequestRewriteProviderGroupsJSONShape(t *testing.T) {
	t.Parallel()

	inputRaw := readProviderGroupFixture(t, "rewrite", "rule_input.json")
	var input RequestRewriteRuleInput
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		t.Fatalf("unmarshal RequestRewriteRuleInput: %v", err)
	}
	if input.Name != "fake-strip-store-premium" || input.Alias != "fake-chat" {
		t.Fatalf("rewrite input identity = %#v", input)
	}
	wantSelectors := []ProviderGroupSelectorInput{
		{Provider: "vendor-fake-a", Group: "premium"},
		{Provider: "vendor-fake-a", Group: "default"},
	}
	if !reflect.DeepEqual(input.ProviderGroups, wantSelectors) {
		t.Fatalf("ProviderGroups = %#v, want %#v", input.ProviderGroups, wantSelectors)
	}
	viewRaw := readProviderGroupFixture(t, "rewrite", "rule_view.json")
	var view RequestRewriteRuleView
	if err := json.Unmarshal(viewRaw, &view); err != nil {
		t.Fatalf("unmarshal RequestRewriteRuleView: %v", err)
	}
	if !reflect.DeepEqual(view.ProviderGroups, []ProviderGroupSelectorView{
		{Provider: "vendor-fake-a", Group: "premium"},
		{Provider: "vendor-fake-a", Group: "default"},
	}) {
		t.Fatalf("view ProviderGroups = %#v", view.ProviderGroups)
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal RequestRewriteRuleView: %v", err)
	}
	assertJSONHasKeys(t, encoded, "name", "alias", "providerGroups", "enabled", "override", "ops")
	assertJSONTagNames(t, reflect.TypeOf(RequestRewriteRuleInput{}), map[string]string{
		"ProviderGroups": "providerGroups",
	})
	assertJSONTagNames(t, reflect.TypeOf(RequestRewriteRuleView{}), map[string]string{
		"ProviderGroups": "providerGroups",
	})
	assertJSONTagNames(t, reflect.TypeOf(ProviderGroupSelectorInput{}), map[string]string{
		"Provider": "provider",
		"Group":    "group",
	})
	assertJSONTagNames(t, reflect.TypeOf(ProviderGroupSelectorView{}), map[string]string{
		"Provider": "provider",
		"Group":    "group",
	})
}

func TestProviderGroupResponseFixturesHaveNoPlaintextKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path []string
		pick string
	}{
		{name: "create_response", path: []string{"create", "response.json"}},
		{name: "update_response", path: []string{"update", "response.json"}},
		{name: "list_groups", path: []string{"responses", "list_groups.json"}, pick: "groups"},
		{name: "create_group_response", path: []string{"responses", "create_group.json"}, pick: "response"},
		{name: "update_group_response", path: []string{"responses", "update_group.json"}, pick: "response"},
		{name: "api_keys_replace_response", path: []string{"api_keys", "changed_true_replace.response.json"}},
		{name: "api_keys_clear_response", path: []string{"api_keys", "changed_true_empty_clear.response.json"}},
		{name: "provider_view_groups", path: []string{"provider_view", "with_groups.json"}, pick: "groups"},
	}

	forbiddenSubstrings := []string{
		"sk-fake-primary-aaaa",
		"sk-fake-backup-bbbb",
		"sk-fake-new-key-cccc",
		"sk-fake-rotate-dddd",
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := readProviderGroupFixture(t, tc.path...)
			payload := raw
			if tc.pick != "" {
				var root map[string]json.RawMessage
				if err := json.Unmarshal(raw, &root); err != nil {
					t.Fatalf("unmarshal fixture root: %v", err)
				}
				picked, ok := root[tc.pick]
				if !ok {
					t.Fatalf("fixture missing %q", tc.pick)
				}
				payload = picked
			}

			if tc.pick == "groups" || strings.HasSuffix(tc.name, "response") || strings.Contains(tc.name, "list") {
				// Decode through typed views to prove response shape contracts.
				switch {
				case tc.pick == "groups":
					var groups []ProviderGroupView
					if err := json.Unmarshal(payload, &groups); err != nil {
						t.Fatalf("unmarshal groups: %v", err)
					}
					encoded, err := json.Marshal(groups)
					if err != nil {
						t.Fatalf("marshal groups: %v", err)
					}
					payload = encoded
				default:
					var view ProviderGroupView
					if err := json.Unmarshal(payload, &view); err != nil {
						// list wrapper already handled above; other responses are single views.
						if !strings.Contains(tc.name, "list") {
							t.Fatalf("unmarshal ProviderGroupView: %v", err)
						}
					} else {
						encoded, err := json.Marshal(view)
						if err != nil {
							t.Fatalf("marshal ProviderGroupView: %v", err)
						}
						payload = encoded
					}
				}
			}

			assertNoPlaintextKeyFields(t, payload)
			assertJSONMissingKeys(t, payload, "apiKey", "apiKeys", "api_key", "api_keys", "key", "secret", "plaintext")
			body := string(payload)
			for _, forbidden := range forbiddenSubstrings {
				if strings.Contains(body, forbidden) {
					t.Fatalf("response payload contains plaintext key %q", forbidden)
				}
			}
		})
	}
}

func TestProviderGroupInputDoesNotReuseProxyAPIKeyDTONames(t *testing.T) {
	t.Parallel()

	forbiddenTypeNames := []string{"ProxyAPIKeyInput", "ProxyAPIKeyView", "APIKeyInput", "APIKeyView", "ApiKeyDTO"}
	groupTypes := []reflect.Type{
		reflect.TypeOf(ProviderGroupInput{}),
		reflect.TypeOf(ProviderGroupView{}),
		reflect.TypeOf(ProviderGroupSelectorInput{}),
		reflect.TypeOf(ProviderGroupSelectorView{}),
	}
	for _, typ := range groupTypes {
		name := typ.Name()
		for _, forbidden := range forbiddenTypeNames {
			if name == forbidden {
				t.Fatalf("provider group type reuses forbidden name %q", forbidden)
			}
		}
	}

	input := reflect.TypeOf(ProviderGroupInput{})
	if _, ok := input.FieldByName("APIKeysChanged"); !ok {
		t.Fatal("ProviderGroupInput must use apiKeysChanged, not proxy API key DTOs")
	}
	if _, ok := input.FieldByName("APIKeys"); !ok {
		t.Fatal("ProviderGroupInput must carry apiKeys for replacement semantics")
	}
	view := reflect.TypeOf(ProviderGroupView{})
	if _, ok := view.FieldByName("APIKeyCount"); !ok {
		t.Fatal("ProviderGroupView must expose apiKeyCount")
	}
	if _, ok := view.FieldByName("APIKeysMasked"); !ok {
		t.Fatal("ProviderGroupView must expose apiKeysMasked")
	}
	if _, ok := view.FieldByName("APIKeys"); ok {
		t.Fatal("ProviderGroupView must not expose plaintext apiKeys")
	}
	if _, ok := view.FieldByName("APIKey"); ok {
		t.Fatal("ProviderGroupView must not expose plaintext apiKey")
	}
}

func TestProviderGroupCreateAndUpdateInputFixtures(t *testing.T) {
	t.Parallel()

	createRaw := readProviderGroupFixture(t, "create", "request.json")
	var create struct {
		ProviderID string             `json:"providerId"`
		Group      ProviderGroupInput `json:"group"`
	}
	if err := json.Unmarshal(createRaw, &create); err != nil {
		t.Fatalf("unmarshal create request: %v", err)
	}
	if create.ProviderID != "vendor-fake-a" || !create.Group.APIKeysChanged || len(create.Group.APIKeys) != 2 {
		t.Fatalf("create request = %#v", create)
	}

	updateRaw := readProviderGroupFixture(t, "update", "request.json")
	var update struct {
		ProviderID string             `json:"providerId"`
		GroupID    string             `json:"groupId"`
		Group      ProviderGroupInput `json:"group"`
	}
	if err := json.Unmarshal(updateRaw, &update); err != nil {
		t.Fatalf("unmarshal update request: %v", err)
	}
	if update.GroupID != "premium" || update.Group.APIKeysChanged || len(update.Group.APIKeys) != 0 {
		t.Fatalf("update request preserve keys = %#v", update)
	}

	preserveRaw := readProviderGroupFixture(t, "api_keys", "changed_false_empty_preserve.request.json")
	var preserve struct {
		Group ProviderGroupInput `json:"group"`
	}
	if err := json.Unmarshal(preserveRaw, &preserve); err != nil {
		t.Fatalf("unmarshal preserve request: %v", err)
	}
	if preserve.Group.APIKeysChanged || len(preserve.Group.APIKeys) != 0 {
		t.Fatalf("preserve request = %#v", preserve.Group)
	}

	replaceRaw := readProviderGroupFixture(t, "api_keys", "changed_true_replace.request.json")
	var replace struct {
		Group ProviderGroupInput `json:"group"`
	}
	if err := json.Unmarshal(replaceRaw, &replace); err != nil {
		t.Fatalf("unmarshal replace request: %v", err)
	}
	if !replace.Group.APIKeysChanged || !reflect.DeepEqual(replace.Group.APIKeys, []string{"sk-fake-new-key-cccc", "sk-fake-rotate-dddd"}) {
		t.Fatalf("replace request = %#v", replace.Group)
	}

	clearRaw := readProviderGroupFixture(t, "api_keys", "changed_true_empty_clear.request.json")
	var clear struct {
		Group ProviderGroupInput `json:"group"`
	}
	if err := json.Unmarshal(clearRaw, &clear); err != nil {
		t.Fatalf("unmarshal clear request: %v", err)
	}
	if !clear.Group.APIKeysChanged || len(clear.Group.APIKeys) != 0 {
		t.Fatalf("clear request = %#v", clear.Group)
	}
}

func readProviderGroupFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	pathParts := append([]string{"testdata", "provider_groups"}, parts...)
	path := filepath.Join(pathParts...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func assertJSONTagNames(t *testing.T, typ reflect.Type, want map[string]string) {
	t.Helper()
	for fieldName, tagName := range want {
		field, ok := typ.FieldByName(fieldName)
		if !ok {
			t.Fatalf("%s missing field %s", typ.Name(), fieldName)
		}
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name != tagName {
			t.Fatalf("%s.%s json tag = %q, want %q", typ.Name(), fieldName, name, tagName)
		}
	}
}

func assertJSONHasKeys(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	for _, key := range keys {
		if !jsonContainsKey(payload, key) {
			t.Fatalf("json missing key %q in %s", key, string(raw))
		}
	}
}

func assertJSONMissingKeys(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	for _, key := range keys {
		if jsonContainsKey(payload, key) {
			t.Fatalf("json must not contain key %q in %s", key, string(raw))
		}
	}
}

func assertNoPlaintextKeyFields(t *testing.T, raw []byte) {
	t.Helper()
	assertJSONMissingKeys(t, raw, "apiKey", "apiKeys", "api_key", "api_keys", "key", "secret", "plaintext")
}

func jsonContainsKey(payload any, key string) bool {
	switch value := payload.(type) {
	case map[string]any:
		if _, ok := value[key]; ok {
			return true
		}
		for _, nested := range value {
			if jsonContainsKey(nested, key) {
				return true
			}
		}
	case []any:
		for _, nested := range value {
			if jsonContainsKey(nested, key) {
				return true
			}
		}
	}
	return false
}
