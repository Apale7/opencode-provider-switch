package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func TestSaveLanguagePersistsDesktopPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	svc := app.NewService(configPath)
	if _, err := svc.SaveDesktopPrefs(context.Background(), app.DesktopPrefsInput{Theme: "dark", Language: "en-US"}); err != nil {
		t.Fatalf("seed prefs: %v", err)
	}
	m := model{svc: svc, ctx: context.Background(), lang: langEN}
	msg := m.saveLanguage(langZH)()
	result, ok := msg.(saveLanguageMsg)
	if !ok {
		t.Fatalf("saveLanguage msg = %T", msg)
	}
	if result.err != nil {
		t.Fatalf("saveLanguage error = %v", result.err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Desktop.Language != langZH {
		t.Fatalf("stored language = %q, want %q", cfg.Desktop.Language, langZH)
	}
	if cfg.Desktop.Theme != "dark" {
		t.Fatalf("stored theme = %q, want preserved dark", cfg.Desktop.Theme)
	}
}

func TestProviderFormIncludesMultiBaseURLFields(t *testing.T) {
	m := model{lang: langEN}
	p := app.ProviderView{ID: "p1", BaseURL: "https://one.example/v1", BaseURLs: []string{"https://one.example/v1", "https://two.example/v1"}, BaseURLStrategy: config.ProviderBaseURLStrategyLatency, Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}}
	m.openProviderForm(actionEditProvider, &p)
	fields := map[string]string{}
	for _, field := range m.formFields {
		fields[field.label] = field.value
	}
	if got := fields[m.t("field.baseURLs")]; got != "https://one.example/v1,https://two.example/v1" {
		t.Fatalf("baseURLs field = %q", got)
	}
	if got := fields[m.t("field.baseURLStrategy")]; got != config.ProviderBaseURLStrategyLatency {
		t.Fatalf("strategy field = %q", got)
	}
}

func TestBindTargetFormSelectsExactProviderGroup(t *testing.T) {
	m := model{lang: langEN, providers: []app.ProviderView{
		{ID: "with-default", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID}, {ID: "premium"}}},
		{ID: "premium-only", Groups: []app.ProviderGroupView{{ID: "premium"}}},
	}}
	m.openBindForm(&app.AliasView{Alias: "chat"})
	if got := formFieldValue(m.formFields, fieldKeyGroup); got != config.DefaultGroupID {
		t.Fatalf("initial group = %q, want default", got)
	}
	for i := range m.formFields {
		if m.formFields[i].key == fieldKeyProviderID {
			m.formFields[i].value = "premium-only"
		}
	}
	m.syncBindTargetGroup()
	if got := formFieldValue(m.formFields, fieldKeyGroup); got != "" {
		t.Fatalf("provider change selected sibling group implicitly: %q", got)
	}
	if got := m.groupOptions("premium-only"); len(got) != 1 || got[0] != "premium" {
		t.Fatalf("premium-only group options = %#v", got)
	}
	if got := providerGroupSummary(m.providers[0].Groups); !strings.Contains(got, "default") || !strings.Contains(got, "premium") {
		t.Fatalf("provider group summary = %q", got)
	}
	if translate(langEN, "field.group") != "Group" || translate(langZH, "field.group") != "分组" {
		t.Fatalf("group translations missing: en=%q zh=%q", translate(langEN, "field.group"), translate(langZH, "field.group"))
	}
}

func formFieldValue(fields []field, key string) string {
	for _, item := range fields {
		if item.key == key {
			return item.value
		}
	}
	return ""
}

