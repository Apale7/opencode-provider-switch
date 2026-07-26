package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

// Upstream API key edit modes for group forms (storage values; not locale keys).
const (
	apiKeysModeKeep    = "keep"
	apiKeysModeReplace = "replace"
	apiKeysModeClear   = "clear"
)

func apiKeysModeOptions() []string {
	return []string{apiKeysModeKeep, apiKeysModeReplace, apiKeysModeClear}
}

// apiKeysModeLabel returns a localized display label; internal value stays keep/replace/clear.
func apiKeysModeLabel(lang, mode string) string {
	switch strings.TrimSpace(mode) {
	case apiKeysModeKeep, apiKeysModeReplace, apiKeysModeClear:
		return translate(lang, "groups.apiKeys.mode."+mode)
	default:
		return mode
	}
}

// parseGroupAPIKeysInput splits multi-key input by commas and newlines.
// Empty tokens are dropped; order is preserved.
func parseGroupAPIKeysInput(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, ",", "\n")
	parts := strings.Split(value, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// resolveGroupAPIKeysThreeState maps TUI three-state editor fields to ProviderGroupInput key fields.
// keep  → APIKeysChanged=false, keys empty (server preserves)
// replace → APIKeysChanged=true, keys from parseGroupAPIKeysInput
// clear → APIKeysChanged=true, keys empty (server clears)
func resolveGroupAPIKeysThreeState(mode, raw string) (changed bool, keys []string) {
	switch strings.TrimSpace(mode) {
	case apiKeysModeClear:
		return true, nil
	case apiKeysModeReplace:
		return true, parseGroupAPIKeysInput(raw)
	default:
		return false, nil
	}
}

// formatGroupAPIKeysSummary builds a non-secret display of masked keys / count.
// Never includes plaintext secrets.
func formatGroupAPIKeysSummary(lang string, g app.ProviderGroupView) string {
	if g.APIKeyCount <= 0 {
		return translate(lang, "groups.apiKeys.none")
	}
	if len(g.APIKeysMasked) > 0 {
		return translate(lang, "groups.apiKeys.summary", g.APIKeyCount, strings.Join(g.APIKeysMasked, ", "))
	}
	return translate(lang, "groups.apiKeys.count", g.APIKeyCount)
}

func groupRowStartY() int { return 8 }

func (m *model) openGroupsForSelectedProvider() {
	p := m.selectedProvider()
	if p == nil {
		m.err = m.t("error.noProvider")
		return
	}
	m.previous = screenProviders
	m.screen = screenGroups
	m.groupIndex = 0
	m.hoverButton = ""
	m.err = ""
	m.status = m.t("status.groupsOpened", p.ID)
}

func (m model) selectedGroup() (*app.ProviderView, *app.ProviderGroupView) {
	p := m.selectedProvider()
	if p == nil || len(p.Groups) == 0 || m.groupIndex < 0 || m.groupIndex >= len(p.Groups) {
		return p, nil
	}
	return p, &p.Groups[m.groupIndex]
}

func (m model) updateGroups(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.groupIndex = max(0, m.groupIndex-1)
	case "down", "j":
		if p := m.selectedProvider(); p != nil && len(p.Groups) > 0 {
			m.groupIndex = min(len(p.Groups)-1, m.groupIndex+1)
		}
	case "r":
		return m, m.refresh("")
	case "a":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		m.openGroupForm(actionAddGroup, p.ID, nil)
	case "e", "enter":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		m.openGroupForm(actionEditGroup, p.ID, g)
	case "x":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m.openImpactGroupRemove(p.ID, g.ID)
	case "m":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m, m.refreshGroupModels(p.ID, g.ID)
	case "p":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m, m.pingGroup(p.ID, g.ID)
	}
	return m, nil
}

func (m model) groupButtons() []tuiButton {
	return []tuiButton{
		{key: "add", label: m.t("action.addGroup")},
		{key: "edit", label: m.t("action.edit")},
		{key: "remove", label: m.t("action.remove")},
		{key: "refreshModels", label: m.t("action.refreshModels")},
		{key: "ping", label: m.t("action.ping")},
		{key: "refresh", label: m.t("action.refresh")},
	}
}

func (m *model) hoverGroups(x int, relY int) {
	if m.hoverButtonAt(x, relY, m.groupButtons()) {
		return
	}
	row := relY - groupRowStartY()
	if p := m.selectedProvider(); p != nil && row >= 0 && row < len(p.Groups) {
		m.groupIndex = row
	}
}

func (m model) clickGroups(x int, relY int) (tea.Model, tea.Cmd) {
	m.hoverButton = ""
	if relY == actionButtonY() {
		return m.groupButtonClick(buttonKeyAt(x, m.groupButtons()))
	}
	row := relY - groupRowStartY()
	if p := m.selectedProvider(); p != nil && row >= 0 && row < len(p.Groups) {
		m.groupIndex = row
		return m, nil
	}
	return m, nil
}

