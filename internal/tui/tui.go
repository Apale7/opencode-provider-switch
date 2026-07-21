package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-runewidth"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

type screen int

const (
	screenOverview screen = iota
	screenProviders
	screenAliases
	screenDoctor
	screenSync
	screenLanguage
	screenHelp
	screenForm
	screenConfirm
)

type action int

const (
	actionNone action = iota
	actionAddProvider
	actionEditProvider
	actionRemoveProvider
	actionAddAlias
	actionEditAlias
	actionRemoveAlias
	actionBindTarget
	actionUnbindTarget
	actionApplySync
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldSelect
	fieldSubmit
)

type field struct {
	key     string
	label   string
	value   string
	kind    fieldKind
	options []string
	mask    bool
	err     string
}

type model struct {
	svc              *app.Service
	ctx              context.Context
	lang             string
	screen           screen
	previous         screen
	menuIndex        int
	providerIndex    int
	aliasIndex       int
	targetIndex      int
	languageIndex    int
	formAction       action
	confirmAction    action
	formFields       []field
	formIndex        int
	selectOpen       bool
	optionIndex      int
	hoverButton      string
	overview         app.Overview
	providers        []app.ProviderView
	aliases          []app.AliasView
	doctorReport     app.DoctorReport
	doctorRan        bool
	syncPreview      app.SyncPreview
	syncPreviewReady bool
	status           string
	err              string
	width            int
	height           int

	// Lifecycle impact preview (provider/alias remove).
	impactActive     bool
	impactLoading    bool
	impactRevision   app.ConfigRevision
	impactOp         lifecycle.Operation
	impactSubject    string
	impactPlan       LifecyclePlanPresentation
	impactRawPlan    app.LifecyclePlanView
	impactSelections map[string]string // choiceID -> optionID
	impactChoiceIdx  int
	impactScroll     int
	impactOutcome    TransportMessage
}

type loadedMsg struct {
	overview  app.Overview
	providers []app.ProviderView
	aliases   []app.AliasView
	prefs     app.DesktopPrefsView
	err       error
}

type refreshedMsg struct {
	overview  app.Overview
	providers []app.ProviderView
	aliases   []app.AliasView
	status    string
	err       error
}

type doctorMsg struct {
	report app.DoctorReport
	err    error
}

type syncPreviewMsg struct {
	preview app.SyncPreview
	err     error
}

type syncApplyMsg struct {
	result app.SyncResult
	err    error
}

type saveLanguageMsg struct {
	lang string
	err  error
}

type lifecyclePreviewMsg struct {
	revision app.ConfigRevision
	plan     app.LifecyclePlanView
	err      error
}

type lifecycleExecuteMsg struct {
	result app.LifecycleExecuteResult
	err    error
}

const defaultViewWidth = 92

var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	ruleStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sectionStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	statusStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("70"))
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	selectedStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	inputStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	buttonStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	buttonActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
)

const (
	fieldKeyProviderID      = "providerID"
	fieldKeyProviderName    = "providerName"
	fieldKeyProtocol        = "protocol"
	fieldKeyBaseURL         = "baseURL"
	fieldKeyBaseURLs        = "baseURLs"
	fieldKeyBaseURLStrategy = "baseURLStrategy"
	fieldKeyAPIKey          = "apiKey"
	fieldKeySkipModels      = "skipModels"
	fieldKeyDisabled        = "disabled"
	fieldKeyAlias           = "alias"
	fieldKeyAliasName       = "aliasName"
	fieldKeyAliasDisplay    = "aliasDisplay"
	fieldKeyModel           = "model"
)

func protocolOptions() []string {
	return []string{config.ProtocolOpenAIResponses, config.ProtocolAnthropicMessages, config.ProtocolOpenAICompatible}
}

func strategyOptions() []string {
	return []string{config.ProviderBaseURLStrategyOrdered, config.ProviderBaseURLStrategyLatency}
}

func yesNoOptions() []string {
	return []string{"yes", "no"}
}

// Run starts the interactive BubbleTea configuration UI.
func Run(configPath string) error {
	svc := app.NewService(configPath)
	m := model{svc: svc, ctx: context.Background(), lang: langEN, screen: screenOverview}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return m.load()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.overview = msg.overview
		m.providers = msg.providers
		m.aliases = msg.aliases
		m.lang = normalizeTUILanguage(msg.prefs.Language)
		m.menuIndex = m.guideMenuIndex()
		m.status = m.t("status.loaded")
		return m, nil
	case refreshedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.overview = msg.overview
		m.providers = msg.providers
		m.aliases = msg.aliases
		m.status = msg.status
		m.err = ""
		m.clampIndexes()
		if m.screen == screenOverview {
			m.menuIndex = m.guideMenuIndex()
		}
		return m, nil
	case doctorMsg:
		m.doctorReport = msg.report
		m.doctorRan = true
		m.status = m.t("status.doctorRan")
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.menuIndex = m.guideMenuIndex()
		return m, nil
	case syncPreviewMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.syncPreview = msg.preview
		m.syncPreviewReady = true
		m.status = m.t("status.syncPreviewed")
		m.err = ""
		m.menuIndex = m.guideMenuIndex()
		return m, nil
	case syncApplyMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.status = m.t("status.syncApplied", msg.result.TargetPath)
		m.err = ""
		m.screen = screenSync
		return m, nil
	case saveLanguageMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.lang = normalizeTUILanguage(msg.lang)
		m.status = m.t("status.languageSaved")
		m.err = ""
		return m, nil
	case lifecyclePreviewMsg:
		m.impactLoading = false
		if msg.err != nil {
			tm := TransportMessageFromError(msg.err, nil)
			m.impactOutcome = tm
			m.err = m.transportMessageText(tm)
			if tm.Kind == "conflict" {
				m.clearImpact()
				m.screen = m.previous
				return m, m.refresh(m.t("impact.revisionConflict"))
			}
			return m, nil
		}
		m.impactRevision = msg.revision
		m.impactRawPlan = msg.plan
		m.impactPlan = PresentLifecyclePlan(msg.plan)
		m.impactOutcome = TransportMessage{}
		m.err = ""
		if !msg.plan.Executable {
			m.status = m.t("impact.notExecutable")
		} else {
			m.status = m.t("impact.ready")
		}
		m.clampImpactIndexes()
		return m, nil
	case lifecycleExecuteMsg:
		m.impactLoading = false
		if msg.err != nil {
			tm := TransportMessageFromError(msg.err, msg.result)
			m.impactOutcome = tm
			m.err = m.transportMessageText(tm)
			if tm.Kind == "conflict" {
				m.clearImpact()
				m.screen = m.previous
				return m, m.refresh(m.t("impact.revisionConflict"))
			}
			if tm.Kind == "apply_failed" {
				summary := PresentLifecycleExecute(msg.result)
				m.status = m.t("impact.applyFailed", summary.RuntimeState)
				m.clearImpact()
				m.screen = m.previous
				return m, m.refresh(m.status)
			}
			if tm.Kind == "blocked" {
				return m, nil
			}
			return m, nil
		}
		summary := PresentLifecycleExecute(msg.result)
		status := m.t("impact.executed", summary.RuntimeState)
		if summary.PendingRestart {
			status = m.t("impact.restartPending")
		}
		m.clearImpact()
		m.screen = m.previous
		m.err = ""
		return m, m.refresh(status)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(msg)
	relY := event.Y - m.contentStartY()
	if relY < 0 {
		return m, nil
	}
	if isMouseMotion(event) {
		m.updateMouseHover(event.X, relY)
		return m, nil
	}
	if !isLeftMousePress(event) {
		return m, nil
	}
	m.updateMouseHover(event.X, relY)
	switch m.screen {
	case screenOverview:
		return m.clickOverview(event.X, relY)
	case screenProviders:
		return m.clickProviders(event.X, relY)
	case screenAliases:
		return m.clickAliases(event.X, relY)
	case screenDoctor:
		return m.clickDoctor(event.X, relY)
	case screenSync:
		return m.clickSync(event.X, relY)
	case screenLanguage:
		return m.clickLanguage(event.X, relY)
	case screenForm:
		return m.clickForm(relY)
	case screenConfirm:
		return m.clickConfirm(event.X, relY)
	}
	return m, nil
}

func isLeftMousePress(event tea.MouseEvent) bool {
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress {
		return true
	}
	return event.Type == tea.MouseLeft && event.Action != tea.MouseActionRelease
}

func isMouseMotion(event tea.MouseEvent) bool {
	return event.Action == tea.MouseActionMotion || event.Type == tea.MouseMotion
}