func TestFormSelectConfirmsValueAndSubmitIsLastEnter(t *testing.T) {
	m := model{lang: langEN}
	m.openAliasForm(actionAddAlias, nil)
	m.formIndex = 2

	updated, cmd := m.updateForm("enter")
	if cmd != nil {
		t.Fatalf("open select returned cmd")
	}
	m = updated.(model)
	if !m.selectOpen {
		t.Fatalf("select not open")
	}

	updated, cmd = m.updateForm("down")
	if cmd != nil {
		t.Fatalf("move select returned cmd")
	}
	m = updated.(model)
	if got := m.formFields[2].value; got != config.ProtocolOpenAIResponses {
		t.Fatalf("protocol before confirm = %q", got)
	}

	updated, cmd = m.updateForm("enter")
	if cmd != nil {
		t.Fatalf("confirm select returned cmd")
	}
	m = updated.(model)
	if got := m.formFields[2].value; got != config.ProtocolAnthropicMessages {
		t.Fatalf("protocol after confirm = %q", got)
	}

	m.formIndex = len(m.formFields) - 1
	updated, _ = m.updateForm("enter")
	m = updated.(model)
	if m.err == "" || !strings.Contains(m.err, m.t("field.aliasName")) {
		t.Fatalf("last enter should submit and validate, err = %q", m.err)
	}
}

func TestProviderFormAcceptsBaseURLWithoutV1(t *testing.T) {
	m := model{lang: langEN}
	m.openProviderForm(actionAddProvider, nil)
	m.formFields[0].value = "p1"
	m.formFields[3].value = "https://api.example"

	if !m.validateFormFields() {
		t.Fatalf("expected form to be valid for base URL without /v1")
	}
	if got := m.formFields[3].err; got != "" {
		t.Fatalf("expected no error on base URL field, got %q", got)
	}
}

func TestMouseClickSelectsProviderRow(t *testing.T) {
	m := model{
		lang:          langEN,
		screen:        screenProviders,
		providerIndex: 0,
		providers: []app.ProviderView{
			{ID: "p1", BaseURL: "https://one.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: "https://two.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
	}

	updated, cmd := m.updateMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: m.contentStartY() + providerRowStartY() + 1})
	if cmd != nil {
		t.Fatalf("mouse row select returned cmd")
	}
	m = updated.(model)
	if m.providerIndex != 1 {
		t.Fatalf("providerIndex = %d, want 1", m.providerIndex)
	}
}

func TestMouseMotionHighlightsProviderRow(t *testing.T) {
	m := model{
		lang:          langEN,
		screen:        screenProviders,
		providerIndex: 0,
		providers: []app.ProviderView{
			{ID: "p1", BaseURL: "https://one.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: "https://two.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
	}

	updated, cmd := m.updateMouse(tea.MouseMsg{Action: tea.MouseActionMotion, X: 1, Y: m.contentStartY() + providerRowStartY() + 1})
	if cmd != nil {
		t.Fatalf("mouse hover returned cmd")
	}
	m = updated.(model)
	if m.providerIndex != 1 {
		t.Fatalf("providerIndex = %d, want 1", m.providerIndex)
	}
}

func TestMouseMotionHighlightsButton(t *testing.T) {
	m := model{
		lang:   langEN,
		screen: screenProviders,
		providers: []app.ProviderView{
			{ID: "p1", BaseURL: "https://one.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
	}
	buttons := m.providerButtons()
	x := runewidth.StringWidth("[ "+buttons[0].label+" ]") + 1

	updated, cmd := m.updateMouse(tea.MouseMsg{Action: tea.MouseActionMotion, X: x, Y: m.contentStartY() + actionButtonY()})
	if cmd != nil {
		t.Fatalf("mouse button hover returned cmd")
	}
	m = updated.(model)
	if m.hoverButton != buttons[1].key {
		t.Fatalf("hoverButton = %q, want %q", m.hoverButton, buttons[1].key)
	}
}

func TestLegacyMouseLeftSelectsProviderRow(t *testing.T) {
	m := model{
		lang:          langEN,
		screen:        screenProviders,
		providerIndex: 0,
		providers: []app.ProviderView{
			{ID: "p1", BaseURL: "https://one.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
			{ID: "p2", BaseURL: "https://two.example/v1", Groups: []app.ProviderGroupView{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses}}},
		},
	}

	updated, cmd := m.updateMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 1, Y: m.contentStartY() + providerRowStartY() + 1})
	if cmd != nil {
		t.Fatalf("legacy mouse row select returned cmd")
	}
	m = updated.(model)
	if m.providerIndex != 1 {
		t.Fatalf("providerIndex = %d, want 1", m.providerIndex)
	}
}

func TestButtonHitUsesDisplayWidth(t *testing.T) {
	buttons := []tuiButton{{key: "add", label: "添加 provider"}, {key: "refresh", label: "刷新"}}
	firstWidth := runewidth.StringWidth("[ " + buttons[0].label + " ]")

	if got := buttonKeyAt(firstWidth+1, buttons); got != "refresh" {
		t.Fatalf("button key = %q, want refresh", got)
	}
}

func TestOverviewShowsGuideAndLipglossLayout(t *testing.T) {
	m := model{lang: langEN, overview: app.Overview{ProviderCount: 0}}
	view := m.viewOverview()

	if !strings.Contains(view, m.t("overview.guideTitle")) {
		t.Fatalf("overview missing guide title: %q", view)
	}
	if !strings.Contains(view, m.t("overview.next", m.t("overview.step.provider"))) {
		t.Fatalf("overview missing next step: %q", view)
	}
}

func TestOverviewMouseOnlyActivatesGuidePanel(t *testing.T) {
	m := model{lang: langEN, screen: screenOverview, overview: app.Overview{ProviderCount: 1, AliasCount: 1}}

	updated, cmd := m.updateMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: overviewGuideStartX() - 1, Y: m.contentStartY() + overviewMenuStartY()})
	if cmd != nil {
		t.Fatalf("left panel click returned cmd")
	}
	m = updated.(model)
	if m.screen != screenOverview {
		t.Fatalf("left panel changed screen = %v", m.screen)
	}

	updated, cmd = m.updateMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: overviewGuideStartX(), Y: m.contentStartY() + overviewMenuStartY()})
	if cmd != nil {
		t.Fatalf("guide click returned cmd")
	}
	m = updated.(model)
	if m.screen != screenProviders {
		t.Fatalf("guide click screen = %v, want providers", m.screen)
	}
}

