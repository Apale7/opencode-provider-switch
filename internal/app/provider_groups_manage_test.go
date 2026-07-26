package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func TestProviderGroupCRUDApiKeysTriStateAndMask(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-fake-a")
	svc := NewService(path)
	ctx := context.Background()

	// Create second sibling is already seeded; create another group with keys.
	createRaw := readManageFixture(t, "create", "request.json")
	var createReq ProviderGroupCreateInput
	if err := json.Unmarshal(createRaw, &createReq); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	// Fixture protocol "openai" is shape-only; use a validated runtime protocol.
	createReq.Group.Protocol = config.ProtocolOpenAIResponses
	// premium already exists in seed — use a fresh id for create path.
	createReq.Group.ID = "enterprise"
	createReq.Group.Name = "Enterprise Fake Group"

	created, err := svc.CreateProviderGroup(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateProviderGroup() error = %v", err)
	}
	if created.ID != "enterprise" || created.APIKeyCount != 2 {
		t.Fatalf("created = %#v", created)
	}
	assertNoPlaintextKeys(t, created)
	if !reflect.DeepEqual(created.APIKeysMasked, []string{"sk-f…aaaa", "sk-f…bbbb"}) {
		t.Fatalf("created.APIKeysMasked = %#v", created.APIKeysMasked)
	}

	// Preserve keys when apiKeysChanged=false with empty apiKeys.
	preserveRaw := readManageFixture(t, "api_keys", "changed_false_empty_preserve.request.json")
	var preserveReq ProviderGroupUpdateInput
	if err := json.Unmarshal(preserveRaw, &preserveReq); err != nil {
		t.Fatalf("unmarshal preserve: %v", err)
	}
	preserveReq.Group.Protocol = config.ProtocolOpenAIResponses
	preserveReq.GroupID = "enterprise"
	preserveReq.Group.ID = "enterprise"
	preserved, err := svc.UpdateProviderGroup(ctx, preserveReq)
	if err != nil {
		t.Fatalf("UpdateProviderGroup(preserve) error = %v", err)
	}
	if preserved.APIKeyCount != 2 {
		t.Fatalf("preserve apiKeyCount = %d", preserved.APIKeyCount)
	}
	assertNoPlaintextKeys(t, preserved)
	assertStoredKeys(t, path, "vendor-fake-a", "enterprise", []string{"sk-fake-primary-aaaa", "sk-fake-backup-bbbb"})

	// Illegal: apiKeysChanged=false with non-empty keys.
	illegalRaw := readManageFixture(t, "api_keys", "changed_false_with_keys.illegal.request.json")
	var illegalReq ProviderGroupUpdateInput
	if err := json.Unmarshal(illegalRaw, &illegalReq); err != nil {
		t.Fatalf("unmarshal illegal: %v", err)
	}
	illegalReq.Group.Protocol = config.ProtocolOpenAIResponses
	illegalReq.GroupID = "enterprise"
	illegalReq.Group.ID = "enterprise"
	if _, err := svc.UpdateProviderGroup(ctx, illegalReq); err == nil {
		t.Fatal("expected illegal apiKeysChanged=false+keys to fail")
	} else if !strings.Contains(err.Error(), "apiKeys must be empty when apiKeysChanged is false") {
		t.Fatalf("illegal error = %q", err.Error())
	}
	assertStoredKeys(t, path, "vendor-fake-a", "enterprise", []string{"sk-fake-primary-aaaa", "sk-fake-backup-bbbb"})

	// Replace keys.
	replaceRaw := readManageFixture(t, "api_keys", "changed_true_replace.request.json")
	var replaceReq ProviderGroupUpdateInput
	if err := json.Unmarshal(replaceRaw, &replaceReq); err != nil {
		t.Fatalf("unmarshal replace: %v", err)
	}
	replaceReq.Group.Protocol = config.ProtocolOpenAIResponses
	replaceReq.GroupID = "enterprise"
	replaceReq.Group.ID = "enterprise"
	replaced, err := svc.UpdateProviderGroup(ctx, replaceReq)
	if err != nil {
		t.Fatalf("UpdateProviderGroup(replace) error = %v", err)
	}
	assertNoPlaintextKeys(t, replaced)
	if !reflect.DeepEqual(replaced.APIKeysMasked, []string{"sk-f…cccc", "sk-f…dddd"}) {
		t.Fatalf("replaced.APIKeysMasked = %#v", replaced.APIKeysMasked)
	}
	assertStoredKeys(t, path, "vendor-fake-a", "enterprise", []string{"sk-fake-new-key-cccc", "sk-fake-rotate-dddd"})

	// Clear keys with apiKeysChanged=true and empty array.
	clearRaw := readManageFixture(t, "api_keys", "changed_true_empty_clear.request.json")
	var clearReq ProviderGroupUpdateInput
	if err := json.Unmarshal(clearRaw, &clearReq); err != nil {
		t.Fatalf("unmarshal clear: %v", err)
	}
	clearReq.Group.Protocol = config.ProtocolOpenAIResponses
	clearReq.GroupID = "enterprise"
	clearReq.Group.ID = "enterprise"
	cleared, err := svc.UpdateProviderGroup(ctx, clearReq)
	if err != nil {
		t.Fatalf("UpdateProviderGroup(clear) error = %v", err)
	}
	if cleared.APIKeyCount != 0 || len(cleared.APIKeysMasked) != 0 {
		t.Fatalf("cleared = %#v", cleared)
	}
	assertNoPlaintextKeys(t, cleared)
	assertStoredKeys(t, path, "vendor-fake-a", "enterprise", nil)

	// Masked placeholders must be rejected and must not persist.
	maskRaw := readManageFixture(t, "api_keys", "masked_placeholder.reject.request.json")
	var maskCases struct {
		Cases []struct {
			Name  string             `json:"name"`
			Group ProviderGroupInput `json:"group"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(maskRaw, &maskCases); err != nil {
		t.Fatalf("unmarshal mask cases: %v", err)
	}
	// Restore real keys first so we can assert they stay unchanged.
	if _, err := svc.UpdateProviderGroup(ctx, ProviderGroupUpdateInput{
		ProviderID: "vendor-fake-a",
		GroupID:    "enterprise",
		Group: ProviderGroupInput{
			ID:             "enterprise",
			Name:           "Enterprise Fake Group",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-fake-primary-aaaa", "sk-fake-backup-bbbb"},
			Models:         []string{"fake-model-a"},
		},
	}); err != nil {
		t.Fatalf("restore keys: %v", err)
	}
	for _, tc := range maskCases.Cases {
		tc.Group.Protocol = config.ProtocolOpenAIResponses
		tc.Group.ID = "enterprise"
		_, err := svc.UpdateProviderGroup(ctx, ProviderGroupUpdateInput{
			ProviderID: "vendor-fake-a",
			GroupID:    "enterprise",
			Group:      tc.Group,
		})
		if err == nil {
			t.Fatalf("case %s: expected mask rejection", tc.Name)
		}
		if !strings.Contains(err.Error(), "apiKeys must not contain masked placeholders") {
			t.Fatalf("case %s: error = %q", tc.Name, err.Error())
		}
	}
	assertStoredKeys(t, path, "vendor-fake-a", "enterprise", []string{"sk-fake-primary-aaaa", "sk-fake-backup-bbbb"})
}

func TestProviderGroupListAndProviderViewNoPlaintext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-fake-a")
	svc := NewService(path)
	ctx := context.Background()

	groups, err := svc.ListProviderGroups(ctx, "vendor-fake-a")
	if err != nil {
		t.Fatalf("ListProviderGroups() error = %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %#v", groups)
	}
	for _, g := range groups {
		assertNoPlaintextKeys(t, g)
	}

	providers, err := svc.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(providers) != 1 || len(providers[0].Groups) != 2 {
		t.Fatalf("providers = %#v", providers)
	}
	for _, g := range providers[0].Groups {
		assertNoPlaintextKeys(t, g)
	}
	encoded, err := json.Marshal(providers[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"sk-default-secret", "sk-premium-secret", "sk-fake-primary-aaaa"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("ProviderView leaked plaintext %q in %s", forbidden, string(encoded))
		}
	}
}

func TestProviderGroupUpdateDoesNotClobberSibling(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)
	ctx := context.Background()

	_, err := svc.UpdateProviderGroup(ctx, ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    "premium",
		Group: ProviderGroupInput{
			ID:             "premium",
			Name:           "Premium Updated",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-premium-new"},
			Models:         []string{"premium-new"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	premium := provider.FindGroup("premium")
	if defaultGroup == nil || premium == nil {
		t.Fatalf("groups = %#v", provider.Groups)
	}
	if !reflect.DeepEqual(defaultGroup.EffectiveAPIKeys(), []string{"sk-default-secret"}) {
		t.Fatalf("default keys mutated: %#v", defaultGroup.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(defaultGroup.Models, []string{"default-model"}) {
		t.Fatalf("default models mutated: %#v", defaultGroup.Models)
	}
	if !reflect.DeepEqual(premium.EffectiveAPIKeys(), []string{"sk-premium-new"}) {
		t.Fatalf("premium keys = %#v", premium.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(premium.Models, []string{"premium-new"}) {
		t.Fatalf("premium models = %#v", premium.Models)
	}
}

func TestProviderUpsertDoesNotOverwriteSiblingGroups(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)

	// Shared edit must not rewrite sibling groups (or even default group auth).
	_, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "vendor-a",
		Name:    "Vendor A Renamed",
		BaseURL: "https://example.com/v1",
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.FindGroup("premium") == nil {
		t.Fatalf("premium group lost: %#v", provider.Groups)
	}
	if got := provider.FindGroup("premium").EffectiveAPIKeys(); !reflect.DeepEqual(got, []string{"sk-premium-secret"}) {
		t.Fatalf("premium keys = %#v", got)
	}
}

func TestProviderCreateMaterializesDefaultGroup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "new-vendor",
		BaseURL: "https://example.com/v1",
		DefaultGroup: &ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Name:           config.DefaultGroupName,
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-create", "sk-create-2"},
			Models:         []string{},
		},
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if len(result.Provider.Groups) != 1 || result.Provider.Groups[0].ID != config.DefaultGroupID {
		t.Fatalf("Groups = %#v", result.Provider.Groups)
	}
	if result.Provider.Groups[0].APIKeyCount != 2 {
		t.Fatalf("APIKeyCount = %d, want 2", result.Provider.Groups[0].APIKeyCount)
	}
	assertNoPlaintextKeys(t, result.Provider.Groups[0])
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	p := reloaded.FindProvider("new-vendor")
	if p == nil {
		t.Fatal("provider missing")
	}
	g := p.FindGroup(config.DefaultGroupID)
	if g == nil || !reflect.DeepEqual(g.EffectiveAPIKeys(), []string{"sk-create", "sk-create-2"}) {
		t.Fatalf("persisted keys = %#v", g)
	}
}

func TestUpsertProviderAtomicDefaultGroupUpdate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)

	// Shared BaseURL + default group protocol/key in one mutation; sibling intact.
	result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "vendor-a",
		BaseURL: "https://new.example.com/v1",
		DefaultGroup: &ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Name:           config.DefaultGroupName,
			Protocol:       config.ProtocolAnthropicMessages,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-atomic-new"},
			Models:         []string{}, // skip discovery
		},
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	if result.Provider.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("BaseURL = %q", result.Provider.BaseURL)
	}
	var defaultView *ProviderGroupView
	for i := range result.Provider.Groups {
		if result.Provider.Groups[i].ID == config.DefaultGroupID {
			defaultView = &result.Provider.Groups[i]
			break
		}
	}
	if defaultView == nil || defaultView.Protocol != config.ProtocolAnthropicMessages || defaultView.APIKeyCount != 1 {
		t.Fatalf("default group view = %#v", defaultView)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("persisted BaseURL = %q", provider.BaseURL)
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	premium := provider.FindGroup("premium")
	if defaultGroup == nil || premium == nil {
		t.Fatalf("groups = %#v", provider.Groups)
	}
	if defaultGroup.Protocol != config.ProtocolAnthropicMessages {
		t.Fatalf("default protocol = %q", defaultGroup.Protocol)
	}
	if !reflect.DeepEqual(defaultGroup.EffectiveAPIKeys(), []string{"sk-atomic-new"}) {
		t.Fatalf("default keys = %#v", defaultGroup.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(premium.EffectiveAPIKeys(), []string{"sk-premium-secret"}) {
		t.Fatalf("premium keys mutated: %#v", premium.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(premium.Models, []string{"premium-model"}) {
		t.Fatalf("premium models mutated: %#v", premium.Models)
	}
}

func TestUpsertProviderAtomicDefaultGroupUpdateFailsWithoutPartialCommit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)

	// Invalid nested group must reject before commit — shared fields stay old.
	_, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "vendor-a",
		BaseURL: "https://should-not-persist.example.com/v1",
		DefaultGroup: &ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Name:           config.DefaultGroupName,
			Protocol:       "not-a-valid-protocol",
			APIKeysChanged: true,
			APIKeys:        []string{"sk-should-not-persist"},
			Models:         []string{},
		},
	})
	if err == nil {
		t.Fatal("expected invalid defaultGroup protocol to fail")
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL partially committed: %q", provider.BaseURL)
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	if defaultGroup == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(defaultGroup.EffectiveAPIKeys(), []string{"sk-default-secret"}) {
		t.Fatalf("default keys partially committed: %#v", defaultGroup.EffectiveAPIKeys())
	}
	if defaultGroup.Protocol != config.ProtocolOpenAIResponses {
		t.Fatalf("default protocol partially committed: %q", defaultGroup.Protocol)
	}
	premium := provider.FindGroup("premium")
	if premium == nil || !reflect.DeepEqual(premium.EffectiveAPIKeys(), []string{"sk-premium-secret"}) {
		t.Fatalf("premium group disturbed: %#v", premium)
	}
}

func TestUpsertProviderDefaultGroupNilKeepsAllGroups(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)

	// Nil DefaultGroup is shared-only: groups (including default auth) untouched.
	_, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
		ID:      "vendor-a",
		Name:    "Renamed Only",
		BaseURL: "https://example.com/v1",
	})
	if err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.Name != "Renamed Only" {
		t.Fatalf("Name = %q", provider.Name)
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	premium := provider.FindGroup("premium")
	if defaultGroup == nil || premium == nil {
		t.Fatalf("groups = %#v", provider.Groups)
	}
	if !reflect.DeepEqual(defaultGroup.EffectiveAPIKeys(), []string{"sk-default-secret"}) {
		t.Fatalf("default keys = %#v", defaultGroup.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(premium.EffectiveAPIKeys(), []string{"sk-premium-secret"}) {
		t.Fatalf("premium keys = %#v", premium.EffectiveAPIKeys())
	}
	if defaultGroup.ModelsSource != "discovered" || premium.ModelsSource != "discovered" {
		t.Fatalf("models sources = default %q premium %q", defaultGroup.ModelsSource, premium.ModelsSource)
	}
}

func TestRefreshProviderModelsDoesNotResurrectConcurrentSiblingDelete(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"default-refreshed"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{
			{
				ID: config.DefaultGroupID, Name: config.DefaultGroupName,
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default-secret",
				Models: []string{"default-old"}, ModelsSource: "discovered",
			},
			{
				ID: "premium", Name: "Premium",
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium-secret",
				Models: []string{"premium-model"}, ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type refreshResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan refreshResult, 1)
	go func() {
		result, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{
			ID:    "vendor-a",
			Group: config.DefaultGroupID,
		})
		done <- refreshResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	// Concurrent sibling delete while discovery is in-flight.
	if err := svc.DeleteProviderGroup(context.Background(), ProviderGroupDeleteInput{
		ProviderID: "vendor-a",
		GroupID:    "premium",
	}); err != nil {
		close(release)
		t.Fatalf("DeleteProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", got.err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.FindGroup("premium") != nil {
		t.Fatalf("premium sibling resurrected: %#v", provider.Groups)
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	if defaultGroup == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(defaultGroup.Models, []string{"default-refreshed"}) || defaultGroup.ModelsSource != "discovered" {
		t.Fatalf("default catalog = %#v", defaultGroup)
	}
}

func TestRefreshProviderModelsRevisionConflictOnConcurrentTargetAuthChange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"stale-discovery"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-old",
			Models: []string{"keep-me"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type refreshResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan refreshResult, 1)
	go func() {
		result, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{
			ID: "vendor-a",
		})
		done <- refreshResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	// Concurrent target-group key rotation invalidates discovery fingerprint.
	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:             config.DefaultGroupID,
			Name:           config.DefaultGroupName,
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-rotated"},
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err == nil {
		t.Fatal("expected revision_conflict after concurrent auth change")
	}
	var outcome *OutcomeError
	if !errors.As(got.err, &outcome) || outcome.Code != "revision_conflict" {
		t.Fatalf("error = %v, want revision_conflict", got.err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	// Stale discovery must not overwrite catalog; rotated key must remain.
	// Concurrent key rotation intentionally marks the catalog untrusted (ModelsSource=""),
	// but must keep the pre-discovery model list (keep-me) rather than applying stale-discovery.
	if !reflect.DeepEqual(group.EffectiveAPIKeys(), []string{"sk-rotated"}) {
		t.Fatalf("keys = %#v, want rotated key kept", group.EffectiveAPIKeys())
	}
	if !reflect.DeepEqual(group.Models, []string{"keep-me"}) {
		t.Fatalf("catalog overwritten by stale discovery: models=%#v", group.Models)
	}
	if group.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty after concurrent auth change", group.ModelsSource)
	}
}

func TestUpsertProviderDiscoveryDoesNotResurrectConcurrentSiblingKeyChange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"upsert-discovered"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		Name:    "Vendor A",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{
			{
				ID: config.DefaultGroupID, Name: config.DefaultGroupName,
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default-secret",
				Models: []string{"default-old"}, ModelsSource: "discovered",
			},
			{
				ID: "premium", Name: "Premium",
				Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium-secret",
				Models: []string{"premium-model"}, ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type upsertResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan upsertResult, 1)
	go func() {
		// Models == nil triggers discovery; concurrent sibling key must survive.
		result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
			ID:      "vendor-a",
			Name:    "Vendor A Renamed",
			BaseURL: upstream.URL + "/v1",
			DefaultGroup: &ProviderGroupInput{
				ID:       config.DefaultGroupID,
				Name:     config.DefaultGroupName,
				Protocol: config.ProtocolOpenAIResponses,
			},
		})
		done <- upsertResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    "premium",
		Group: ProviderGroupInput{
			ID:             "premium",
			Name:           "Premium",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: true,
			APIKeys:        []string{"sk-premium-rotated"},
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup(premium) error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("UpsertProvider() error = %v", got.err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	if provider.Name != "Vendor A Renamed" {
		t.Fatalf("Name = %q", provider.Name)
	}
	premium := provider.FindGroup("premium")
	if premium == nil {
		t.Fatalf("premium missing: %#v", provider.Groups)
	}
	if !reflect.DeepEqual(premium.EffectiveAPIKeys(), []string{"sk-premium-rotated"}) {
		t.Fatalf("premium keys overwritten by stale upsert snapshot: %#v", premium.EffectiveAPIKeys())
	}
	defaultGroup := provider.FindGroup(config.DefaultGroupID)
	if defaultGroup == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(defaultGroup.Models, []string{"upsert-discovered"}) || defaultGroup.ModelsSource != "discovered" {
		t.Fatalf("default catalog = %#v", defaultGroup)
	}
}

func TestRefreshProviderModelsFailureDoesNotOverwriteConcurrentCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
			Models: []string{"pre-discovery"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type refreshResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan refreshResult, 1)
	go func() {
		result, err := svc.RefreshProviderModels(context.Background(), ProviderRefreshModelsInput{
			ID:    "vendor-a",
			Group: config.DefaultGroupID,
		})
		done <- refreshResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	// Concurrent catalog write while discovery fails; must survive commit.
	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:       config.DefaultGroupID,
			Name:     config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses,
			Models:   []string{"concurrent-live"},
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", got.err)
	}
	if !containsWarning(got.result.Warnings, "could not discover provider models") {
		t.Fatalf("warnings %#v missing discovery failure", got.result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(group.Models, []string{"concurrent-live"}) {
		t.Fatalf("catalog overwritten by failed discovery pre-snapshot: %#v", group.Models)
	}
	// Explicit models write is manual (untrusted source).
	if group.ModelsSource != "" {
		t.Fatalf("ModelsSource = %q, want empty after explicit models write", group.ModelsSource)
	}
}

func TestRefreshProviderGroupModelsEmptyDoesNotOverwriteConcurrentCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
			Models: []string{"pre-discovery"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type refreshResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan refreshResult, 1)
	go func() {
		// RefreshProviderGroupModels shares commit semantics with RefreshProviderModels.
		result, err := svc.RefreshProviderGroupModels(context.Background(), ProviderGroupRefreshModelsInput{
			ProviderID: "vendor-a",
			GroupID:    config.DefaultGroupID,
		})
		done <- refreshResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:       config.DefaultGroupID,
			Name:     config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses,
			Models:   []string{"concurrent-empty-live"},
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("RefreshProviderGroupModels() error = %v", got.err)
	}
	if !containsWarning(got.result.Warnings, "returned no models") {
		t.Fatalf("warnings %#v missing empty-result note", got.result.Warnings)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(group.Models, []string{"concurrent-empty-live"}) {
		t.Fatalf("catalog overwritten by empty discovery pre-snapshot: %#v", group.Models)
	}
}

func TestUpsertProviderDiscoveryFailureDoesNotOverwriteConcurrentCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		Name:    "Vendor A",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: "Original Default",
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
			Models: []string{"pre-discovery"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type upsertResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan upsertResult, 1)
	go func() {
		// Models == nil triggers discovery; failure must not clobber concurrent catalog.
		result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
			ID:      "vendor-a",
			Name:    "Vendor A",
			BaseURL: upstream.URL + "/v1",
			DefaultGroup: &ProviderGroupInput{
				ID: config.DefaultGroupID,
				// Name omitted: must not freeze pre-network name over concurrent rename.
				Protocol: config.ProtocolOpenAIResponses,
			},
		})
		done <- upsertResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:       config.DefaultGroupID,
			Name:     "Concurrent Rename",
			Protocol: config.ProtocolOpenAIResponses,
			Models:   []string{"upsert-concurrent-live"},
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("UpsertProvider() error = %v", got.err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	if group.Name != "Concurrent Rename" {
		t.Fatalf("Name = %q, want concurrent rename preserved (omitted input must not freeze old name)", group.Name)
	}
	if !reflect.DeepEqual(group.Models, []string{"upsert-concurrent-live"}) {
		t.Fatalf("catalog overwritten by failed discovery: %#v", group.Models)
	}
}

func TestUpsertProviderOmittedDefaultGroupNamePreservesConcurrentRename(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"discovered-after-rename"}]}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		Name:    "Vendor A",
		BaseURL: upstream.URL + "/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: "Pre Network Name",
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
			Models: []string{"old"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	type upsertResult struct {
		result ProviderSaveResult
		err    error
	}
	done := make(chan upsertResult, 1)
	go func() {
		result, err := svc.UpsertProvider(context.Background(), ProviderUpsertInput{
			ID:      "vendor-a",
			Name:    "Vendor A",
			BaseURL: upstream.URL + "/v1",
			DefaultGroup: &ProviderGroupInput{
				ID:       config.DefaultGroupID,
				Protocol: config.ProtocolOpenAIResponses,
			},
		})
		done <- upsertResult{result: result, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}

	if _, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
		Group: ProviderGroupInput{
			ID:       config.DefaultGroupID,
			Name:     "Live Concurrent Name",
			Protocol: config.ProtocolOpenAIResponses,
		},
	}); err != nil {
		close(release)
		t.Fatalf("UpdateProviderGroup() error = %v", err)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("UpsertProvider() error = %v", got.err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	if group.Name != "Live Concurrent Name" {
		t.Fatalf("Name = %q, want Live Concurrent Name", group.Name)
	}
	if !reflect.DeepEqual(group.Models, []string{"discovered-after-rename"}) || group.ModelsSource != "discovered" {
		t.Fatalf("successful discovery catalog = %#v", group)
	}
}

func TestRefreshProviderModelsCancelPropagates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	entered := make(chan struct{})
	// Hang on request body wait tied to request ctx so caller cancel aborts I/O.
	// Two BaseURLs: if Background() were used, cancel would not stop the in-flight
	// probe and the refresh would not return promptly.
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-r.Context().Done()
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstreamB.Close()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:       "vendor-a",
		BaseURL:  upstreamA.URL + "/v1",
		BaseURLs: []string{upstreamA.URL + "/v1", upstreamB.URL + "/v1"},
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Name: config.DefaultGroupName,
			Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
			Models: []string{"keep-me"}, ModelsSource: "discovered",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type refreshResult struct {
		err error
	}
	done := make(chan refreshResult, 1)
	started := time.Now()
	go func() {
		_, err := svc.RefreshProviderModels(ctx, ProviderRefreshModelsInput{
			ID:    "vendor-a",
			Group: config.DefaultGroupID,
		})
		done <- refreshResult{err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery to start")
	}
	cancel()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("expected cancel error from RefreshProviderModels")
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", got.err)
		}
		// Must return promptly (not wait on hung Background probes).
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("cancel took %v, want prompt propagation", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancel to abort discovery")
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil {
		t.Fatal("provider missing")
	}
	group := provider.FindGroup(config.DefaultGroupID)
	if group == nil {
		t.Fatal("default group missing")
	}
	if !reflect.DeepEqual(group.Models, []string{"keep-me"}) || group.ModelsSource != "discovered" {
		t.Fatalf("cancel must not commit: catalog = %#v", group)
	}
}

func TestDeleteProviderGroupLastGroupBlocked(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{{
			ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default",
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)
	err = svc.DeleteProviderGroup(context.Background(), ProviderGroupDeleteInput{
		ProviderID: "vendor-a",
		GroupID:    config.DefaultGroupID,
	})
	if err == nil {
		t.Fatal("expected last-group delete to fail")
	}
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "plan_not_executable" {
		t.Fatalf("error = %v, want plan_not_executable", err)
	}
}

func TestDeleteProviderGroupProtectedTargetRequiresSelection(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium"},
		},
	})
	cfg.UpsertAlias(config.Alias{
		Alias:   "manual-model",
		Enabled: true,
		Targets: []config.Target{{
			Provider: "vendor-a",
			Group:    "premium",
			Model:    "m1",
			Enabled:  true,
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)

	// Without selections, protected target blocks deletion (no silent rebind).
	err = svc.DeleteProviderGroup(context.Background(), ProviderGroupDeleteInput{
		ProviderID: "vendor-a",
		GroupID:    "premium",
	})
	if err == nil {
		t.Fatal("expected protected target to block delete")
	}
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "plan_not_executable" {
		t.Fatalf("error = %v", err)
	}

	// Explicit remove_target selection executes (choice IDs come from the plan, not hard-coded).
	rev, live, err := svc.SnapshotConfigRevision(context.Background())
	if err != nil {
		t.Fatalf("SnapshotConfigRevision() error = %v", err)
	}
	planned, err := lifecycle.PlanGroupRemove(live, string(rev), "vendor-a", "premium", nil)
	if err != nil {
		t.Fatalf("PlanGroupRemove() error = %v", err)
	}
	if len(planned.Plan.Choices) == 0 {
		t.Fatal("expected protected_target choices")
	}
	selections := make([]lifecycle.Selection, 0, len(planned.Plan.Choices))
	for _, choice := range planned.Plan.Choices {
		if choice.Code != lifecycle.ReasonProtectedTarget {
			continue
		}
		selections = append(selections, lifecycle.Selection{
			ChoiceID: choice.ID,
			OptionID: lifecycle.OptionRemoveTarget,
		})
	}
	if len(selections) == 0 {
		t.Fatalf("choices = %#v", planned.Plan.Choices)
	}
	err = svc.DeleteProviderGroup(context.Background(), ProviderGroupDeleteInput{
		ProviderID: "vendor-a", GroupID: "premium", Selections: selections,
	})
	var revisionOutcome *OutcomeError
	if !errors.As(err, &revisionOutcome) || revisionOutcome.Code != "revision_required" {
		t.Fatalf("DeleteProviderGroup without expected revision error = %v", err)
	}
	err = svc.DeleteProviderGroup(context.Background(), ProviderGroupDeleteInput{
		ProviderID:       "vendor-a",
		GroupID:          "premium",
		Selections:       selections,
		ExpectedRevision: rev,
	})
	if err != nil {
		t.Fatalf("DeleteProviderGroup with selection error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.FindProvider("vendor-a").FindGroup("premium") != nil {
		t.Fatal("premium group still present")
	}
	if alias := reloaded.FindAlias("manual-model"); alias == nil || len(alias.Targets) != 0 {
		t.Fatalf("alias targets = %#v", alias)
	}
}

func TestProviderDiscoveryFingerprintIncludesCatalog(t *testing.T) {
	t.Parallel()
	provider := config.Provider{
		ID: "p1", BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKeys: []string{"sk"}, Models: []string{"before"}, ModelsSource: "manual"}},
	}
	fingerprint := captureProviderDiscoveryFingerprint(provider, config.DefaultGroupID)
	provider.Groups[0].Models = []string{"concurrent"}
	if fingerprint.MatchesProviderGroupCatalog(provider, config.DefaultGroupID) {
		t.Fatal("concurrent catalog edit must invalidate discovery fingerprint")
	}
}

func TestAutoAliasReconcileRunsWhenGenerationDisabled(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.AutoAliasEnabled = false
	providerEnabled := false
	provider := config.Provider{
		ID: "p1", BaseURL: "https://example.com/v1", AutoAliasEnabled: &providerEnabled,
		Groups: []config.ProviderGroup{{ID: "premium", Protocol: config.ProtocolOpenAIResponses, Models: []string{"current"}, ModelsSource: "discovered"}},
	}
	cfg.Aliases = []config.Alias{{
		Alias: "stale", AutoGenerated: true, Enabled: true,
		Targets: []config.Target{{Provider: "p1", Group: "premium", Model: "stale", Enabled: true, AutoGenerated: true}},
	}}
	warnings := appendAutoAliasWarnings(cfg, provider)
	if cfg.FindAutoAlias("stale") != nil {
		t.Fatal("stale system target must be revoked while generation is disabled")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "removed stale") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestExecuteLifecycleRejectsExpiredOriginalPlanToken(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-expiry")
	svc := NewService(path)
	ctx := context.Background()
	rev, _, err := svc.SnapshotConfigRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(lifecycle.GroupIDChangePayload{ProviderID: "vendor-expiry", OldGroupID: "premium", NewGroupID: "gold"})
	if err != nil {
		t.Fatal(err)
	}
	op := lifecycle.Operation{Kind: lifecycle.OpGroupIDChange, Payload: payload}
	expiredAt := time.Now().Add(-time.Minute)
	plan, err := svc.previewLifecycle(ctx, LifecyclePreviewInput{Revision: rev, Operation: op}, &expiredAt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ExecuteLifecycle(ctx, LifecycleExecuteInput{Revision: rev, PlanToken: plan.PlanToken, Operation: op})
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "plan_expired" {
		t.Fatalf("ExecuteLifecycle() error = %v, want plan_expired", err)
	}
	validPlan, err := svc.PreviewLifecycle(ctx, LifecyclePreviewInput{Revision: rev, Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(validPlan.PlanToken, ".")
	if len(parts) != 3 {
		t.Fatalf("plan token = %q", validPlan.PlanToken)
	}
	parts[1] = fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix())
	_, err = svc.ExecuteLifecycle(ctx, LifecycleExecuteInput{Revision: rev, PlanToken: strings.Join(parts, "."), Operation: op})
	if !errors.As(err, &outcome) || outcome.Code != "plan_mismatch" {
		t.Fatalf("tampered expiry error = %v, want plan_mismatch", err)
	}
}

func TestUpdateProviderGroupIDChangeUsesLifecycle(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Name: "Premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium", Models: []string{"m1"}},
		},
	})
	cfg.UpsertAlias(config.Alias{
		Alias:         "auto-m",
		Protocol:      config.ProtocolOpenAIResponses,
		Enabled:       true,
		AutoGenerated: true,
		Targets: []config.Target{{
			Provider: "vendor-a", Group: "premium", Model: "m1", Enabled: true, AutoGenerated: true,
		}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
	svc := NewService(path)
	view, err := svc.UpdateProviderGroup(context.Background(), ProviderGroupUpdateInput{
		ProviderID: "vendor-a",
		GroupID:    "premium",
		Group: ProviderGroupInput{
			ID:             "gold",
			Name:           "Gold",
			Protocol:       config.ProtocolOpenAIResponses,
			APIKeysChanged: false,
			Models:         []string{"m1"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateProviderGroup ID change error = %v", err)
	}
	if view.ID != "gold" || view.Name != "Gold" {
		t.Fatalf("view = %#v", view)
	}
	assertNoPlaintextKeys(t, view)

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider.FindGroup("premium") != nil || provider.FindGroup("gold") == nil {
		t.Fatalf("groups = %#v", provider.Groups)
	}
	alias := reloaded.FindAutoAlias("auto-m")
	if alias == nil || len(alias.Targets) != 1 || alias.Targets[0].Group != "gold" {
		t.Fatalf("alias targets not retargeted: %#v", alias)
	}
}

func TestUpdateProviderGroupIDChangeRejectsPreviewRevisionDrift(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor-a",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses},
			{ID: "premium", Name: "Premium", Protocol: config.ProtocolOpenAIResponses, Models: []string{"m1"}},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	ctx := context.Background()
	previewRevision, err := svc.GetConfigRevision(ctx)
	if err != nil {
		t.Fatalf("GetConfigRevision() error = %v", err)
	}
	if _, err := svc.CreateProviderGroup(ctx, ProviderGroupCreateInput{
		ProviderID: "vendor-a",
		Group: ProviderGroupInput{
			ID:       "concurrent",
			Name:     "Concurrent",
			Protocol: config.ProtocolOpenAIResponses,
		},
	}); err != nil {
		t.Fatalf("CreateProviderGroup() error = %v", err)
	}

	_, err = svc.UpdateProviderGroup(ctx, ProviderGroupUpdateInput{
		ProviderID:       "vendor-a",
		GroupID:          "premium",
		ExpectedRevision: previewRevision,
		Group: ProviderGroupInput{
			ID:       "gold",
			Name:     "Gold",
			Protocol: config.ProtocolOpenAIResponses,
			Models:   []string{"m1"},
		},
	})
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Code != "revision_conflict" {
		t.Fatalf("UpdateProviderGroup() error = %v, want revision_conflict", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil || provider.FindGroup("premium") == nil || provider.FindGroup("gold") != nil || provider.FindGroup("concurrent") == nil {
		t.Fatalf("groups after conflict = %#v", provider)
	}
}

func TestUpsertRequestRewriteRuleProviderGroups(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	svc := NewService(path)
	ctx := context.Background()

	// Explicit precise groups (including non-default) — never expand to siblings.
	view, err := svc.UpsertRequestRewriteRule(ctx, RequestRewriteRuleInput{
		Name:     "group-scope",
		Alias:    "chat-fast",
		Enabled:  true,
		Override: true,
		ProviderGroups: []ProviderGroupSelectorInput{
			{Provider: "vendor-a", Group: "premium"},
			{Provider: "vendor-a", Group: "premium"}, // dedupe
			{Provider: "vendor-b", Group: config.DefaultGroupID},
		},
		Ops: []config.RequestRewriteOperation{{Op: config.RequestRewriteOpDelete, Path: "$.store"}},
	})
	if err != nil {
		t.Fatalf("UpsertRequestRewriteRule(providerGroups) error = %v", err)
	}
	want := []ProviderGroupSelectorView{
		{Provider: "vendor-a", Group: "premium"},
		{Provider: "vendor-b", Group: config.DefaultGroupID},
	}
	if !reflect.DeepEqual(view.ProviderGroups, want) {
		t.Fatalf("ProviderGroups = %#v, want %#v", view.ProviderGroups, want)
	}
	// Explicit empty providerGroups is wildcard (distinct from omitted).
	wild, err := svc.UpsertRequestRewriteRule(ctx, RequestRewriteRuleInput{
		Name:           "wildcard-groups",
		Alias:          "chat-fast",
		Enabled:        true,
		Override:       true,
		ProviderGroups: []ProviderGroupSelectorInput{},
		Ops:            []config.RequestRewriteOperation{{Op: config.RequestRewriteOpDelete, Path: "$.n"}},
	})
	if err != nil {
		t.Fatalf("UpsertRequestRewriteRule(wildcard) error = %v", err)
	}
	if wild.ProviderGroups == nil || len(wild.ProviderGroups) != 0 {
		t.Fatalf("wildcard ProviderGroups = %#v", wild.ProviderGroups)
	}
	if _, err := svc.UpsertRequestRewriteRule(ctx, RequestRewriteRuleInput{
		Name: "malformed-scope", Alias: "chat-fast", Enabled: true,
		ProviderGroups: []ProviderGroupSelectorInput{{Provider: "vendor-a"}},
		Ops:            []config.RequestRewriteOperation{{Op: config.RequestRewriteOpDelete, Path: "$.n"}},
	}); err == nil || !strings.Contains(err.Error(), "provider and group") {
		t.Fatalf("malformed selector error = %v", err)
	}
}

func TestBindAliasTargetRequiresExactGroup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)
	ctx := context.Background()

	// Missing group must not fall back to default.
	_, err := svc.BindAliasTarget(ctx, AliasTargetInput{
		Alias:    "m1",
		Provider: "vendor-a",
		Group:    "missing",
		Model:    "premium-model",
	})
	if err == nil || !strings.Contains(err.Error(), "group") {
		t.Fatalf("expected missing group error, got %v", err)
	}

	// Empty group is rejected at the management boundary.
	_, err = svc.BindAliasTarget(ctx, AliasTargetInput{
		Alias:    "default-model",
		Provider: "vendor-a",
		Model:    "default-model",
	})
	if err == nil || !strings.Contains(err.Error(), "group") {
		t.Fatalf("expected required group error, got %v", err)
	}

	// Explicit premium group.
	view, err := svc.BindAliasTarget(ctx, AliasTargetInput{
		Alias:    "premium-model",
		Provider: "vendor-a",
		Group:    "premium",
		Model:    "premium-model",
	})
	if err != nil {
		t.Fatalf("BindAliasTarget(premium) error = %v", err)
	}
	if len(view.Targets) != 1 || view.Targets[0].Group != "premium" {
		t.Fatalf("premium targets = %#v", view.Targets)
	}
}

func TestIsMaskedAPIKeyPlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"sk-fake-primary-aaaa", false},
		{"sk-f…aaaa", true},
		{"sk-****bbbb", true},
		{"***", true},
		{"••••••••", true},
		{"", false},
	}
	for _, tc := range cases {
		if got := isMaskedAPIKeyPlaceholder(tc.in); got != tc.want {
			t.Fatalf("isMaskedAPIKeyPlaceholder(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLifecycleAPIGroupIDChangePreviewAndExecute(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	svc := NewService(path)
	ctx := context.Background()
	rev, _, err := svc.SnapshotConfigRevision(ctx)
	if err != nil {
		t.Fatalf("SnapshotConfigRevision() error = %v", err)
	}
	payload, err := json.Marshal(lifecycle.GroupIDChangePayload{
		ProviderID: "vendor-a", OldGroupID: "premium", NewGroupID: "gold",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	op := lifecycle.Operation{Kind: lifecycle.OpGroupIDChange, Payload: payload}
	plan, err := svc.PreviewLifecycle(ctx, LifecyclePreviewInput{Revision: rev, Operation: op})
	if err != nil {
		t.Fatalf("PreviewLifecycle() error = %v", err)
	}
	if !plan.Executable || plan.PlanToken == "" || plan.OperationKind != lifecycle.OpGroupIDChange {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := svc.ExecuteLifecycle(ctx, LifecycleExecuteInput{
		Revision: rev, PlanToken: plan.PlanToken, Operation: op,
	}); err != nil {
		t.Fatalf("ExecuteLifecycle() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := reloaded.FindProvider("vendor-a")
	if provider == nil || provider.FindGroup("premium") != nil || provider.FindGroup("gold") == nil {
		t.Fatalf("provider groups = %#v", provider)
	}
}

func TestLifecycleAPIExecuteReplaysSignedExternalRefs(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	seedTwoGroupProvider(t, path, "vendor-a")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertAlias(config.Alias{Alias: "chat", Enabled: true, Targets: []config.Target{{
		Provider: "vendor-a", Group: config.DefaultGroupID, Model: "default-model", Enabled: true,
	}}})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}

	svc := NewService(path)
	ctx := context.Background()
	rev, _, err := svc.SnapshotConfigRevision(ctx)
	if err != nil {
		t.Fatalf("SnapshotConfigRevision() error = %v", err)
	}
	payload, err := json.Marshal(lifecycle.AliasRemovePayload{Alias: "chat"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	op := lifecycle.Operation{Kind: lifecycle.OpAliasRemove, Payload: payload}
	external := lifecycle.ExternalRefs{OpenCodeModel: "vendor-a/chat"}
	plan, err := svc.PreviewLifecycle(ctx, LifecyclePreviewInput{
		Revision: rev, Operation: op, ExternalOpenCode: external,
	})
	if err != nil {
		t.Fatalf("PreviewLifecycle() error = %v", err)
	}
	if !plan.Executable || plan.PlanToken == "" || len(plan.PreservedIssues) == 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := svc.ExecuteLifecycle(ctx, LifecycleExecuteInput{
		Revision: rev, PlanToken: plan.PlanToken, Operation: op,
	}); func() string {
		var outcome *OutcomeError
		if errors.As(err, &outcome) {
			return outcome.Code
		}
		return ""
	}() != "plan_mismatch" {
		t.Fatalf("ExecuteLifecycle(without external refs) error = %v, want plan_mismatch", err)
	}
	if _, err := svc.ExecuteLifecycle(ctx, LifecycleExecuteInput{
		Revision: rev, PlanToken: plan.PlanToken, Operation: op, ExternalOpenCode: external,
	}); err != nil {
		t.Fatalf("ExecuteLifecycle() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.FindAlias("chat") != nil {
		t.Fatalf("alias chat still exists after lifecycle execute")
	}
}

func seedTwoGroupProvider(t *testing.T, path, providerID string) {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      providerID,
		Name:    "Vendor Fake A",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{
				ID:           config.DefaultGroupID,
				Name:         config.DefaultGroupName,
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-default-secret",
				Models:       []string{"default-model"},
				ModelsSource: "discovered",
			},
			{
				ID:           "premium",
				Name:         "Premium Fake Group",
				Protocol:     config.ProtocolOpenAIResponses,
				APIKey:       "sk-premium-secret",
				Models:       []string{"premium-model"},
				ModelsSource: "discovered",
			},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save() error = %v", err)
	}
}

func assertStoredKeys(t *testing.T, path, providerID, groupID string, want []string) {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	provider := cfg.FindProvider(providerID)
	if provider == nil {
		t.Fatalf("provider %q missing", providerID)
	}
	group := provider.FindGroup(groupID)
	if group == nil {
		t.Fatalf("group %q missing", groupID)
	}
	got := group.EffectiveAPIKeys()
	if len(want) == 0 {
		if len(got) != 0 {
			t.Fatalf("stored keys = %#v, want empty", got)
		}
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored keys = %#v, want %#v", got, want)
	}
}

func assertNoPlaintextKeys(t *testing.T, view ProviderGroupView) {
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
	// Masked values are allowed; full secrets are not.
	for _, secret := range []string{
		"sk-fake-primary-aaaa",
		"sk-fake-backup-bbbb",
		"sk-fake-new-key-cccc",
		"sk-fake-rotate-dddd",
		"sk-default-secret",
		"sk-premium-secret",
		"sk-create",
	} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("view leaked plaintext secret %q: %s", secret, string(raw))
		}
	}
}

func readManageFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	pathParts := append([]string{"testdata", "provider_groups"}, parts...)
	path := filepath.Join(pathParts...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}