func (m *model) updateMouseHover(x int, relY int) {
	m.hoverButton = ""
	switch m.screen {
	case screenOverview:
		m.hoverOverview(x, relY)
	case screenProviders:
		m.hoverProviders(x, relY)
	case screenAliases:
		m.hoverAliases(x, relY)
	case screenDoctor:
		m.hoverButtonAt(x, relY, m.doctorButtons())
	case screenSync:
		m.hoverButtonAt(x, relY, m.syncButtons())
	case screenLanguage:
		m.hoverLanguage(x, relY)
	case screenForm:
		m.hoverForm(relY)
	case screenConfirm:
		if relY == confirmButtonY() {
			if m.impactActive {
				m.hoverButton = buttonKeyAt(x, m.impactButtons())
			} else {
				m.hoverButton = buttonKeyAt(x, m.confirmButtons())
			}
		}
	}
}

func (m *model) hoverOverview(x int, relY int) {
	if x < overviewGuideStartX() {
		return
	}
	index := relY - overviewMenuStartY()
	if index >= 0 && index < len(m.menuItems()) && !m.menuDisabled(index) {
		m.menuIndex = index
	}
}

func (m *model) hoverProviders(x int, relY int) {
	if m.hoverButtonAt(x, relY, m.providerButtons()) {
		return
	}
	row := relY - providerRowStartY()
	if row >= 0 && row < len(m.providers) {
		m.providerIndex = row
	}
}

func (m *model) hoverAliases(x int, relY int) {
	if m.hoverButtonAt(x, relY, m.aliasButtons()) {
		return
	}
	aliasRow := relY - aliasRowStartY()
	if aliasRow >= 0 && aliasRow < len(m.aliases) {
		m.aliasIndex = aliasRow
		m.targetIndex = 0
		return
	}
	targetRow := relY - m.targetRowStartY()
	if alias := m.selectedAlias(); alias != nil && targetRow >= 0 && targetRow < len(alias.Targets) {
		m.targetIndex = targetRow
	}
}

func (m *model) hoverLanguage(x int, relY int) {
	items := []string{langEN, langZH}
	row := relY - languageRowStartY()
	if row >= 0 && row < len(items) {
		m.languageIndex = row
		return
	}
	m.hoverButtonAt(x, relY, []tuiButton{{key: "save", label: m.t("form.submit")}})
}

func (m *model) hoverForm(relY int) {
	row := formFieldsStartY()
	for i, field := range m.formFields {
		if relY == row {
			m.formIndex = i
			m.optionIndex = optionIndex(field.options, field.value)
			return
		}
		row++
		if m.selectOpen && i == m.formIndex && field.kind == fieldSelect {
			for option := range field.options {
				if relY == row+option {
					m.optionIndex = option
					return
				}
			}
			row += len(field.options)
		}
	}
}

func (m *model) hoverButtonAt(x int, relY int, buttons []tuiButton) bool {
	if relY != actionButtonY() {
		return false
	}
	m.hoverButton = buttonKeyAt(x, buttons)
	return m.hoverButton != ""
}

func (m model) contentStartY() int {
	start := 3
	if m.status != "" {
		start++
	}
	if m.err != "" {
		start++
	}
	return start
}

func (m model) clickOverview(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if x < overviewGuideStartX() {
		return m, nil
	}
	index := relY - overviewMenuStartY()
	items := m.menuItems()
	if index < 0 || index >= len(items) || m.menuDisabled(index) {
		return m, nil
	}
	m.menuIndex = index
	m.screen = items[index].target
	return m, nil
}

func (m model) clickProviders(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY == actionButtonY() {
		return m.providerButtonClick(buttonKeyAt(x, m.providerButtons()))
	}
	row := relY - providerRowStartY()
	if row >= 0 && row < len(m.providers) {
		m.providerIndex = row
		return m, nil
	}
	return m, nil
}

func (m model) providerButtonClick(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "add":
		m.openProviderForm(actionAddProvider, nil)
	case "edit":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		m.openProviderForm(actionEditProvider, p)
	case "toggle":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		return m, m.toggleProvider(*p)
	case "remove":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		return m.openImpactProviderRemove(p.ID)
	case "refresh":
		return m, m.refresh("")
	}
	return m, nil
}

func (m model) clickAliases(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY == actionButtonY() {
		return m.aliasButtonClick(buttonKeyAt(x, m.aliasButtons()))
	}
	aliasRow := relY - aliasRowStartY()
	if aliasRow >= 0 && aliasRow < len(m.aliases) {
		m.aliasIndex = aliasRow
		m.targetIndex = 0
		return m, nil
	}
	targetRow := relY - m.targetRowStartY()
	if alias := m.selectedAlias(); alias != nil && targetRow >= 0 && targetRow < len(alias.Targets) {
		m.targetIndex = targetRow
		return m, nil
	}
	return m, nil
}

func (m model) aliasButtonClick(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "add":
		m.openAliasForm(actionAddAlias, nil)
	case "edit":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		m.openAliasForm(actionEditAlias, a)
	case "bind":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		m.openBindForm(a)
	case "toggle":
		a, t := m.selectedTarget()
		if a == nil || t == nil {
			m.err = m.t("error.noTarget")
			return m, nil
		}
		return m, m.toggleTarget(*a, *t)
	case "up":
		return m.reorderTarget(-1)
	case "down":
		return m.reorderTarget(1)
	case "unbind":
		a, t := m.selectedTarget()
		if a == nil || t == nil {
			m.err = m.t("error.noTarget")
			return m, nil
		}
		m.openConfirm(actionUnbindTarget, m.t("confirm.unbindTarget", t.Provider, t.Model, a.Alias))
	case "remove":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		return m.openImpactAliasRemove(a.Alias)
	case "refresh":
		return m, m.refresh("")
	}
	return m, nil
}

func (m model) clickDoctor(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY == actionButtonY() && buttonKeyAt(x, m.doctorButtons()) == "run" {
		return m, m.runDoctor()
	}
	return m, nil
}

func (m model) clickSync(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY != actionButtonY() {
		return m, nil
	}
	switch buttonKeyAt(x, m.syncButtons()) {
	case "preview":
		return m, m.previewSync()
	case "apply":
		if !m.syncPreviewReady {
			m.err = m.t("error.needPreview")
			return m, nil
		}
		m.openConfirm(actionApplySync, m.t("confirm.applySync", m.syncPreview.TargetPath))
	}
	return m, nil
}

func (m model) clickLanguage(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	items := []string{langEN, langZH}
	row := relY - languageRowStartY()
	if row >= 0 && row < len(items) {
		m.languageIndex = row
		return m, nil
	}
	if relY == languageButtonY() && buttonKeyAt(x, []tuiButton{{key: "save", label: m.t("form.submit")}}) == "save" {
		lang := langEN
		if m.languageIndex == 1 {
			lang = langZH
		}
		return m, m.saveLanguage(lang)
	}
	return m, nil
}

func (m model) clickForm(relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	row := formFieldsStartY()
	for i, field := range m.formFields {
		if relY == row {
			m.formIndex = i
			m.optionIndex = optionIndex(field.options, field.value)
			if field.kind == fieldSubmit {
				return m.submitForm()
			}
			if field.kind == fieldSelect {
				m.selectOpen = true
			}
			return m, nil
		}
		row++
		if m.selectOpen && i == m.formIndex && field.kind == fieldSelect {
			for option := range field.options {
				if relY == row+option {
					m.optionIndex = option
					m.formFields[i].value = field.options[option]
					m.selectOpen = false
					m.validateFormFields()
					return m, nil
				}
			}
			row += len(field.options)
		}
	}
	return m, nil
}

func (m model) clickConfirm(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY != confirmButtonY() {
		return m, nil
	}
	if m.impactActive {
		switch buttonKeyAt(x, m.impactButtons()) {
		case "yes":
			return m.updateImpact("y")
		case "no":
			return m.updateImpact("n")
		case "refresh":
			return m.updateImpact("r")
		}
		return m, nil
	}
	switch buttonKeyAt(x, m.confirmButtons()) {
	case "yes":
		return m.confirm()
	case "no":
		m.screen = m.previous
		return m, nil
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" || (key == "q" && m.screen != screenForm && m.screen != screenConfirm) {
		return m, tea.Quit
	}
	if m.screen == screenForm {
		return m.updateForm(key)
	}
	if m.screen == screenConfirm {
		return m.updateConfirm(key)
	}
	if key == "?" {
		m.previous = m.screen
		m.screen = screenHelp
		return m, nil
	}
	if key == "esc" {
		m.screen = screenOverview
		m.hoverButton = ""
		m.menuIndex = m.guideMenuIndex()
		return m, nil
	}
	switch m.screen {
	case screenOverview:
		return m.updateOverview(key)
	case screenProviders:
		return m.updateProviders(key)
	case screenAliases:
		return m.updateAliases(key)
	case screenDoctor:
		if key == "r" || key == "enter" {
			return m, m.runDoctor()
		}
	case screenSync:
		return m.updateSync(key)
	case screenLanguage:
		return m.updateLanguage(key)
	case screenHelp:
		if key == "enter" || key == "esc" {
			m.screen = m.previous
			if m.screen == screenHelp {
				m.screen = screenOverview
			}
		}
	}
	return m, nil
}