func TestProviderGroupsKeyEntryOpensGroupScreen(t *testing.T) {
	m := model{
		lang:   langEN,
		screen: screenProviders,
		providers: []app.ProviderView{
			{
				ID: "p1",
				Groups: []app.ProviderGroupView{
					{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKeyCount: 1, APIKeysMasked: []string{"sk-f…aaaa"}},
					{ID: "premium", Protocol: config.ProtocolAnthropicMessages, Models: []string{"m1"}},
				},
			},
		},
	}

	updated, cmd := m.updateProviders("g")
	if cmd != nil {
		t.Fatalf("g entry returned cmd")
	}
	m = updated.(model)
	if m.screen != screenGroups {
		t.Fatalf("screen = %v, want groups", m.screen)
	}
	if m.groupIndex != 0 {
		t.Fatalf("groupIndex = %d", m.groupIndex)
	}

	// Mouse/button entry via provider toolbar.
	m.screen = screenProviders
	updated, cmd = m.providerButtonClick("groups")
	if cmd != nil {
		t.Fatalf("groups button returned cmd")
	}
	m = updated.(model)
	if m.screen != screenGroups {
		t.Fatalf("button entry screen = %v, want groups", m.screen)
	}

	// Navigation within groups + edit form entry.
	updated, cmd = m.updateGroups("down")
	if cmd != nil {
		t.Fatalf("down returned cmd")
	}
	m = updated.(model)
	if m.groupIndex != 1 {
		t.Fatalf("groupIndex after down = %d, want 1", m.groupIndex)
	}
	updated, cmd = m.updateGroups("e")
	if cmd != nil {
		t.Fatalf("edit returned cmd")
	}
	m = updated.(model)
	if m.screen != screenForm || m.formAction != actionEditGroup {
		t.Fatalf("edit form screen/action = %v/%v", m.screen, m.formAction)
	}
	if m.formGroupID != "premium" || m.formProviderID != "p1" {
		t.Fatalf("form identity provider=%q group=%q", m.formProviderID, m.formGroupID)
	}
	if got := formFieldValue(m.formFields, fieldKeyAPIKeysMode); got != apiKeysModeKeep {
		t.Fatalf("edit apiKeys mode = %q, want keep", got)
	}
	if got := formFieldValue(m.formFields, fieldKeyAPIKeys); got != "" {
		t.Fatalf("edit form must not prefill plaintext keys, got %q", got)
	}
}