func (m model) groupButtonClick(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "add":
		p := m.selectedProvider()
		if p == nil {
			m.err = m.t("error.noProvider")
			return m, nil
		}
		m.openGroupForm(actionAddGroup, p.ID, nil)
	case "edit":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		m.openGroupForm(actionEditGroup, p.ID, g)
	case "remove":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m.openImpactGroupRemove(p.ID, g.ID)
	case "refreshModels":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m, m.refreshGroupModels(p.ID, g.ID)
	case "ping":
		p, g := m.selectedGroup()
		if p == nil || g == nil {
			m.err = m.t("error.noGroup")
			return m, nil
		}
		return m, m.pingGroup(p.ID, g.ID)
	case "refresh":
		return m, m.refresh("")
	}
	return m, nil
}

func (m model) viewGroups() string {
	var b strings.Builder
	p := m.selectedProvider()
	if p == nil {
		return m.t("error.noProvider")
	}
	b.WriteString(m.renderButtons(m.groupButtons()))
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render(m.t("groups.provider", p.ID)))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(m.t("groups.help")))
	b.WriteString("\n\n")
	if len(p.Groups) == 0 {
		return b.String() + m.t("groups.empty")
	}
	t := table.New().Border(lipgloss.NormalBorder()).BorderStyle(ruleStyle).Wrap(false).
		Headers("", "ID", m.t("field.groupName"), m.t("field.protocol"), m.t("field.models"), m.t("groups.apiKeys.label"), m.t("field.disabled"))
	for i, g := range p.Groups {
		state := m.t("provider.state.enabled")
		if g.Disabled {
			state = m.t("provider.state.disabled")
		}
		marker := ""
		if i == m.groupIndex {
			marker = "›"
		}
		name := g.Name
		if name == "" {
			name = "-"
		}
		t.Row(marker, g.ID, name, g.Protocol, strconvItoa(len(g.Models)), formatGroupAPIKeysSummary(m.lang, g), state)
	}
	t.StyleFunc(func(row int, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return sectionStyle
		}
		if row == m.groupIndex {
			return selectedStyle
		}
		return lipgloss.NewStyle()
	})
	b.WriteString(t.Render())
	if _, g := m.selectedGroup(); g != nil {
		b.WriteString("\n\n")
		b.WriteString(sectionStyle.Render(m.t("groups.detail")))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.t("groups.apiKeys.maskHint")))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s: %s\n", m.t("groups.apiKeys.label"), formatGroupAPIKeysSummary(m.lang, *g)))
		if len(g.Models) > 0 {
			b.WriteString(fmt.Sprintf("%s: %s\n", m.t("field.models"), strings.Join(g.Models, ", ")))
		}
		if g.ModelsSource != "" {
			b.WriteString(fmt.Sprintf("%s: %s\n", m.t("groups.modelsSource"), g.ModelsSource))
		}
	}
	return b.String()
}

func strconvItoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func (m *model) openGroupForm(a action, providerID string, g *app.ProviderGroupView) {
	m.previous = screenGroups
	m.screen = screenForm
	m.formAction = a
	m.formIndex = 0
	m.selectOpen = false
	m.optionIndex = 0
	m.formProviderID = providerID
	m.formGroupID = ""
	id, name, protocol, models, disabled := "", "", config.ProtocolOpenAIResponses, "", "no"
	apiKeysMode := apiKeysModeKeep
	if a == actionAddGroup {
		apiKeysMode = apiKeysModeReplace
	}
	if g != nil {
		m.formGroupID = g.ID
		id, name, protocol = g.ID, g.Name, g.Protocol
		models = strings.Join(g.Models, ",")
		if g.Disabled {
			disabled = "yes"
		}
	}
	fields := []field{
		{key: fieldKeyGroupID, label: m.t("field.groupID"), value: id, kind: fieldText},
		{key: fieldKeyGroupName, label: m.t("field.groupName"), value: name, kind: fieldText},
		{key: fieldKeyProtocol, label: m.t("field.protocol"), value: protocol, kind: fieldSelect, options: protocolOptions()},
		{key: fieldKeyModels, label: m.t("field.models"), value: models, kind: fieldText},
		{key: fieldKeyDisabled, label: m.t("field.disabled"), value: disabled, kind: fieldSelect, options: yesNoOptions()},
	}
	if a == actionEditGroup {
		summary := ""
		if g != nil {
			summary = formatGroupAPIKeysSummary(m.lang, *g)
		}
		// Current keys appear only as masked summary on the mode label — never as an editable secret.
		fields = append(fields,
			field{key: fieldKeyAPIKeysMode, label: m.t("field.apiKeysMode") + " [" + summary + "]", value: apiKeysMode, kind: fieldSelect, options: apiKeysModeOptions()},
			field{key: fieldKeyAPIKeys, label: m.t("field.apiKeysReplace"), value: "", kind: fieldText, mask: true},
		)
	} else {
		fields = append(fields,
			field{key: fieldKeyAPIKeys, label: m.t("field.apiKeys"), value: "", kind: fieldText, mask: true},
		)
	}
	fields = append(fields, field{key: "submit", label: m.t("form.submit"), value: m.t("form.submitHint"), kind: fieldSubmit})
	m.formFields = fields
	m.validateFormFields()
}