func (m model) updateOverview(key string) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	switch key {
	case "up", "k":
		m.menuIndex = m.nextMenuIndex(-1)
	case "down", "j":
		m.menuIndex = m.nextMenuIndex(1)
	case "enter":
		if m.menuDisabled(m.menuIndex) {
			return m, nil
		}
		m.screen = items[m.menuIndex].target
	case "r":
		return m, m.refresh("")
	}
	return m, nil
}

func (m model) updateProviders(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.providerIndex = max(0, m.providerIndex-1)
	case "down", "j":
		m.providerIndex = min(len(m.providers)-1, m.providerIndex+1)
	case "r":
		return m, m.refresh("")
	case "a":
		m.openProviderForm(actionAddProvider, nil)
	case "e", "enter":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		m.openProviderForm(actionEditProvider, p)
	case " ":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		return m, m.toggleProvider(*p)
	case "x":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		return m.openImpactProviderRemove(p.ID)
	}
	return m, nil
}

func (m model) updateAliases(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.aliasIndex = max(0, m.aliasIndex-1)
		m.targetIndex = 0
	case "down", "j":
		m.aliasIndex = min(len(m.aliases)-1, m.aliasIndex+1)
		m.targetIndex = 0
	case "tab":
		if a := m.selectedAlias(); a != nil && len(a.Targets) > 0 {
			m.targetIndex = (m.targetIndex + 1) % len(a.Targets)
		}
	case "r":
		return m, m.refresh("")
	case "a":
		m.openAliasForm(actionAddAlias, nil)
	case "e", "enter":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		m.openAliasForm(actionEditAlias, a)
	case "b":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		m.openBindForm(a)
	case " ":
		a, t := m.selectedTarget()
		if a == nil || t == nil {
			m.err = m.t("error.noTarget")
			return m, nil
		}
		return m, m.toggleTarget(*a, *t)
	case "[":
		return m.reorderTarget(-1)
	case "]":
		return m.reorderTarget(1)
	case "u":
		a, t := m.selectedTarget()
		if a == nil || t == nil {
			m.err = m.t("error.noTarget")
			return m, nil
		}
		m.openConfirm(actionUnbindTarget, m.t("confirm.unbindTarget", t.Provider, t.Model, a.Alias))
	case "x":
		a := m.selectedAlias()
		if a == nil {
			m.err = m.t("error.noAlias")
			return m, nil
		}
		return m.openImpactAliasRemove(a.Alias)
	}
	return m, nil
}

func (m model) updateSync(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "p", "enter":
		return m, m.previewSync()
	case "a":
		if !m.syncPreviewReady {
			m.err = m.t("error.needPreview")
			return m, nil
		}
		m.openConfirm(actionApplySync, m.t("confirm.applySync", m.syncPreview.TargetPath))
	}
	return m, nil
}

func (m model) updateLanguage(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.languageIndex = max(0, m.languageIndex-1)
	case "down", "j":
		m.languageIndex = min(1, m.languageIndex+1)
	case "enter":
		lang := langEN
		if m.languageIndex == 1 {
			lang = langZH
		}
		return m, m.saveLanguage(lang)
	}
	return m, nil
}

func (m model) updateForm(key string) (tea.Model, tea.Cmd) {
	if len(m.formFields) == 0 {
		return m, nil
	}
	switch key {
	case "esc":
		if m.selectOpen {
			m.selectOpen = false
			return m, nil
		}
		m.hoverButton = ""
		m.screen = m.previous
		return m, nil
	case "enter":
		return m.activateFormField()
	case "up", "k":
		if m.selectOpen {
			m.moveSelectOption(-1)
			return m, nil
		}
		m.moveFormField(-1)
		return m, nil
	case "down", "j", "tab":
		if m.selectOpen {
			m.moveSelectOption(1)
			return m, nil
		}
		m.moveFormField(1)
		return m, nil
	case "shift+tab":
		m.moveFormField(-1)
		return m, nil
	case "backspace":
		field := &m.formFields[m.formIndex]
		if field.kind != fieldText {
			return m, nil
		}
		v := field.value
		if len(v) > 0 {
			_, size := utf8.DecodeLastRuneInString(v)
			field.value = v[:len(v)-size]
			m.validateFormFields()
		}
		return m, nil
	}
	field := &m.formFields[m.formIndex]
	if field.kind == fieldText && len([]rune(key)) == 1 {
		field.value += key
		m.validateFormFields()
	}
	return m, nil
}

func (m model) activateFormField() (tea.Model, tea.Cmd) {
	field := &m.formFields[m.formIndex]
	switch field.kind {
	case fieldSubmit:
		return m.submitForm()
	case fieldSelect:
		if m.selectOpen {
			if len(field.options) > 0 {
				field.value = field.options[m.optionIndex]
			}
			m.selectOpen = false
			m.validateFormFields()
			return m, nil
		}
		m.selectOpen = true
		m.optionIndex = optionIndex(field.options, field.value)
		return m, nil
	default:
		m.moveFormField(1)
		return m, nil
	}
}

func (m *model) moveFormField(delta int) {
	m.selectOpen = false
	m.formIndex = clamp(m.formIndex+delta, 0, len(m.formFields)-1)
	field := m.formFields[m.formIndex]
	m.optionIndex = optionIndex(field.options, field.value)
}

func (m *model) moveSelectOption(delta int) {
	field := m.formFields[m.formIndex]
	if field.kind != fieldSelect || len(field.options) == 0 {
		return
	}
	m.optionIndex = clamp(m.optionIndex+delta, 0, len(field.options)-1)
}

func (m model) updateConfirm(key string) (tea.Model, tea.Cmd) {
	if m.impactActive {
		return m.updateImpact(key)
	}
	switch key {
	case "y", "Y":
		return m.confirm()
	case "n", "N", "esc":
		m.hoverButton = ""
		m.screen = m.previous
		return m, nil
	}
	return m, nil
}

func (m model) updateImpact(key string) (tea.Model, tea.Cmd) {
	if m.impactLoading && key != "n" && key != "N" && key != "esc" {
		return m, nil
	}
	switch key {
	case "n", "N", "esc":
		m.clearImpact()
		m.hoverButton = ""
		m.screen = m.previous
		return m, nil
	case "r":
		m.impactLoading = true
		m.status = m.t("impact.loading")
		return m, m.loadImpactPreview()
	case "up", "k":
		if m.impactScroll > 0 {
			m.impactScroll--
		} else if m.impactChoiceIdx > 0 {
			m.impactChoiceIdx--
		}
		return m, nil
	case "down", "j":
		if m.impactChoiceIdx+1 < len(m.impactPlan.Choices) {
			m.impactChoiceIdx++
		} else {
			m.impactScroll++
		}
		return m, nil
	case "left", "h":
		if len(m.impactPlan.Choices) == 0 {
			return m, nil
		}
		m.cycleImpactOption(-1)
		m.impactLoading = true
		m.status = m.t("impact.loading")
		return m, m.loadImpactPreview()
	case "right", "l", "tab":
		if len(m.impactPlan.Choices) == 0 {
			return m, nil
		}
		m.cycleImpactOption(1)
		m.impactLoading = true
		m.status = m.t("impact.loading")
		return m, m.loadImpactPreview()
	case "y", "Y", "enter":
		if !m.impactRawPlan.Executable || strings.TrimSpace(m.impactRawPlan.PlanToken) == "" {
			m.status = m.t("impact.notExecutable")
			return m, nil
		}
		m.impactLoading = true
		m.status = m.t("impact.executing")
		return m, m.executeImpact()
	}
	return m, nil
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	if !m.validateFormFields() {
		m.err = m.firstFormError()
		return m, nil
	}
	fields := map[string]string{}
	for _, f := range m.formFields {
		if f.kind != fieldSubmit {
			fields[f.key] = strings.TrimSpace(f.value)
		}
	}
	switch m.formAction {
	case actionAddProvider, actionEditProvider:
		id := fields[fieldKeyProviderID]
		baseURL := fields[fieldKeyBaseURL]
		in := app.ProviderUpsertInput{
			ID:              id,
			Name:            fields[fieldKeyProviderName],
			Protocol:        defaultString(fields[fieldKeyProtocol], config.ProtocolOpenAIResponses),
			BaseURL:         baseURL,
			BaseURLs:        splitList(fields[fieldKeyBaseURLs]),
			BaseURLStrategy: defaultString(fields[fieldKeyBaseURLStrategy], config.ProviderBaseURLStrategyOrdered),
			APIKey:          fields[fieldKeyAPIKey],
			SkipModels:      parseYes(fields[fieldKeySkipModels]),
			Disabled:        parseYes(fields[fieldKeyDisabled]),
		}
		return m, m.saveProvider(in)
	case actionAddAlias, actionEditAlias:
		alias := fields[fieldKeyAliasName]
		in := app.AliasUpsertInput{
			Alias:       alias,
			DisplayName: fields[fieldKeyAliasDisplay],
			Protocol:    defaultString(fields[fieldKeyProtocol], config.ProtocolOpenAIResponses),
			Disabled:    parseYes(fields[fieldKeyDisabled]),
		}
		return m, m.saveAlias(in)
	case actionBindTarget:
		alias := fields[fieldKeyAlias]
		provider := fields[fieldKeyProviderID]
		modelName := fields[fieldKeyModel]
		in := app.AliasTargetInput{Alias: alias, Provider: provider, Model: modelName, Disabled: parseYes(fields[fieldKeyDisabled])}
		return m, m.bindTarget(in)
	}
	return m, nil
}