func TestGroupFormBuildInputThreeStateAPIKeys(t *testing.T) {
	// Create always sets keys from input.
	createFields := map[string]string{
		fieldKeyGroupID:   "premium",
		fieldKeyGroupName: "Premium",
		fieldKeyProtocol:  config.ProtocolOpenAIResponses,
		fieldKeyModels:    "m1,m2",
		fieldKeyDisabled:  "no",
		fieldKeyAPIKeys:   "sk-a, sk-b\nsk-c",
	}
	created := buildProviderGroupInputFromFields(createFields, false)
	if !created.APIKeysChanged {
		t.Fatal("create should set APIKeysChanged")
	}
	if len(created.APIKeys) != 3 || created.APIKeys[0] != "sk-a" || created.APIKeys[2] != "sk-c" {
		t.Fatalf("create keys = %#v", created.APIKeys)
	}
	if len(created.Models) != 2 {
		t.Fatalf("models = %#v", created.Models)
	}

	// Edit keep: preserve (changed=false, empty keys).
	keepFields := map[string]string{
		fieldKeyGroupID:     "premium",
		fieldKeyProtocol:    config.ProtocolOpenAIResponses,
		fieldKeyDisabled:    "no",
		fieldKeyAPIKeysMode: apiKeysModeKeep,
		fieldKeyAPIKeys:     "should-be-ignored",
	}
	kept := buildProviderGroupInputFromFields(keepFields, true)
	if kept.APIKeysChanged || len(kept.APIKeys) != 0 {
		t.Fatalf("keep = changed=%v keys=%#v", kept.APIKeysChanged, kept.APIKeys)
	}

	// Edit replace.
	replaceFields := map[string]string{
		fieldKeyGroupID:     "premium",
		fieldKeyProtocol:    config.ProtocolOpenAIResponses,
		fieldKeyDisabled:    "no",
		fieldKeyAPIKeysMode: apiKeysModeReplace,
		fieldKeyAPIKeys:     "sk-new-1\nsk-new-2",
	}
	replaced := buildProviderGroupInputFromFields(replaceFields, true)
	if !replaced.APIKeysChanged || len(replaced.APIKeys) != 2 {
		t.Fatalf("replace = changed=%v keys=%#v", replaced.APIKeysChanged, replaced.APIKeys)
	}

	// Edit clear.
	clearFields := map[string]string{
		fieldKeyGroupID:     "premium",
		fieldKeyProtocol:    config.ProtocolOpenAIResponses,
		fieldKeyDisabled:    "no",
		fieldKeyAPIKeysMode: apiKeysModeClear,
		fieldKeyAPIKeys:     "ignored",
	}
	cleared := buildProviderGroupInputFromFields(clearFields, true)
	if !cleared.APIKeysChanged || cleared.APIKeys != nil && len(cleared.APIKeys) != 0 {
		t.Fatalf("clear = changed=%v keys=%#v", cleared.APIKeysChanged, cleared.APIKeys)
	}
}

