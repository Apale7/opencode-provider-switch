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
	p := app.ProviderView{ID: "p1", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://one.example/v1", BaseURLs: []string{"https://one.example/v1", "https://two.example/v1"}, BaseURLStrategy: config.ProviderBaseURLStrategyLatency}
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

func TestProviderFormValidatesBaseURLImmediately(t *testing.T) {
	m := model{lang: langEN}
	m.openProviderForm(actionAddProvider, nil)
	m.formFields[0].value = "p1"
	m.formFields[3].value = "https://api.example"

	if m.validateFormFields() {
		t.Fatalf("form valid with base URL missing /v1")
	}
	if got := m.formFields[3].err; got == "" {
		t.Fatalf("base URL field error empty")
	}
}

func TestMouseClickSelectsProviderRow(t *testing.T) {
	m := model{
		lang:          langEN,
		screen:        screenProviders,
		providerIndex: 0,
		providers: []app.ProviderView{
			{ID: "p1", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://one.example/v1"},
			{ID: "p2", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://two.example/v1"},
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
			{ID: "p1", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://one.example/v1"},
			{ID: "p2", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://two.example/v1"},
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
			{ID: "p1", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://one.example/v1"},
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
			{ID: "p1", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://one.example/v1"},
			{ID: "p2", Protocol: config.ProtocolOpenAIResponses, BaseURL: "https://two.example/v1"},
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