func (m model) confirm() (tea.Model, tea.Cmd) {
	if m.impactActive {
		return m.updateImpact("y")
	}
	switch m.confirmAction {
	case actionUnbindTarget:
		a, t := m.selectedTarget()
		if a == nil || t == nil {
			m.err = m.t("error.noTarget")
			m.screen = m.previous
			return m, nil
		}
		return m, m.unbindTarget(a.Alias, *t)
	case actionApplySync:
		return m, m.applySync()
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	width := m.viewWidth()
	b.WriteString(titleStyle.Render(m.t("app.title")))
	b.WriteString(" > ")
	b.WriteString(sectionStyle.Render(m.screenTitle()))
	b.WriteString("\n")
	b.WriteString(ruleStyle.Render(strings.Repeat("─", width)))
	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.t("app.status", m.status)))
		b.WriteString("\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render(m.t("app.error", m.err)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	switch m.screen {
	case screenOverview:
		b.WriteString(m.viewOverview())
	case screenProviders:
		b.WriteString(m.viewProviders())
	case screenAliases:
		b.WriteString(m.viewAliases())
	case screenDoctor:
		b.WriteString(m.viewDoctor())
	case screenSync:
		b.WriteString(m.viewSync())
	case screenLanguage:
		b.WriteString(m.viewLanguage())
	case screenHelp:
		b.WriteString(m.t("help.body"))
	case screenForm:
		b.WriteString(m.viewForm())
	case screenConfirm:
		b.WriteString(m.viewConfirm())
	}
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("app.help")))
	return b.String()
}

func (m model) viewWidth() int {
	if m.width > 0 {
		return clamp(m.width-2, 60, 120)
	}
	return defaultViewWidth
}

func (m model) viewOverview() string {
	summary := []string{
		fmt.Sprintf("%s: %s", m.t("overview.config"), emptyLabel(m.overview.ConfigPath)),
		fmt.Sprintf("%s: %d", m.t("overview.providers"), m.overview.ProviderCount),
		fmt.Sprintf("%s: %d", m.t("overview.aliases"), m.overview.AliasCount),
		fmt.Sprintf("%s: %s", m.t("overview.routable"), emptyLabel(strings.Join(m.overview.AvailableAliases, ", "))),
		fmt.Sprintf("%s: %s", m.t("overview.proxy"), proxyLabel(m.overview.Proxy)),
	}
	left := panelStyle.Width(42).Render(sectionStyle.Render(m.t("overview.summaryTitle")) + "\n" + strings.Join(summary, "\n"))
	right := panelStyle.Width(42).Render(sectionStyle.Render(m.t("overview.guideTitle")) + "\n" + m.renderGuideItems())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m model) viewProviders() string {
	var b strings.Builder
	b.WriteString(m.renderButtons(m.providerButtons()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("providers.help")))
	b.WriteString("\n\n")
	if len(m.providers) == 0 {
		return b.String() + m.t("providers.empty")
	}
	t := table.New().Border(lipgloss.NormalBorder()).BorderStyle(ruleStyle).Wrap(false).Headers("", "ID", "State", "Protocol", "Base URL", "API", "Models")
	for i, p := range m.providers {
		state := m.t("provider.state.enabled")
		if p.Disabled {
			state = m.t("provider.state.disabled")
		}
		apiKey := m.t("provider.api.none")
		if p.APIKeySet {
			apiKey = m.t("provider.api.set", p.APIKeyMasked)
		}
		marker := ""
		if i == m.providerIndex {
			marker = "›"
		}
		t.Row(marker, p.ID, state, p.Protocol, p.BaseURL, apiKey, strconv.Itoa(len(p.Models)))
	}
	t.StyleFunc(func(row int, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return sectionStyle
		}
		if row == m.providerIndex {
			return selectedStyle
		}
		return lipgloss.NewStyle()
	})
	b.WriteString(t.Render())
	return b.String()
}

func (m model) viewAliases() string {
	var b strings.Builder
	b.WriteString(m.renderButtons(m.aliasButtons()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("aliases.help")))
	b.WriteString("\n\n")
	if len(m.aliases) == 0 {
		return b.String() + m.t("aliases.empty")
	}
	t := table.New().Border(lipgloss.NormalBorder()).BorderStyle(ruleStyle).Wrap(false).Headers("", "Alias", "State", "Protocol", "Targets", "Available")
	for i, a := range m.aliases {
		state := m.t("alias.state.enabled")
		if !a.Enabled {
			state = m.t("alias.state.disabled")
		}
		marker := ""
		if i == m.aliasIndex {
			marker = "›"
		}
		t.Row(marker, a.Alias, state, a.Protocol, strconv.Itoa(a.TargetCount), strconv.Itoa(a.AvailableTargetCount))
	}
	t.StyleFunc(func(row int, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return sectionStyle
		}
		if row == m.aliasIndex {
			return selectedStyle
		}
		return lipgloss.NewStyle()
	})
	b.WriteString(t.Render())
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render("Targets"))
	b.WriteString("\n")
	b.WriteString(m.renderTargets())
	return b.String()
}

func (m model) viewDoctor() string {
	var b strings.Builder
	b.WriteString(m.renderButtons(m.doctorButtons()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("doctor.help")))
	b.WriteString("\n\n")
	if !m.doctorRan {
		return b.String() + m.t("doctor.notRun")
	}
	if len(m.doctorReport.Issues) == 0 {
		b.WriteString(m.t("doctor.ok"))
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(m.t("doctor.failed", strconv.Itoa(len(m.doctorReport.Issues))))
	b.WriteString("\n")
	for _, issue := range m.doctorReport.Issues {
		b.WriteString(fmt.Sprintf("- [%s/%s] %s\n", issue.Severity, issue.Code, issue.Message))
		if issue.ActionHint != "" {
			b.WriteString("  ")
			b.WriteString(issue.ActionHint)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m model) viewSync() string {
	var b strings.Builder
	b.WriteString(m.renderButtons(m.syncButtons()))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("sync.help")))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render(m.t("sync.capabilityNote")))
	b.WriteString("\n\n")
	if !m.syncPreviewReady {
		return b.String() + m.t("sync.notPreviewed")
	}
	changed := m.t("sync.changed.false")
	if m.syncPreview.WouldChange {
		changed = m.t("sync.changed.true")
	}
	b.WriteString(fmt.Sprintf("%s: %s\n", m.t("sync.target"), m.syncPreview.TargetPath))
	b.WriteString(fmt.Sprintf("%s: %s\n", m.t("sync.change"), changed))
	b.WriteString(fmt.Sprintf("%s:\n", m.t("sync.protocols")))
	for _, p := range m.syncPreview.Protocols {
		b.WriteString(fmt.Sprintf("- %s [%s] %s\n", p.Key, p.Protocol, strings.Join(p.AliasNames, ", ")))
	}
	if len(m.syncPreview.DoctorIssues) > 0 {
		b.WriteString(fmt.Sprintf("%s: %d\n", m.t("sync.issues"), len(m.syncPreview.DoctorIssues)))
	}
	return b.String()
}