// buildProviderGroupInputFromFields converts form field map to ProviderGroupInput.
// edit=true applies three-state key semantics; edit=false always sets keys from input.
func buildProviderGroupInputFromFields(fields map[string]string, edit bool) app.ProviderGroupInput {
	in := app.ProviderGroupInput{
		ID:          strings.TrimSpace(fields[fieldKeyGroupID]),
		Name:        strings.TrimSpace(fields[fieldKeyGroupName]),
		NameChanged: edit,
		Protocol:    defaultString(fields[fieldKeyProtocol], config.ProtocolOpenAIResponses),
		Models:      splitList(fields[fieldKeyModels]),
		Disabled:    parseYes(fields[fieldKeyDisabled]),
	}
	if edit {
		changed, keys := resolveGroupAPIKeysThreeState(fields[fieldKeyAPIKeysMode], fields[fieldKeyAPIKeys])
		in.APIKeysChanged = changed
		in.APIKeys = keys
		return in
	}
	in.APIKeysChanged = true
	in.APIKeys = parseGroupAPIKeysInput(fields[fieldKeyAPIKeys])
	return in
}

func (m model) validateGroupField(field field, values map[string]string) string {
	value := strings.TrimSpace(field.value)
	switch field.key {
	case fieldKeyGroupID:
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
	case fieldKeyAPIKeysMode:
		if m.formAction == actionEditGroup && !containsOption(apiKeysModeOptions(), value) {
			return m.t("error.invalidAPIKeysMode")
		}
	case fieldKeyAPIKeys:
		if m.formAction == actionEditGroup {
			mode := strings.TrimSpace(values[fieldKeyAPIKeysMode])
			if mode == apiKeysModeReplace && strings.TrimSpace(value) == "" {
				return m.t("error.apiKeysReplaceEmpty")
			}
			if mode != apiKeysModeReplace && strings.TrimSpace(value) != "" {
				// Allow typing before switching mode; no hard error.
			}
		}
	}
	return ""
}

func (m model) openImpactGroupRemove(providerID, groupID string) (model, tea.Cmd) {
	payload, _ := json.Marshal(lifecycle.GroupRemovePayload{ProviderID: providerID, GroupID: groupID})
	op := lifecycle.Operation{Kind: lifecycle.OpGroupRemove, Payload: payload}
	return m.beginImpact(actionRemoveGroup, providerID+"/"+groupID, op)
}

func (m model) openImpactGroupIDChange(providerID, oldGroupID, newGroupID string) (model, tea.Cmd) {
	payload, _ := json.Marshal(lifecycle.GroupIDChangePayload{
		ProviderID: providerID,
		OldGroupID: oldGroupID,
		NewGroupID: newGroupID,
	})
	op := lifecycle.Operation{Kind: lifecycle.OpGroupIDChange, Payload: payload}
	return m.beginImpact(actionEditGroup, providerID+"/"+oldGroupID+"→"+newGroupID, op)
}

func (m model) saveGroup(providerID string, groupID string, in app.ProviderGroupInput, create bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		var view app.ProviderGroupView
		if create {
			view, err = m.svc.CreateProviderGroup(m.ctx, app.ProviderGroupCreateInput{
				ProviderID: providerID,
				Group:      in,
			})
		} else {
			view, err = m.svc.UpdateProviderGroup(m.ctx, app.ProviderGroupUpdateInput{
				ProviderID: providerID,
				GroupID:    groupID,
				Group:      in,
			})
		}
		if err != nil {
			return refreshedMsg{err: err}
		}
		return runRefresh(m, m.t("status.groupSaved", providerID, view.ID))
	}
}

func (m model) refreshGroupModels(providerID, groupID string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.RefreshProviderGroupModels(m.ctx, app.ProviderGroupRefreshModelsInput{
			ProviderID: providerID,
			GroupID:    groupID,
		})
		if err != nil {
			return refreshedMsg{err: err}
		}
		status := m.t("status.groupModelsRefreshed", providerID, groupID)
		if len(result.Warnings) > 0 {
			status += ": " + strings.Join(result.Warnings, "; ")
		}
		return runRefresh(m, status)
	}
}

func (m model) pingGroup(providerID, groupID string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.PingProviderGroupBaseURL(m.ctx, app.ProviderGroupPingInput{
			ProviderID: providerID,
			GroupID:    groupID,
		})
		if err != nil {
			return refreshedMsg{err: err}
		}
		status := m.t("status.groupPingOK", providerID, groupID, result.LatencyMs)
		if !result.Reachable {
			errText := strings.TrimSpace(result.Error)
			if errText == "" {
				errText = fmt.Sprintf("status=%d", result.StatusCode)
			}
			status = m.t("status.groupPingFail", providerID, groupID, errText)
		}
		return runRefresh(m, status)
	}
}
