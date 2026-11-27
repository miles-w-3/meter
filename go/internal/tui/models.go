package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/protocol"
)

type modelItem struct {
	model     protocol.ModelInfo
	available bool
}

func (i modelItem) FilterValue() string {
	return i.model.Name + " " + i.model.Provider + " " + i.model.ModelId
}

type levelItem string

func (i levelItem) FilterValue() string { return string(i) }

type modelDelegate struct{ colors terminalTheme }

func (d modelDelegate) Height() int                         { return 1 }
func (d modelDelegate) Spacing() int                        { return 0 }
func (d modelDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d modelDelegate) Render(w io.Writer, l list.Model, index int, item list.Item) {
	prefix := "  "
	style := d.colors.muted
	if index == l.Index() {
		prefix, style = "› ", d.colors.moss
	}
	label := ""
	switch i := item.(type) {
	case modelItem:
		label = i.model.Name + " · " + i.model.Provider
		if !i.available {
			label += " · unavailable"
		}
	case levelItem:
		label = string(i)
	}
	_, _ = io.WriteString(w, ansi.Truncate(prefix+style.Render(label), l.Width(), "…"))
}

type modelsMsg struct {
	session   *session
	inventory protocol.ModelInventory
	err       error
}
type modelChangedMsg struct {
	session      *session
	conversation protocol.Conversation
	err          error
}

func (m *Model) selection() protocol.Selection {
	if m.state.Current.Selection != nil {
		return *m.state.Current.Selection
	}
	if m.conversation.Selection != nil {
		return *m.conversation.Selection
	}
	return protocol.Selection{ThinkingLevel: "off"}
}
func (m *Model) openModels() tea.Cmd {
	if m.api == nil || !m.connected || m.running() {
		m.notice = "Model selection is available when connected and idle"
		return nil
	}
	m.overlay = overlay{kind: overlayModel, filter: textinput.New(), expected: m.selection()}
	m.overlay.filter.Prompt = "Search  "
	m.overlay.filter.Placeholder = "Filter models…"
	m.overlay.filter.CharLimit = 128
	m.overlay.filter.SetStyles(textinput.Styles{})
	m.overlay.list = list.New(nil, modelDelegate{m.colors}, 20, 5)
	m.overlay.list.SetShowTitle(false)
	m.overlay.list.SetShowFilter(false)
	m.overlay.list.SetShowHelp(false)
	m.overlay.list.SetShowStatusBar(false)
	m.overlay.list.SetShowPagination(false)
	m.overlay.list.DisableQuitKeybindings()
	m.resizeOverlay()
	s, api, ctx := m.session, m.api, m.ctx
	return tea.Batch(m.overlay.filter.Focus(), func() tea.Msg { inventory, err := api.Models(ctx); return modelsMsg{s, inventory, err} })
}
func (m *Model) receiveModels(msg modelsMsg) tea.Cmd {
	if m.session != msg.session || m.overlay.kind != overlayModel {
		return nil
	}
	if msg.err != nil {
		m.overlay.modelError = clean(msg.err.Error())
		return nil
	}
	status := map[string]bool{}
	for _, p := range msg.inventory.Providers {
		status[p.Id] = string(p.Status) == "configured"
	}
	items := make([]list.Item, 0, len(msg.inventory.Models))
	for _, v := range msg.inventory.Models {
		items = append(items, modelItem{v, status[v.Provider]})
	}
	m.overlay.modelLoaded = true
	return m.overlay.list.SetItems(items)
}
func (m *Model) modelKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "down", "pgup", "pgdown":
		var cmd tea.Cmd
		m.overlay.list, cmd = m.overlay.list.Update(msg)
		return cmd
	case "enter":
		switch item := m.overlay.list.SelectedItem().(type) {
		case modelItem:
			if !item.available {
				m.overlay.modelError = "Provider is not configured"
				return nil
			}
			m.overlay.modelError = ""
			m.overlay.chosen = item.model
			m.overlay.levels = true
			m.overlay.filter.SetValue("")
			m.overlay.list.ResetFilter()
			levels := make([]list.Item, 0, len(item.model.ThinkingLevels))
			for _, level := range item.model.ThinkingLevels {
				levels = append(levels, levelItem(level))
			}
			if len(levels) == 0 {
				levels = append(levels, levelItem("off"))
			}
			m.overlay.list.SetItems(levels)
			m.overlay.list.Select(0)
			return nil
		case levelItem:
			if m.overlay.modelPending {
				return nil
			}
			m.overlay.modelPending = true
			// Commands run off the Update goroutine: capture values, not the
			// session, whose conversation Update replaces.
			chosen, expected, s, id, api, ctx := m.overlay.chosen, m.overlay.expected, m.session, m.conversation.Id, m.api, m.ctx
			input := protocol.SelectionChange{Model: protocol.ModelRef{Provider: chosen.Provider, ModelId: chosen.ModelId}, ThinkingLevel: protocol.ThinkingLevel(item), Expected: expected}
			return func() tea.Msg {
				conversation, err := api.SetConversationModel(ctx, id, input)
				return modelChangedMsg{s, conversation, err}
			}
		}
	}
	return nil
}
func (m *Model) receiveModelChanged(msg modelChangedMsg) {
	if msg.session != m.session {
		return
	}
	if msg.err != nil {
		if m.overlay.kind == overlayModel {
			m.overlay.modelPending = false
			m.overlay.modelError = clean(msg.err.Error())
		} else {
			m.notice = clean(msg.err.Error())
		}
		return
	}
	m.conversation = msg.conversation
	if msg.conversation.Selection != nil {
		m.state.Current.Selection = msg.conversation.Selection
	}
	m.notice = ""
	if m.overlay.kind == overlayModel {
		m.overlay = overlay{}
	}
}
func (m *Model) modelLabel() string {
	selection := m.selection()
	if selection.Model != nil {
		return fmt.Sprintf("%s · %s", clean(selection.Model.ModelId), clean(string(selection.ThinkingLevel)))
	}
	if name := strings.TrimSpace(m.conversation.Model); name != "" {
		return clean(name)
	}
	return "Choose model"
}