func (m model) viewLanguage() string {
	var b strings.Builder
	b.WriteString(m.t("language.current", m.lang))
	b.WriteString("\n")
	b.WriteString(m.t("language.help"))
	b.WriteString("\n\n")
	items := []string{m.t("language.english"), m.t("language.chinese")}
	for i, item := range items {
		b.WriteString(rowLabel(i == m.languageIndex, item))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.renderButtons([]tuiButton{{key: "save", label: m.t("form.submit")}}))
	return b.String()
}

func (m model) viewForm() string {
	var b strings.Builder
	b.WriteString(m.t("form.help"))
	b.WriteString("\n\n")
	row := formFieldsStartY()
	_ = row
	for i, f := range m.formFields {
		label := f.label
		if f.kind == fieldSubmit {
			label = renderButton(tuiButton{key: "submit", label: f.label}, i == m.formIndex)
		} else if i == m.formIndex {
			label = selectedStyle.Render("› " + label)
		} else {
			label = "  " + label
		}
		b.WriteString(label)
		if f.kind == fieldSubmit {
			b.WriteString(" ")
			b.WriteString(mutedStyle.Render(f.value))
			b.WriteString("\n")
			continue
		}
		value := f.value
		if f.mask && value != "" {
			value = strings.Repeat("*", len(value))
		}
		b.WriteString(": ")
		if i == m.formIndex {
			b.WriteString(inputStyle.Render(value))
		} else {
			b.WriteString(value)
		}
		if f.kind == fieldSelect {
			b.WriteString(" ")
			b.WriteString(mutedStyle.Render("▾"))
		}
		if f.err != "" {
			b.WriteString(" ")
			b.WriteString(errorStyle.Render(f.err))
		}
		b.WriteString("\n")
		if m.selectOpen && i == m.formIndex && f.kind == fieldSelect {
			b.WriteString(m.renderSelectOptions(f))
		}
	}
	return b.String()
}

func (m model) viewConfirm() string {
	if m.impactActive {
		return m.viewImpact()
	}
	if len(m.formFields) == 0 {
		return m.t("confirm.help")
	}
	return m.formFields[0].value + "\n\n" + m.renderButtons(m.confirmButtons()) + "\n" + mutedStyle.Render(m.t("confirm.help"))
}

func (m model) viewImpact() string {
	var lines []string
	title := m.t("impact.title", m.impactSubject)
	if m.impactLoading {
		lines = append(lines, title, mutedStyle.Render(m.t("impact.loading")))
	} else {
		lines = append(lines, title)
		lines = append(lines, mutedStyle.Render(m.t("impact.meta",
			m.impactPlan.OperationKind,
			fmt.Sprintf("%v", m.impactPlan.Executable),
			m.impactPlan.BaseRevision,
		)))
		if m.impactOutcome.Code != "" && !m.impactOutcome.OK {
			lines = append(lines, errorStyle.Render(m.transportMessageText(m.impactOutcome)))
		}
		lines = append(lines, m.renderImpactSection(m.t("impact.automatic"), formatImpactChanges(m.impactPlan.Automatic))...)
		lines = append(lines, m.renderImpactSection(m.t("impact.blockers"), formatImpactIssues(m.impactPlan.Blockers))...)
		if len(m.impactPlan.Choices) > 0 {
			lines = append(lines, sectionStyle.Render(m.t("impact.choices")))
			for i, ch := range m.impactPlan.Choices {
				selected := m.impactSelections[ch.ID]
				if selected == "" && len(ch.Options) > 0 {
					selected = ch.Options[0].ID
				}
				marker := "  "
				if i == m.impactChoiceIdx {
					marker = "› "
				}
				opts := make([]string, 0, len(ch.Options))
				for _, opt := range ch.Options {
					label := opt.ID
					if opt.ID == selected {
						label = "[" + opt.ID + "]"
					}
					opts = append(opts, label)
				}
				line := marker + ch.Code + " " + strings.Join(opts, " | ")
				if i == m.impactChoiceIdx {
					line = selectedStyle.Render(line)
				}
				lines = append(lines, line)
			}
		}
		lines = append(lines, m.renderImpactSection(m.t("impact.preserved"), formatImpactIssues(m.impactPlan.Preserved))...)
		if m.impactPlan.RuntimeImpact.ProviderRemoved || m.impactPlan.RuntimeImpact.AliasRemoved || m.impactPlan.RuntimeImpact.RoutingChanged {
			lines = append(lines, mutedStyle.Render(m.t("impact.runtime",
				fmt.Sprintf("%v", m.impactPlan.RuntimeImpact.ProviderRemoved),
				fmt.Sprintf("%v", m.impactPlan.RuntimeImpact.AliasRemoved),
				fmt.Sprintf("%v", m.impactPlan.RuntimeImpact.RoutingChanged),
			)))
		}
	}
	body := m.scrollLines(lines, m.impactScroll)
	help := mutedStyle.Render(m.t("impact.help"))
	buttons := m.renderButtons(m.impactButtons())
	return body + "\n\n" + buttons + "\n" + help
}

func (m model) renderImpactSection(title string, rows []string) []string {
	if len(rows) == 0 {
		return nil
	}
	out := []string{sectionStyle.Render(title)}
	out = append(out, rows...)
	return out
}

func formatImpactChanges(items []LifecycleChangeLine) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, c := range items {
		out = append(out, fmt.Sprintf("  - %s %s (%s)", c.Kind, c.Entity, c.ReasonCode))
	}
	return out
}

func formatImpactIssues(items []LifecycleIssueLine) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, issue := range items {
		path := issue.Path
		if path == "" {
			path = "-"
		}
		out = append(out, fmt.Sprintf("  - %s @ %s", issue.Code, path))
	}
	return out
}

func (m model) scrollLines(lines []string, offset int) string {
	maxBody := m.height - 8
	if maxBody < 8 {
		maxBody = 8
	}
	if len(lines) <= maxBody {
		return strings.Join(lines, "\n")
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines)-maxBody {
		offset = len(lines) - maxBody
	}
	slice := lines[offset : offset+maxBody]
	more := ""
	if offset+maxBody < len(lines) {
		more = "\n" + mutedStyle.Render(m.t("impact.more"))
	}
	return strings.Join(slice, "\n") + more
}

func (m model) impactButtons() []tuiButton {
	buttons := []tuiButton{{key: "no", label: m.t("confirm.no")}}
	if m.impactRawPlan.Executable && strings.TrimSpace(m.impactRawPlan.PlanToken) != "" && !m.impactLoading {
		buttons = append([]tuiButton{{key: "yes", label: m.t("impact.execute")}}, buttons...)
	} else {
		buttons = append([]tuiButton{{key: "refresh", label: m.t("impact.refresh")}}, buttons...)
	}
	return buttons
}

func (m model) transportMessageText(tm TransportMessage) string {
	if tm.Code == "" {
		return m.t("transport.outcome.internal_error")
	}
	key := OutcomeI18nKey(tm.Code)
	text := m.t(key)
	if text == key {
		return tm.Code
	}
	return text
}

func (m *model) clearImpact() {
	m.impactActive = false
	m.impactLoading = false
	m.impactRevision = ""
	m.impactOp = lifecycle.Operation{}
	m.impactSubject = ""
	m.impactPlan = LifecyclePlanPresentation{}
	m.impactRawPlan = app.LifecyclePlanView{}
	m.impactSelections = nil
	m.impactChoiceIdx = 0
	m.impactScroll = 0
	m.impactOutcome = TransportMessage{}
}

func (m *model) clampImpactIndexes() {
	if m.impactChoiceIdx >= len(m.impactPlan.Choices) {
		m.impactChoiceIdx = max(0, len(m.impactPlan.Choices)-1)
	}
	if m.impactSelections == nil {
		m.impactSelections = map[string]string{}
	}
	for _, ch := range m.impactPlan.Choices {
		if _, ok := m.impactSelections[ch.ID]; !ok && len(ch.Options) > 0 {
			m.impactSelections[ch.ID] = ch.Options[0].ID
		}
	}
}

