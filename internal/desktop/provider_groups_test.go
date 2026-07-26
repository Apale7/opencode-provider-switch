package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestProviderGroupBindingsAndAppDelegateToService(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedDesktopTwoGroupProvider(t, path, "vendor-desktop-a", "https://example.com/v1")
	instance := New(path)
	ctx := context.Background()
	b := instance.Bindings()

	// List via Bindings context + App + background wrapper.
	groups, err := b.ListProviderGroups(ctx, "vendor-desktop-a")
	if err != nil {
		t.Fatalf("Bindings.ListProviderGroups() error = %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("ListProviderGroups() len = %d, want 2", len(groups))
	}
	assertDesktopGroupMasked(t, groups[0])
	assertDesktopGroupMasked(t, groups[1])

	appGroups, err := instance.ListProviderGroups("vendor-desktop-a")
	if err != nil {
		t.Fatalf("App.ListProviderGroups() error = %v", err)
	}
	if len(appGroups) != 2 {
		t.Fatalf("App.ListProviderGroups() len = %d, want 2", len(appGroups))
	}
	nowGroups, err := b.ListProviderGroupsNow("vendor-desktop-a")
	if err != nil {
		t.Fatalf("ListProviderGroupsNow() error = %v", err)
	}
	if len(nowGroups) != 2 {
		t.Fatalf("ListProviderGroupsNow() len = %d, want 2", len(nowGroups))
	}

	// Create via Bindings / App / Now.
	createIn := app.ProviderGroupCreateInput{
		ProviderID: "vendor-desktop-a",
		Group: app.ProviderGroupInput{
			ID:             "enterprise",
			Name:           "Enterprise",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-desktop-primary-aaaa", "sk-desktop-backup-bbbb"},
			Models:         []string{"enterprise-model"},
		},
	}
	created, err := b.CreateProviderGroup(ctx, createIn)
	if err != nil {
		t.Fatalf("CreateProviderGroup() error = %v", err)
	}
	if created.ID != "enterprise" || created.APIKeyCount != 2 {
		t.Fatalf("created = %#v", created)
	}
	assertDesktopGroupMasked(t, created)
	if !reflect.DeepEqual(created.APIKeysMasked, []string{"sk-d…aaaa", "sk-d…bbbb"}) {
		t.Fatalf("created.APIKeysMasked = %#v", created.APIKeysMasked)
	}

	// Update (preserve keys when apiKeysChanged=false).
	updated, err := instance.UpdateProviderGroup(app.ProviderGroupUpdateInput{
		ProviderID: "vendor-desktop-a",
		GroupID:    "enterprise",
		Group: app.ProviderGroupInput{
			ID:             "enterprise",
			Name:           "Enterprise Renamed",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: false,
			Models:         []string{"enterprise-model", "enterprise-model-2"},
		},
	})
	if err != nil {
		t.Fatalf("App.UpdateProviderGroup() error = %v", err)
	}
	if updated.Name != "Enterprise Renamed" || updated.APIKeyCount != 2 {
		t.Fatalf("updated = %#v", updated)
	}
	assertDesktopGroupMasked(t, updated)

	// Refresh via App — exact group only.
	upstream := modelsDiscoveryServer(t, "enterprise-refreshed")
	defer upstream.Close()
	rewriteProviderBaseURL(t, path, "vendor-desktop-a", upstream.URL+"/v1")
	// Reload service after config rewrite on disk.
	instance = New(path)
	b = instance.Bindings()

	refreshResult, err := instance.RefreshProviderGroupModels(app.ProviderGroupRefreshModelsInput{
		ProviderID: "vendor-desktop-a",
		GroupID:    "enterprise",
	})
	if err != nil {
		t.Fatalf("RefreshProviderGroupModels() error = %v", err)
	}
	ent := findGroupView(refreshResult.Provider.Groups, "enterprise")
	if ent == nil || !reflect.DeepEqual(ent.Models, []string{"enterprise-refreshed"}) {
		t.Fatalf("refresh enterprise group = %#v", ent)
	}
	// Sibling catalog must not be clobbered by group-scoped refresh.
	premium := findGroupView(refreshResult.Provider.Groups, "premium")
	if premium == nil || !reflect.DeepEqual(premium.Models, []string{"premium-model"}) {
		t.Fatalf("sibling premium after refresh = %#v", premium)
	}

	// Ping via Bindings Now wrapper.
	ping, err := b.PingProviderGroupBaseURLNow(app.ProviderGroupPingInput{
		ProviderID: "vendor-desktop-a",
		GroupID:    "enterprise",
		BaseURL:    upstream.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("PingProviderGroupBaseURLNow() error = %v", err)
	}
	if !ping.Reachable {
		t.Fatalf("ping not reachable: %#v", ping)
	}

	// Delete via App (remove non-default enterprise).
	if err := instance.DeleteProviderGroup(app.ProviderGroupDeleteInput{
		ProviderID: "vendor-desktop-a",
		GroupID:    "enterprise",
	}); err != nil {
		t.Fatalf("DeleteProviderGroup() error = %v", err)
	}
	remaining, err := b.ListProviderGroups(ctx, "vendor-desktop-a")
	if err != nil {
		t.Fatalf("ListProviderGroups after delete: %v", err)
	}
	if findGroupView(remaining, "enterprise") != nil {
		t.Fatalf("enterprise still present after delete: %#v", remaining)
	}
	if findGroupView(remaining, "premium") == nil || findGroupView(remaining, config.DefaultGroupID) == nil {
		t.Fatalf("siblings missing after delete: %#v", remaining)
	}
}

func TestProviderGroupDesktopDTOsAvoidClientAPIKeyTypes(t *testing.T) {
	t.Parallel()

	// Desktop must pass internal/app ProviderGroup DTOs — not client proxy API key contracts.
	methodPairs := []struct {
		name string
		fn   any
	}{
		{"Bindings.CreateProviderGroup", (*Bindings).CreateProviderGroup},
		{"Bindings.UpdateProviderGroup", (*Bindings).UpdateProviderGroup},
		{"Bindings.DeleteProviderGroup", (*Bindings).DeleteProviderGroup},
		{"Bindings.RefreshProviderGroupModels", (*Bindings).RefreshProviderGroupModels},
		{"Bindings.PingProviderGroupBaseURL", (*Bindings).PingProviderGroupBaseURL},
		{"App.CreateProviderGroup", (*App).CreateProviderGroup},
		{"App.UpdateProviderGroup", (*App).UpdateProviderGroup},
		{"App.DeleteProviderGroup", (*App).DeleteProviderGroup},
		{"App.RefreshProviderGroupModels", (*App).RefreshProviderGroupModels},
		{"App.PingProviderGroupBaseURL", (*App).PingProviderGroupBaseURL},
	}
	allowed := map[string]bool{
		"ProviderGroupCreateInput":        true,
		"ProviderGroupUpdateInput":        true,
		"ProviderGroupDeleteInput":        true,
		"ProviderGroupRefreshModelsInput": true,
		"ProviderGroupPingInput":          true,
		"ProviderGroupView":               true,
		"ProviderSaveResult":              true,
		"ProviderPingResult":              true,
		"ProviderGroupInput":              true,
		"error":                           true,
		"Context":                         true,
		"string":                          true,
	}
	forbiddenSubstr := []string{"ProxyAPIKey", "APIKeyInput", "APIKeyView", "ApiKeyDTO", "ClientAPIKey"}
	for _, pair := range methodPairs {
		typ := reflect.TypeOf(pair.fn)
		if typ.Kind() != reflect.Func {
			t.Fatalf("%s is not a function", pair.name)
		}
		for i := 0; i < typ.NumIn(); i++ {
			arg := typ.In(i)
			// Skip receiver.
			if i == 0 {
				continue
			}
			checkDesktopDTOType(t, pair.name+" arg", arg, allowed, forbiddenSubstr)
		}
		for i := 0; i < typ.NumOut(); i++ {
			checkDesktopDTOType(t, pair.name+" out", typ.Out(i), allowed, forbiddenSubstr)
		}
	}

	input := reflect.TypeOf(app.ProviderGroupInput{})
	if _, ok := input.FieldByName("APIKeysChanged"); !ok {
		t.Fatal("ProviderGroupInput must use apiKeysChanged, not proxy API key DTOs")
	}
	view := reflect.TypeOf(app.ProviderGroupView{})
	if _, ok := view.FieldByName("APIKeys"); ok {
		t.Fatal("ProviderGroupView must not expose plaintext apiKeys")
	}
	if _, ok := view.FieldByName("APIKey"); ok {
		t.Fatal("ProviderGroupView must not expose plaintext apiKey")
	}
	if _, ok := view.FieldByName("APIKeysMasked"); !ok {
		t.Fatal("ProviderGroupView must expose apiKeysMasked")
	}
}

func TestAliasTargetGroupPassthroughViaDesktopBindings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedDesktopTwoGroupProvider(t, path, "vendor-desktop-a", "https://example.com/v1")
	instance := New(path)
	ctx := context.Background()
	b := instance.Bindings()

	if _, err := b.UpsertAlias(ctx, app.AliasUpsertInput{
		Alias:    "chat",
		Protocol: config.ProtocolOpenAIResponses,
	}); err != nil {
		t.Fatalf("UpsertAlias() error = %v", err)
	}

	// Bind with explicit non-default Group — must not silently fall back to default.
	bound, err := instance.BindTarget(app.AliasTargetInput{
		Alias:    "chat",
		Provider: "vendor-desktop-a",
		Group:    "premium",
		Model:    "premium-model",
	})
	if err != nil {
		t.Fatalf("BindTarget() error = %v", err)
	}
	if len(bound.Targets) != 1 || bound.Targets[0].Group != "premium" {
		t.Fatalf("bound targets = %#v, want group premium", bound.Targets)
	}

	// Also bind default so reorder can exercise multi-target Group fields.
	if _, err := b.BindAliasTarget(ctx, app.AliasTargetInput{
		Alias:    "chat",
		Provider: "vendor-desktop-a",
		Group:    config.DefaultGroupID,
		Model:    "default-model",
	}); err != nil {
		t.Fatalf("BindAliasTarget(default) error = %v", err)
	}

	reordered, err := instance.ReorderTargets(app.AliasTargetReorderInput{
		Alias: "chat",
		Targets: []app.AliasTargetRefInput{
			{Provider: "vendor-desktop-a", Group: config.DefaultGroupID, Model: "default-model"},
			{Provider: "vendor-desktop-a", Group: "premium", Model: "premium-model"},
		},
	})
	if err != nil {
		t.Fatalf("ReorderTargets() error = %v", err)
	}
	if len(reordered.Targets) != 2 {
		t.Fatalf("reordered targets = %#v", reordered.Targets)
	}
	if reordered.Targets[0].Group != config.DefaultGroupID || reordered.Targets[1].Group != "premium" {
		t.Fatalf("reorder lost Group fields: %#v", reordered.Targets)
	}

	// Unbind premium — Group must be part of identity, not ignored.
	unbound, err := instance.UnbindTarget(app.AliasTargetInput{
		Alias:    "chat",
		Provider: "vendor-desktop-a",
		Group:    "premium",
		Model:    "premium-model",
	})
	if err != nil {
		t.Fatalf("UnbindTarget() error = %v", err)
	}
	if len(unbound.Targets) != 1 || unbound.Targets[0].Group != config.DefaultGroupID {
		t.Fatalf("after unbind targets = %#v", unbound.Targets)
	}
}

func seedDesktopTwoGroupProvider(t *testing.T, path, providerID, baseURL string) {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      providerID,
		Name:    "Vendor Desktop A",
		BaseURL: baseURL,
		Groups: []config.ProviderGroup{
			{
				ID:           config.DefaultGroupID,
				Name:         config.DefaultGroupName,
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-default-secret-zzzz",
				Models:       []string{"default-model"},
				ModelsSource: "discovered",
			},
			{
				ID:           "premium",
				Name:         "Premium",
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-premium-secret-yyyy",
				Models:       []string{"premium-model"},
				ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
}

func rewriteProviderBaseURL(t *testing.T, path, providerID, baseURL string) {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := cfg.FindProvider(providerID)
	if provider == nil {
		t.Fatalf("provider %q missing", providerID)
	}
	updated := *provider
	updated.BaseURL = baseURL
	updated.BaseURLs = []string{baseURL}
	cfg.UpsertProvider(updated)
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
}

func modelsDiscoveryServer(t *testing.T, modelIDs ...string) *httptest.Server {
	t.Helper()
	type modelEntry struct {
		ID string `json:"id"`
	}
	data := make([]modelEntry, 0, len(modelIDs))
	for _, id := range modelIDs {
		data = append(data, modelEntry{ID: id})
	}
	payload, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func findGroupView(groups []app.ProviderGroupView, id string) *app.ProviderGroupView {
	for i := range groups {
		if groups[i].ID == id {
			return &groups[i]
		}
	}
	return nil
}

func assertDesktopGroupMasked(t *testing.T, view app.ProviderGroupView) {
	t.Helper()
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	for _, key := range []string{"apiKey", "apiKeys", "api_key", "api_keys", "key", "secret", "plaintext"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("view contains forbidden field %q: %s", key, string(raw))
		}
	}
	for _, secret := range []string{
		"sk-desktop-primary-aaaa",
		"sk-desktop-backup-bbbb",
		"sk-default-secret-zzzz",
		"sk-premium-secret-yyyy",
	} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("view leaked plaintext secret %q: %s", secret, string(raw))
		}
	}
}

func checkDesktopDTOType(t *testing.T, label string, typ reflect.Type, allowed map[string]bool, forbiddenSubstr []string) {
	t.Helper()
	for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.PkgPath() == "" {
		// Builtins and interfaces without package (error, string, context.Context name is Context).
		if typ.Name() != "" && !allowed[typ.Name()] && typ.Kind() != reflect.Interface {
			// allow unnamed/builtin
		}
		return
	}
	name := typ.Name()
	for _, forbidden := range forbiddenSubstr {
		if strings.Contains(name, forbidden) {
			t.Fatalf("%s type %s uses forbidden client API key DTO", label, name)
		}
	}
	if typ.PkgPath() == "github.com/Apale7/opencode-provider-switch/internal/app" {
		if !allowed[name] && name != "" {
			// Nested app types on return structs are OK if not forbidden above.
			// Only flag clearly client-key shaped names (already covered by forbiddenSubstr).
			_ = allowed
		}
	}
}