func TestParseGroupAPIKeysInputCommaAndNewline(t *testing.T) {
	got := parseGroupAPIKeysInput(" sk-1 , sk-2\n sk-3\r\nsk-4 ")
	want := []string{"sk-1", "sk-2", "sk-3", "sk-4"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
	if len(parseGroupAPIKeysInput("  , \n ")) != 0 {
		t.Fatal("empty tokens should be dropped")
	}
}

func TestGroupAPIKeysSummaryNeverShowsPlaintext(t *testing.T) {
	g := app.ProviderGroupView{
		ID:            "premium",
		APIKeyCount:   2,
		APIKeysMasked: []string{"sk-f…aaaa", "sk-f…bbbb"},
	}
	summary := formatGroupAPIKeysSummary(langEN, g)
	if strings.Contains(summary, "sk-fake") || strings.Contains(summary, "plaintext") {
		t.Fatalf("summary leaked plaintext: %q", summary)
	}
	if !strings.Contains(summary, "sk-f…aaaa") || !strings.Contains(summary, "2") {
		t.Fatalf("summary missing mask/count: %q", summary)
	}
	if got := formatGroupAPIKeysSummary(langZH, app.ProviderGroupView{}); got != translate(langZH, "groups.apiKeys.none") {
		t.Fatalf("empty zh summary = %q", got)
	}
}

func TestGroupLifecycleRemoveUsesGroupRemoveOperation(t *testing.T) {
	m := model{lang: langEN, screen: screenGroups, providers: []app.ProviderView{
		{ID: "p1", Groups: []app.ProviderGroupView{{ID: "premium"}}},
	}}
	m.groupIndex = 0
	updated, cmd := m.openImpactGroupRemove("p1", "premium")
	if cmd == nil {
		t.Fatal("expected lifecycle preview cmd")
	}
	m = updated
	if !m.impactActive {
		t.Fatal("impact not active")
	}
	if m.impactOp.Kind != "group.remove" {
		t.Fatalf("op kind = %q, want group.remove", m.impactOp.Kind)
	}
	if m.confirmAction != actionRemoveGroup {
		t.Fatalf("confirmAction = %v", m.confirmAction)
	}
	if m.previous != screenGroups {
		t.Fatalf("previous = %v, want groups", m.previous)
	}
	if !strings.Contains(m.impactSubject, "p1") || !strings.Contains(m.impactSubject, "premium") {
		t.Fatalf("subject = %q", m.impactSubject)
	}
	// Payload must be GroupRemovePayload JSON (providerId + groupId).
	if !strings.Contains(string(m.impactOp.Payload), `"providerId":"p1"`) || !strings.Contains(string(m.impactOp.Payload), `"groupId":"premium"`) {
		t.Fatalf("payload = %s", string(m.impactOp.Payload))
	}
}

func TestGroupLifecycleIDChangeUsesGroupIDChangeOperation(t *testing.T) {
	m := model{lang: langEN, screen: screenForm, formAction: actionEditGroup, formProviderID: "p1", formGroupID: "premium"}
	m.formFields = []field{
		{key: fieldKeyGroupID, value: "gold", kind: fieldText},
		{key: fieldKeyGroupName, value: "Gold", kind: fieldText},
		{key: fieldKeyProtocol, value: config.ProtocolOpenAIResponses, kind: fieldSelect, options: protocolOptions()},
		{key: fieldKeyModels, value: "m1", kind: fieldText},
		{key: fieldKeyDisabled, value: "no", kind: fieldSelect, options: yesNoOptions()},
		{key: fieldKeyAPIKeysMode, value: apiKeysModeKeep, kind: fieldSelect, options: apiKeysModeOptions()},
		{key: fieldKeyAPIKeys, value: "", kind: fieldText, mask: true},
		{key: "submit", label: "Save", kind: fieldSubmit},
	}
	updated, cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("expected lifecycle preview cmd for ID change")
	}
	m = updated.(model)
	if !m.impactActive {
		t.Fatal("impact not active for ID change")
	}
	if m.impactOp.Kind != lifecycle.OpGroupIDChange {
		t.Fatalf("op kind = %q, want %q", m.impactOp.Kind, lifecycle.OpGroupIDChange)
	}
	if m.confirmAction != actionEditGroup {
		t.Fatalf("confirmAction = %v, want edit group", m.confirmAction)
	}
	if m.pendingGroupUpdate == nil || m.pendingGroupUpdate.ID != "gold" {
		t.Fatalf("pending update = %#v", m.pendingGroupUpdate)
	}
	if m.pendingGroupProviderID != "p1" {
		t.Fatalf("pending provider = %q", m.pendingGroupProviderID)
	}
	if m.pendingGroupOldID != "premium" {
		t.Fatalf("pending old id = %q, want premium", m.pendingGroupOldID)
	}
	payload := string(m.impactOp.Payload)
	if !strings.Contains(payload, `"providerId":"p1"`) ||
		!strings.Contains(payload, `"oldGroupId":"premium"`) ||
		!strings.Contains(payload, `"newGroupId":"gold"`) {
		t.Fatalf("payload = %s", payload)
	}
	// Same ID must not enter lifecycle.
	m2 := model{lang: langEN, screen: screenForm, formAction: actionEditGroup, formProviderID: "p1", formGroupID: "premium", svc: nil}
	m2.formFields = []field{
		{key: fieldKeyGroupID, value: "premium", kind: fieldText},
		{key: fieldKeyProtocol, value: config.ProtocolOpenAIResponses, kind: fieldSelect, options: protocolOptions()},
		{key: fieldKeyDisabled, value: "no", kind: fieldSelect, options: yesNoOptions()},
		{key: fieldKeyAPIKeysMode, value: apiKeysModeKeep, kind: fieldSelect, options: apiKeysModeOptions()},
		{key: "submit", label: "Save", kind: fieldSubmit},
	}
	// Without svc, saveGroup cmd still returns a function; ensure no impact path.
	updated2, cmd2 := m2.submitForm()
	m2 = updated2.(model)
	if m2.impactActive {
		t.Fatal("same-ID edit must not open impact")
	}
	if cmd2 == nil {
		t.Fatal("same-ID edit should still schedule save")
	}
	if m2.pendingGroupUpdate != nil {
		t.Fatal("same-ID edit must not stash pending group update")
	}
}