func (m *model) cycleImpactOption(delta int) {
	if len(m.impactPlan.Choices) == 0 {
		return
	}
	m.clampImpactIndexes()
	ch := m.impactPlan.Choices[m.impactChoiceIdx]
	if len(ch.Options) == 0 {
		return
	}
	cur := m.impactSelections[ch.ID]
	idx := 0
	for i, opt := range ch.Options {
		if opt.ID == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(ch.Options)
	if idx < 0 {
		idx += len(ch.Options)
	}
	if m.impactSelections == nil {
		m.impactSelections = map[string]string{}
	}
	m.impactSelections[ch.ID] = ch.Options[idx].ID
}

func (m model) impactSelectionSlice() []lifecycle.Selection {
	if len(m.impactSelections) == 0 {
		return nil
	}
	out := make([]lifecycle.Selection, 0, len(m.impactSelections))
	for id, opt := range m.impactSelections {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(opt) == "" {
			continue
		}
		out = append(out, lifecycle.Selection{ChoiceID: id, OptionID: opt})
	}
	return out
}

func (m model) beginImpact(a action, subject string, op lifecycle.Operation) (model, tea.Cmd) {
	m.previous = m.screen
	m.screen = screenConfirm
	m.confirmAction = a
	m.impactActive = true
	m.impactLoading = true
	m.impactSubject = subject
	m.impactOp = op
	m.impactSelections = map[string]string{}
	m.impactChoiceIdx = 0
	m.impactScroll = 0
	m.impactOutcome = TransportMessage{}
	m.impactPlan = LifecyclePlanPresentation{}
	m.impactRawPlan = app.LifecyclePlanView{}
	m.impactRevision = ""
	m.formFields = []field{{value: subject}}
	m.err = ""
	m.status = m.t("impact.loading")
	m.hoverButton = ""
	return m, m.loadImpactPreview()
}

func (m model) openImpactProviderRemove(id string) (model, tea.Cmd) {
	payload, _ := json.Marshal(lifecycle.ProviderRemovePayload{ProviderID: id})
	op := lifecycle.Operation{Kind: lifecycle.OpProviderRemove, Payload: payload}
	return m.beginImpact(actionRemoveProvider, id, op)
}

func (m model) openImpactAliasRemove(name string) (model, tea.Cmd) {
	payload, _ := json.Marshal(lifecycle.AliasRemovePayload{Alias: name})
	op := lifecycle.Operation{Kind: lifecycle.OpAliasRemove, Payload: payload}
	return m.beginImpact(actionRemoveAlias, name, op)
}

func (m model) previewLifecycleWith(op lifecycle.Operation, selections []lifecycle.Selection) tea.Msg {
	rev, err := m.svc.GetConfigRevision(m.ctx)
	if err != nil {
		return lifecyclePreviewMsg{err: err}
	}
	plan, err := m.svc.PreviewLifecycle(m.ctx, app.LifecyclePreviewInput{
		Revision:   rev,
		Operation:  op,
		Selections: selections,
	})
	return lifecyclePreviewMsg{revision: rev, plan: plan, err: err}
}

func (m model) loadImpactPreview() tea.Cmd {
	op := m.impactOp
	selections := m.impactSelectionSlice()
	return func() tea.Msg {
		return m.previewLifecycleWith(op, selections)
	}
}

func (m model) executeImpact() tea.Cmd {
	rev := m.impactRevision
	token := m.impactRawPlan.PlanToken
	op := m.impactOp
	selections := m.impactSelectionSlice()
	return func() tea.Msg {
		result, err := m.svc.ExecuteLifecycle(m.ctx, app.LifecycleExecuteInput{
			Revision:   rev,
			PlanToken:  token,
			Operation:  op,
			Selections: selections,
		})
		return lifecycleExecuteMsg{result: result, err: err}
	}
}

func (m model) screenTitle() string {
	switch m.screen {
	case screenProviders:
		return m.t("screen.providers")
	case screenAliases:
		return m.t("screen.aliases")
	case screenDoctor:
		return m.t("screen.doctor")
	case screenSync:
		return m.t("screen.sync")
	case screenLanguage:
		return m.t("screen.language")
	case screenHelp:
		return m.t("screen.help")
	default:
		return m.t("screen.overview")
	}
}

func (m model) t(key string, args ...any) string {
	return translate(m.lang, key, args...)
}

type menuItem struct {
	label  string
	target screen
}

func (m model) menuItems() []menuItem {
	return []menuItem{
		{label: m.t("menu.providers"), target: screenProviders},
		{label: m.t("menu.aliases"), target: screenAliases},
		{label: m.t("menu.doctor"), target: screenDoctor},
		{label: m.t("menu.sync"), target: screenSync},
		{label: m.t("menu.language"), target: screenLanguage},
	}
}

func (m model) renderGuideItems() string {
	var b strings.Builder
	steps := m.guideSteps()
	for i, item := range m.menuItems() {
		label := item.label
		if i < len(steps) {
			label = steps[i]
		}
		if m.menuDisabled(i) {
			label = mutedStyle.Render("  " + label)
		} else if i == m.menuIndex {
			label = selectedStyle.Render("› " + label)
		} else {
			label = "  " + label
		}
		b.WriteString(label)
		b.WriteString("\n")
	}
	next := m.nextGuideStep()
	if next == "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.t("overview.done")))
	} else {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.t("overview.next", next)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) guideSteps() []string {
	return []string{
		m.t("overview.step.provider"),
		m.t("overview.step.alias"),
		m.t("overview.step.doctor"),
		m.t("overview.step.sync"),
		m.t("overview.step.language"),
	}
}

func (m model) menuDisabled(index int) bool {
	return m.overview.ProviderCount == 0 && index == 1
}

func (m model) nextMenuIndex(delta int) int {
	items := m.menuItems()
	index := m.menuIndex
	for range items {
		index = clamp(index+delta, 0, len(items)-1)
		if !m.menuDisabled(index) {
			return index
		}
		if index == 0 || index == len(items)-1 {
			break
		}
	}
	return m.menuIndex
}

func (m model) nextGuideStep() string {
	if m.overview.ProviderCount == 0 {
		return m.t("overview.step.provider")
	}
	if m.overview.AliasCount == 0 {
		return m.t("overview.step.alias")
	}
	if !m.doctorRan {
		return m.t("overview.step.doctor")
	}
	if !m.syncPreviewReady {
		return m.t("overview.step.sync")
	}
	return ""
}

func (m model) guideMenuIndex() int {
	if m.overview.ProviderCount == 0 {
		return 0
	}
	if m.overview.AliasCount == 0 {
		return 1
	}
	if !m.doctorRan {
		return 2
	}
	if !m.syncPreviewReady {
		return 3
	}
	return 4
}

func (m model) renderTargets() string {
	a := m.selectedAlias()
	if a == nil {
		return m.t("error.noAlias")
	}
	if len(a.Targets) == 0 {
		return m.t("target.none")
	}
	var b strings.Builder
	for i, target := range a.Targets {
		state := m.t("target.state.enabled")
		if !target.Enabled {
			state = m.t("target.state.disabled")
		}
		text := fmt.Sprintf("%d. [%s] %s/%s", i+1, state, target.Provider, target.Model)
		b.WriteString(rowLabel(i == m.targetIndex, text))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type tuiButton struct {
	key   string
	label string
}

func (m model) providerButtons() []tuiButton {
	label := m.t("action.disable")
	if p := m.selectedProvider(); p != nil && p.Disabled {
		label = m.t("action.enable")
	}
	return []tuiButton{
		{key: "add", label: m.t("action.addProvider")},
		{key: "edit", label: m.t("action.edit")},
		{key: "toggle", label: label},
		{key: "remove", label: m.t("action.remove")},
		{key: "refresh", label: m.t("action.refresh")},
	}
}

func (m model) aliasButtons() []tuiButton {
	toggleLabel := m.t("action.disable")
	_, selected := m.selectedTarget()
	if selected != nil && !selected.Enabled {
		toggleLabel = m.t("action.enable")
	}
	return []tuiButton{
		{key: "add", label: m.t("action.addAlias")},
		{key: "edit", label: m.t("action.edit")},
		{key: "bind", label: m.t("action.bindTarget")},
		{key: "toggle", label: toggleLabel},
		{key: "up", label: m.t("action.moveUp")},
		{key: "down", label: m.t("action.moveDown")},
		{key: "unbind", label: m.t("action.unbind")},
		{key: "remove", label: m.t("action.remove")},
		{key: "refresh", label: m.t("action.refresh")},
	}
}

func (m model) doctorButtons() []tuiButton {
	return []tuiButton{{key: "run", label: m.t("action.runDoctor")}}
}

func (m model) syncButtons() []tuiButton {
	return []tuiButton{
		{key: "preview", label: m.t("action.preview")},
		{key: "apply", label: m.t("action.apply")},
	}
}

func (m model) confirmButtons() []tuiButton {
	return []tuiButton{{key: "yes", label: m.t("confirm.yes")}, {key: "no", label: m.t("confirm.no")}}
}

func (m model) renderButtons(buttons []tuiButton) string {
	parts := make([]string, 0, len(buttons))
	for _, button := range buttons {
		parts = append(parts, renderButton(button, button.key == m.hoverButton))
	}
	return strings.Join(parts, " ")
}

func renderButton(button tuiButton, active bool) string {
	text := "[ " + button.label + " ]"
	if active {
		return buttonActiveStyle.Render(text)
	}
	return buttonStyle.Render(text)
}

func buttonKeyAt(x int, buttons []tuiButton) string {
	pos := 0
	for _, button := range buttons {
		width := runewidth.StringWidth("[ " + button.label + " ]")
		if x >= pos && x < pos+width {
			return button.key
		}
		pos += width + 1
	}
	return ""
}

func rowLabel(selected bool, text string) string {
	if selected {
		return selectedStyle.Render("› " + text)
	}
	return "  " + text
}

func (m model) renderSelectOptions(field field) string {
	var b strings.Builder
	for i, option := range field.options {
		label := "    " + option
		if option == field.value {
			label += " " + mutedStyle.Render("("+m.t("form.optionMarker")+")")
		}
		if i == m.optionIndex {
			label = selectedStyle.Render("  › " + option)
		}
		b.WriteString(label)
		b.WriteString("\n")
	}
	return b.String()
}

func overviewMenuStartY() int { return 2 }

func overviewGuideStartX() int { return 46 }

func actionButtonY() int { return 0 }

func providerRowStartY() int { return 7 }

func aliasRowStartY() int { return 7 }

func languageRowStartY() int { return 3 }

func languageButtonY() int { return 6 }

func formFieldsStartY() int { return 2 }

func confirmButtonY() int { return 2 }

func (m model) targetRowStartY() int {
	return aliasRowStartY() + max(0, len(m.aliases)) + 3
}

func optionIndex(options []string, value string) int {
	for i, option := range options {
		if option == value {
			return i
		}
	}
	return 0
}

func containsOption(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

func emptyLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (m *model) openProviderForm(a action, p *app.ProviderView) {
	m.previous = screenProviders
	m.screen = screenForm
	m.formAction = a
	m.formIndex = 0
	m.selectOpen = false
	m.optionIndex = 0
	id, name, protocol, baseURL, baseURLs, strategy, disabled := "", "", config.ProtocolOpenAIResponses, "", "", config.ProviderBaseURLStrategyOrdered, "no"
	if p != nil {
		id, name, protocol, baseURL = p.ID, p.Name, p.Protocol, p.BaseURL
		baseURLs = strings.Join(p.BaseURLs, ",")
		strategy = p.BaseURLStrategy
		if p.Disabled {
			disabled = "yes"
		}
	}
	m.formFields = []field{
		{key: fieldKeyProviderID, label: m.t("field.providerID"), value: id, kind: fieldText},
		{key: fieldKeyProviderName, label: m.t("field.providerName"), value: name, kind: fieldText},
		{key: fieldKeyProtocol, label: m.t("field.protocol"), value: protocol, kind: fieldSelect, options: protocolOptions()},
		{key: fieldKeyBaseURL, label: m.t("field.baseURL"), value: baseURL, kind: fieldText},
		{key: fieldKeyBaseURLs, label: m.t("field.baseURLs"), value: baseURLs, kind: fieldText},
		{key: fieldKeyBaseURLStrategy, label: m.t("field.baseURLStrategy"), value: strategy, kind: fieldSelect, options: strategyOptions()},
		{key: fieldKeyAPIKey, label: m.t("field.apiKey"), kind: fieldText, mask: true},
		{key: fieldKeySkipModels, label: m.t("field.skipModels"), value: "yes", kind: fieldSelect, options: yesNoOptions()},
		{key: fieldKeyDisabled, label: m.t("field.disabled"), value: disabled, kind: fieldSelect, options: yesNoOptions()},
		{key: "submit", label: m.t("form.submit"), value: m.t("form.submitHint"), kind: fieldSubmit},
	}
	m.validateFormFields()
}

func (m *model) openAliasForm(a action, alias *app.AliasView) {
	m.previous = screenAliases
	m.screen = screenForm
	m.formAction = a
	m.formIndex = 0
	m.selectOpen = false
	m.optionIndex = 0
	name, display, protocol, disabled := "", "", config.ProtocolOpenAIResponses, "no"
	if alias != nil {
		name, display, protocol = alias.Alias, alias.DisplayName, alias.Protocol
		if !alias.Enabled {
			disabled = "yes"
		}
	}
	m.formFields = []field{
		{key: fieldKeyAliasName, label: m.t("field.aliasName"), value: name, kind: fieldText},
		{key: fieldKeyAliasDisplay, label: m.t("field.aliasDisplay"), value: display, kind: fieldText},
		{key: fieldKeyProtocol, label: m.t("field.protocol"), value: protocol, kind: fieldSelect, options: protocolOptions()},
		{key: fieldKeyDisabled, label: m.t("field.disabled"), value: disabled, kind: fieldSelect, options: yesNoOptions()},
		{key: "submit", label: m.t("form.submit"), value: m.t("form.submitHint"), kind: fieldSubmit},
	}
	m.validateFormFields()
}

func (m *model) openBindForm(alias *app.AliasView) {
	m.previous = screenAliases
	m.screen = screenForm
	m.formAction = actionBindTarget
	m.formIndex = 0
	m.selectOpen = false
	m.optionIndex = 0
	provider := ""
	if len(m.providers) > 0 {
		provider = m.providers[min(m.providerIndex, len(m.providers)-1)].ID
	}
	m.formFields = []field{
		{key: fieldKeyAlias, label: m.t("field.alias"), value: alias.Alias, kind: fieldText},
		{key: fieldKeyProviderID, label: m.t("field.providerID"), value: provider, kind: fieldSelect, options: m.providerOptions()},
		{key: fieldKeyModel, label: m.t("field.model"), kind: fieldText},
		{key: fieldKeyDisabled, label: m.t("field.disabled"), value: "no", kind: fieldSelect, options: yesNoOptions()},
		{key: "submit", label: m.t("form.submit"), value: m.t("form.submitHint"), kind: fieldSubmit},
	}
	m.validateFormFields()
}

func (m *model) openConfirm(a action, text string) {
	m.previous = m.screen
	m.screen = screenConfirm
	m.confirmAction = a
	m.selectOpen = false
	m.formFields = []field{{value: text}}
}

func (m model) providerOptions() []string {
	items := make([]string, 0, len(m.providers))
	for _, provider := range m.providers {
		items = append(items, provider.ID)
	}
	return items
}

func (m *model) validateFormFields() bool {
	valid := true
	values := map[string]string{}
	for _, field := range m.formFields {
		values[field.key] = strings.TrimSpace(field.value)
	}
	for i := range m.formFields {
		field := &m.formFields[i]
		field.err = m.validateField(*field, values)
		if field.err != "" {
			valid = false
		}
	}
	return valid
}

func (m model) validateField(field field, values map[string]string) string {
	if field.kind == fieldSubmit {
		return ""
	}
	value := strings.TrimSpace(field.value)
	if field.kind == fieldSelect && !containsOption(field.options, value) {
		return m.selectFieldError(field)
	}
	switch m.formAction {
	case actionAddProvider, actionEditProvider:
		return m.validateProviderField(field, values)
	case actionAddAlias, actionEditAlias:
		return m.validateAliasField(field)
	case actionBindTarget:
		return m.validateBindField(field)
	default:
		return ""
	}
}

func (m model) validateProviderField(field field, values map[string]string) string {
	value := strings.TrimSpace(field.value)
	switch field.key {
	case fieldKeyProviderID:
		if value == "" {
			return m.t("error.required", field.label)
		}
	case fieldKeyProtocol:
		if err := config.ValidateProtocol(value); err != nil {
			return m.t("error.invalidProtocol")
		}
	case fieldKeyBaseURL:
		if value == "" {
			return m.t("error.required", field.label)
		}
		protocol := defaultString(values[fieldKeyProtocol], config.ProtocolOpenAIResponses)
		if err := config.ValidateProviderBaseURL(protocol, value); err != nil {
			return m.t("error.invalidBaseURL", field.label)
		}
	case fieldKeyBaseURLs:
		protocol := defaultString(values[fieldKeyProtocol], config.ProtocolOpenAIResponses)
		for _, item := range splitList(value) {
			if err := config.ValidateProviderBaseURL(protocol, item); err != nil {
				return m.t("error.invalidBaseURL", field.label)
			}
		}
	case fieldKeyBaseURLStrategy:
		if !containsOption(strategyOptions(), value) {
			return m.t("error.invalidStrategy")
		}
	case fieldKeySkipModels, fieldKeyDisabled:
		if !containsOption(yesNoOptions(), value) {
			return m.t("error.invalidBoolean")
		}
	}
	return ""
}

func (m model) validateAliasField(field field) string {
	value := strings.TrimSpace(field.value)
	switch field.key {
	case fieldKeyAliasName:
		if value == "" {
			return m.t("error.required", field.label)
		}
	case fieldKeyProtocol:
		if err := config.ValidateProtocol(value); err != nil {
			return m.t("error.invalidProtocol")
		}
	case fieldKeyDisabled:
		if !containsOption(yesNoOptions(), value) {
			return m.t("error.invalidBoolean")
		}
	}
	return ""
}

func (m model) validateBindField(field field) string {
	value := strings.TrimSpace(field.value)
	switch field.key {
	case fieldKeyAlias, fieldKeyProviderID, fieldKeyModel:
		if value == "" {
			return m.t("error.required", field.label)
		}
	case fieldKeyDisabled:
		if !containsOption(yesNoOptions(), value) {
			return m.t("error.invalidBoolean")
		}
	}
	return ""
}

func (m model) selectFieldError(field field) string {
	switch field.key {
	case fieldKeyProtocol:
		return m.t("error.invalidProtocol")
	case fieldKeyBaseURLStrategy:
		return m.t("error.invalidStrategy")
	case fieldKeySkipModels, fieldKeyDisabled:
		return m.t("error.invalidBoolean")
	default:
		return m.t("error.required", field.label)
	}
}

func (m model) firstFormError() string {
	for _, field := range m.formFields {
		if field.err != "" {
			return field.err
		}
	}
	return ""
}

func (m model) load() tea.Cmd {
	return func() tea.Msg {
		overview, err := m.svc.GetOverview(m.ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		providers, err := m.svc.ListProviders(m.ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		aliases, err := m.svc.ListAliases(m.ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		prefs, err := m.svc.GetDesktopPrefs(m.ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		return loadedMsg{overview: overview, providers: providers, aliases: aliases, prefs: prefs}
	}
}

func (m model) refresh(status string) tea.Cmd {
	return func() tea.Msg {
		overview, err := m.svc.GetOverview(m.ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		providers, err := m.svc.ListProviders(m.ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		aliases, err := m.svc.ListAliases(m.ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		return refreshedMsg{overview: overview, providers: providers, aliases: aliases, status: status}
	}
}

func (m model) saveProvider(in app.ProviderUpsertInput) tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.UpsertProvider(m.ctx, in)
		if err != nil {
			return refreshedMsg{err: err}
		}
		status := m.t("status.providerSaved", result.Provider.ID)
		if len(result.Warnings) > 0 {
			status += ": " + strings.Join(result.Warnings, "; ")
		}
		return runRefresh(m, status)
	}
}

func (m model) toggleProvider(p app.ProviderView) tea.Cmd {
	return func() tea.Msg {
		_, err := m.svc.SetProviderDisabled(m.ctx, app.ProviderStateInput{ID: p.ID, Disabled: !p.Disabled})
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.providerToggled", p.ID))
	}
}

func (m model) removeProvider(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.svc.RemoveProvider(m.ctx, id); err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.providerRemoved", id))
	}
}

func (m model) saveAlias(in app.AliasUpsertInput) tea.Cmd {
	return func() tea.Msg {
		alias, err := m.svc.UpsertAlias(m.ctx, in)
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.aliasSaved", alias.Alias))
	}
}

func (m model) removeAlias(name string) tea.Cmd {
	return func() tea.Msg {
		if err := m.svc.RemoveAlias(m.ctx, name); err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.aliasRemoved", name))
	}
}

func (m model) bindTarget(in app.AliasTargetInput) tea.Cmd {
	return func() tea.Msg {
		_, err := m.svc.BindAliasTarget(m.ctx, in)
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.targetBound", in.Provider, in.Model))
	}
}

func (m model) toggleTarget(alias app.AliasView, target app.AliasTargetView) tea.Cmd {
	return func() tea.Msg {
		_, err := m.svc.SetAliasTargetDisabled(m.ctx, app.AliasTargetInput{Alias: alias.Alias, Provider: target.Provider, Model: target.Model, Disabled: target.Enabled})
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.targetToggled", target.Provider, target.Model))
	}
}

func (m model) reorderTarget(delta int) (tea.Model, tea.Cmd) {
	a := m.selectedAlias()
	if a == nil || len(a.Targets) == 0 {
		m.err = m.t("error.noTarget")
		return m, nil
	}
	nextIndex := m.targetIndex + delta
	if nextIndex < 0 || nextIndex >= len(a.Targets) {
		return m, nil
	}
	refs := make([]app.AliasTargetRefInput, 0, len(a.Targets))
	for _, target := range a.Targets {
		refs = append(refs, app.AliasTargetRefInput{Provider: target.Provider, Model: target.Model})
	}
	refs[m.targetIndex], refs[nextIndex] = refs[nextIndex], refs[m.targetIndex]
	m.targetIndex = nextIndex
	return m, func() tea.Msg {
		_, err := m.svc.ReorderAliasTargets(m.ctx, app.AliasTargetReorderInput{Alias: a.Alias, Targets: refs})
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.targetReordered", a.Alias))
	}
}

func (m model) unbindTarget(alias string, target app.AliasTargetView) tea.Cmd {
	return func() tea.Msg {
		_, err := m.svc.UnbindAliasTarget(m.ctx, app.AliasTargetInput{Alias: alias, Provider: target.Provider, Model: target.Model})
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.targetUnbound", target.Provider, target.Model))
	}
}

func (m model) runDoctor() tea.Cmd {
	return func() tea.Msg {
		report, err := m.svc.RunDoctor(m.ctx)
		return doctorMsg{report: report, err: err}
	}
}

func (m model) previewSync() tea.Cmd {
	return func() tea.Msg {
		preview, err := m.svc.PreviewOpenCodeSync(m.ctx, app.SyncInput{DryRun: true})
		return syncPreviewMsg{preview: preview, err: err}
	}
}

func (m model) applySync() tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.ApplyOpenCodeSync(m.ctx, app.SyncInput{})
		return syncApplyMsg{result: result, err: err}
	}
}

func (m model) saveLanguage(lang string) tea.Cmd {
	return func() tea.Msg {
		prefs, err := m.svc.GetDesktopPrefs(m.ctx)
		if err != nil {
			return saveLanguageMsg{err: err}
		}
		_, err = m.svc.SaveDesktopPrefs(m.ctx, app.DesktopPrefsInput{
			LaunchAtLogin:  prefs.LaunchAtLogin,
			AutoStartProxy: prefs.AutoStartProxy,
			MinimizeToTray: prefs.MinimizeToTray,
			Notifications:  prefs.Notifications,
			Theme:          prefs.Theme,
			Language:       lang,
		})
		return saveLanguageMsg{lang: lang, err: err}
	}
}

func runRefresh(m model, status string) refreshedMsg {
	overview, err := m.svc.GetOverview(m.ctx)
	if err != nil {
		return refreshedMsg{err: err}
	}
	providers, err := m.svc.ListProviders(m.ctx)
	if err != nil {
		return refreshedMsg{err: err}
	}
	aliases, err := m.svc.ListAliases(m.ctx)
	if err != nil {
		return refreshedMsg{err: err}
	}
	return refreshedMsg{overview: overview, providers: providers, aliases: aliases, status: status}
}

func (m model) selectedProvider() *app.ProviderView {
	if len(m.providers) == 0 || m.providerIndex < 0 || m.providerIndex >= len(m.providers) {
		return nil
	}
	return &m.providers[m.providerIndex]
}

func (m model) selectedAlias() *app.AliasView {
	if len(m.aliases) == 0 || m.aliasIndex < 0 || m.aliasIndex >= len(m.aliases) {
		return nil
	}
	return &m.aliases[m.aliasIndex]
}

func (m model) selectedTarget() (*app.AliasView, *app.AliasTargetView) {
	a := m.selectedAlias()
	if a == nil || len(a.Targets) == 0 || m.targetIndex < 0 || m.targetIndex >= len(a.Targets) {
		return a, nil
	}
	return a, &a.Targets[m.targetIndex]
}

func (m *model) clampIndexes() {
	m.providerIndex = clamp(m.providerIndex, 0, len(m.providers)-1)
	m.aliasIndex = clamp(m.aliasIndex, 0, len(m.aliases)-1)
	if alias := m.selectedAlias(); alias != nil {
		m.targetIndex = clamp(m.targetIndex, 0, len(alias.Targets)-1)
	} else {
		m.targetIndex = 0
	}
	if m.screen == screenForm || m.screen == screenConfirm {
		m.screen = m.previous
	}
}

func selector(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func proxyLabel(status app.ProxyStatusView) string {
	state := "stopped"
	if status.Running {
		state = "running"
	}
	if status.BindAddress == "" {
		return state
	}
	return state + " " + status.BindAddress
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func parseYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(value, low, high int) int {
	if high < low {
		return low
	}
	return min(max(value, low), high)
}