func TestGroupIDChangeUpdateInputForwardsSelections(t *testing.T) {
	// executeImpact must pass preview-collected selections into UpdateProviderGroup.
	m := model{
		lang:                   langEN,
		pendingGroupProviderID: "p1",
		pendingGroupOldID:      "premium",
		pendingGroupUpdate: &app.ProviderGroupInput{
			ID:       "gold",
			Name:     "Gold",
			Protocol: config.ProtocolOpenAIResponses,
			Models:   []string{"m1"},
		},
		impactOp: lifecycle.Operation{Kind: lifecycle.OpGroupIDChange},
		impactSelections: map[string]lifecycle.Selection{
			"choice-protected": {ChoiceID: "choice-protected", OptionID: lifecycle.OptionRemoveTarget},
			"choice-rewrite":   {ChoiceID: "choice-rewrite", OptionID: lifecycle.OptionKeepDormant},
		},
	}
	in := groupIDChangeUpdateInput(
		m.pendingGroupProviderID,
		m.pendingGroupOldID,
		*m.pendingGroupUpdate,
		m.impactSelectionSlice(),
		app.ConfigRevision("preview-revision"),
	)
	if in.ProviderID != "p1" || in.GroupID != "premium" || in.Group.ID != "gold" {
		t.Fatalf("identity = provider=%q group=%q new=%q", in.ProviderID, in.GroupID, in.Group.ID)
	}
	if len(in.Selections) != 2 {
		t.Fatalf("selections = %#v, want 2 from impact preview", in.Selections)
	}
	if in.ExpectedRevision != app.ConfigRevision("preview-revision") {
		t.Fatalf("expected revision = %q", in.ExpectedRevision)
	}
	got := map[string]string{}
	for _, sel := range in.Selections {
		got[sel.ChoiceID] = sel.OptionID
	}
	if got["choice-protected"] != lifecycle.OptionRemoveTarget || got["choice-rewrite"] != lifecycle.OptionKeepDormant {
		t.Fatalf("selection options = %#v", got)
	}
}

func TestAPIKeysModeDisplayLocalizedStableValue(t *testing.T) {
	enKeep := apiKeysModeLabel(langEN, apiKeysModeKeep)
	zhKeep := apiKeysModeLabel(langZH, apiKeysModeKeep)
	if enKeep == "" || enKeep == apiKeysModeKeep {
		t.Fatalf("en keep label should be localized, got %q", enKeep)
	}
	if zhKeep == "" || zhKeep == apiKeysModeKeep {
		t.Fatalf("zh keep label should be localized, got %q", zhKeep)
	}
	if enKeep == zhKeep {
		t.Fatalf("en/zh keep labels should differ: %q", enKeep)
	}
	// Internal storage values remain stable English tokens.
	for _, mode := range apiKeysModeOptions() {
		if mode != apiKeysModeKeep && mode != apiKeysModeReplace && mode != apiKeysModeClear {
			t.Fatalf("unexpected mode option %q", mode)
		}
	}
	m := model{lang: langZH}
	display := m.fieldOptionDisplay(field{key: fieldKeyAPIKeysMode, value: apiKeysModeReplace}, apiKeysModeReplace)
	if display != apiKeysModeLabel(langZH, apiKeysModeReplace) {
		t.Fatalf("display = %q", display)
	}
	if display == apiKeysModeReplace {
		t.Fatalf("display should not be raw mode token, got %q", display)
	}
}

func TestGroupLocaleKeysBilingual(t *testing.T) {
	keys := []string{
		"screen.groups",
		"groups.help",
		"groups.empty",
		"groups.provider",
		"groups.detail",
		"groups.apiKeys.label",
		"groups.apiKeys.none",
		"groups.apiKeys.count",
		"groups.apiKeys.summary",
		"groups.apiKeys.maskHint",
		"groups.apiKeys.modeHint",
		"groups.apiKeys.mode.keep",
		"groups.apiKeys.mode.replace",
		"groups.apiKeys.mode.clear",
		"action.addGroup",
		"action.manageGroups",
		"action.refreshModels",
		"action.ping",
		"field.groupID",
		"field.groupName",
		"field.models",
		"field.apiKeys",
		"field.apiKeysMode",
		"field.apiKeysReplace",
		"status.groupsOpened",
		"status.groupSaved",
		"status.groupModelsRefreshed",
		"status.groupPingOK",
		"status.groupPingFail",
		"error.noGroup",
		"error.invalidAPIKeysMode",
		"error.apiKeysReplaceEmpty",
	}
	for _, key := range keys {
		en := translate(langEN, key)
		zh := translate(langZH, key)
		if en == "" || en == key {
			t.Fatalf("missing en for %s", key)
		}
		if zh == "" || zh == key {
			t.Fatalf("missing zh for %s", key)
		}
		if en == zh && !strings.Contains(key, "action.move") {
			// Most keys should differ; allow identical only if intentionally shared.
			// group action symbols may match; locale prose should not.
			if strings.HasPrefix(key, "groups.") || strings.HasPrefix(key, "field.") || strings.HasPrefix(key, "error.") || strings.HasPrefix(key, "status.") {
				t.Fatalf("en/zh identical for %s: %q", key, en)
			}
		}
	}
	// Help strings mention lifecycle delete entry.
	if !strings.Contains(translate(langEN, "groups.help"), "lifecycle") {
		t.Fatalf("en groups.help should mention lifecycle")
	}
	if !strings.Contains(translate(langZH, "groups.help"), "生命周期") {
		t.Fatalf("zh groups.help should mention 生命周期")
	}
}

func TestGroupFormValidationReplaceRequiresKeys(t *testing.T) {
	m := model{lang: langEN}
	m.openGroupForm(actionEditGroup, "p1", &app.ProviderGroupView{
		ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKeyCount: 1, APIKeysMasked: []string{"sk-f…aaaa"},
	})
	for i := range m.formFields {
		switch m.formFields[i].key {
		case fieldKeyAPIKeysMode:
			m.formFields[i].value = apiKeysModeReplace
		case fieldKeyAPIKeys:
			m.formFields[i].value = ""
		}
	}
	if m.validateFormFields() {
		t.Fatal("replace with empty keys should be invalid")
	}
	if !strings.Contains(m.firstFormError(), "replace") && !strings.Contains(m.firstFormError(), m.t("error.apiKeysReplaceEmpty")) {
		// firstFormError should be the replace-empty message
		if got := m.firstFormError(); got != m.t("error.apiKeysReplaceEmpty") {
			t.Fatalf("err = %q", got)
		}
	}

	// keep mode with empty keys is valid.
	for i := range m.formFields {
		if m.formFields[i].key == fieldKeyAPIKeysMode {
			m.formFields[i].value = apiKeysModeKeep
		}
	}
	if !m.validateFormFields() {
		t.Fatalf("keep mode should be valid, err=%q", m.firstFormError())
	}
}
