package tui

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/theme"
)

type overlayKind uint8

const (
	overlayNone overlayKind = iota
	overlaySwitch
	overlayModel
	overlaySearch
	overlayPager
	overlayHelp
)

type overlay struct {
	kind                              overlayKind
	list                              list.Model
	filter                            textinput.Model
	tool                              Block
	pagerText                         string
	pagerTop                          int
	pagerBusy                         bool
	pagerError                        string
	pagerFind                         bool
	pagerQuery                        string
	pagerMatch                        int
	helpTop                           int
	expected                          protocol.Selection
	chosen                            protocol.ModelInfo
	levels, modelLoaded, modelPending bool
	modelError                        string
}

type conversationItem struct {
	conversation            protocol.Conversation
	project, projectID, tab string
}

func (i conversationItem) title() string {
	if title := value(i.conversation.Title); title != "" {
		return clean(title)
	}
	return "Conversation #" + i.conversation.Id
}

func (i conversationItem) crumb() string {
	if i.tab == "" {
		return i.project
	}
	return i.project + " / " + i.tab
}

func (i conversationItem) FilterValue() string {
	return i.title() + " " + i.crumb()
}

func (m *Model) itemFor(conversation protocol.Conversation) conversationItem {
	if item, found := m.directory[conversation.Id]; found {
		return item
	}
	return conversationItem{conversation: conversation, project: clean(filepath.Base(conversation.Cwd))}
}

type catalogMsg struct {
	epoch   int
	catalog protocol.Catalog
	layout  protocol.Layout
	err     error
}

func (m *Model) fetchCatalog() tea.Cmd {
	if m.api == nil {
		return nil
	}
	m.catalogEpoch++
	epoch := m.catalogEpoch
	return func() tea.Msg {
		catalog, err := m.api.Catalog(m.ctx)
		if err != nil {
			return catalogMsg{epoch: epoch, err: err}
		}
		layout, _ := m.api.Layout(m.ctx, m.options.Workspace)
		return catalogMsg{epoch, catalog, layout, nil}
	}
}

func (m *Model) receiveCatalog(msg catalogMsg) tea.Cmd {
	if msg.epoch != m.catalogEpoch {
		return nil
	}
	if msg.err != nil {
		m.notice = clean(msg.err.Error())
		return nil
	}
	projects := make(map[string]string, len(msg.catalog.Projects))
	for _, p := range msg.catalog.Projects {
		projects[p.Id] = clean(p.Name)
	}
	members := make(map[string]conversationItem)
	// Prefer the active tab when the same conversation is in multiple tabs.
	for _, tab := range msg.layout.Tabs {
		for _, node := range tab.Nodes {
			id := value(node.ConversationId)
			if id == "" {
				continue
			}
			if _, exists := members[id]; exists && value(msg.layout.ActiveTab) != tab.Id {
				continue
			}
			tabName := clean(value(tab.Name))
			if tabName == "" {
				tabName = "Main"
			}
			project := ""
			if tab.ProjectId != nil {
				project = tab.ProjectId.String()
			}
			members[id] = conversationItem{projectID: project, tab: tabName}
		}
	}
	m.directory = make(map[string]conversationItem, len(msg.catalog.Conversations))
	for _, conv := range msg.catalog.Conversations {
		item := members[conv.Id]
		item.conversation = conv
		if item.projectID == "" && conv.ProjectId != nil {
			item.projectID = conv.ProjectId.String()
		}
		item.project = projects[item.projectID]
		if item.project == "" {
			item.project = clean(filepath.Base(conv.Cwd))
		}
		m.directory[conv.Id] = item
	}
	if m.overlay.kind == overlaySwitch {
		cmd := m.overlay.list.SetItems(m.switchItems())
		m.filterSwitcher()
		return cmd
	}
	return nil
}

func (m *Model) switchItems() []list.Item {
	conversations := make([]conversationItem, 0, len(m.directory)+1)
	for _, item := range m.directory {
		conversations = append(conversations, item)
	}
	if _, found := m.directory[m.conversation.Id]; !found {
		conversations = append(conversations, m.itemFor(m.conversation))
	}
	slices.SortFunc(conversations, func(a, b conversationItem) int {
		if order := cmp.Compare(value(b.conversation.LastActivity), value(a.conversation.LastActivity)); order != 0 {
			return order
		}
		return cmp.Compare(a.conversation.Id, b.conversation.Id)
	})
	items := make([]list.Item, len(conversations))
	for i, item := range conversations {
		items[i] = item
	}
	return items
}

type conversationDelegate struct{ colors terminalTheme }

func (d conversationDelegate) Height() int                         { return 1 }
func (d conversationDelegate) Spacing() int                        { return 0 }
func (d conversationDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d conversationDelegate) Render(out io.Writer, model list.Model, index int, item list.Item) {
	i := item.(conversationItem)
	prefix, style := "  ", d.colors.fog
	if index == model.Index() {
		prefix, style = "› ", d.colors.moss
	}
	h := fnv.New32a()
	_, _ = io.WriteString(h, i.projectID+i.project)
	hues := [...]string{theme.Moss, theme.Rain, theme.Chanterelle, theme.Madrone}
	meta := i.crumb()
	if age := activityAge(i.conversation.LastActivity); age != "" {
		meta += " · " + age
	}
	line := prefix + d.colors.fg(hues[h.Sum32()%uint32(len(hues))]).Render("●") + " " + style.Render(i.title()) + " · " + d.colors.muted.Render(meta)
	_, _ = io.WriteString(out, ansi.Truncate(line, model.Width(), "…"))
}

func (m *Model) openSwitcher() tea.Cmd {
	m.overlay = overlay{kind: overlaySwitch, filter: textinput.New()}
	m.overlay.filter.Prompt = "Search  "
	m.overlay.filter.Placeholder = "Filter conversations…"
	m.overlay.filter.CharLimit = 128
	m.overlay.filter.SetStyles(textinput.Styles{})
	m.overlay.list = list.New(m.switchItems(), conversationDelegate{m.colors}, 20, 5)
	m.overlay.list.SetShowTitle(false)
	m.overlay.list.SetShowFilter(false)
	m.overlay.list.SetShowHelp(false)
	m.overlay.list.SetShowStatusBar(false)
	m.overlay.list.SetShowPagination(false)
	m.overlay.list.DisableQuitKeybindings()
	m.styleSwitcher()
	m.resizeOverlay()
	return tea.Batch(m.overlay.filter.Focus(), m.fetchCatalog())
}

func (m *Model) styleSwitcher() {
	if m.overlay.kind == overlaySwitch {
		m.overlay.list.SetDelegate(conversationDelegate{m.colors})
	}
}

func (m *Model) filterSwitcher() {
	if query := m.overlay.filter.Value(); query != "" {
		m.overlay.list.SetFilterText(query)
	} else {
		m.overlay.list.ResetFilter()
	}
}

func (m *Model) resizeOverlay() {
	w, h := m.overlaySize(m.frame())
	if m.overlay.kind == overlaySwitch || m.overlay.kind == overlayModel {
		m.overlay.list.SetSize(w-2, max(1, h-4))
		m.overlay.filter.SetWidth(max(1, w-10))
	}
	if m.overlay.kind == overlaySearch {
		m.search.SetWidth(max(1, w-5))
	}
	if m.overlay.kind == overlayPager && m.overlay.pagerFind {
		m.search.SetWidth(max(1, w-5))
	}
}

func (m *Model) overlaySize(f frame) (int, int) {
	w, h := min(m.width, max(4, min(76, f.width))), min(f.rows, 14)
	if m.overlay.kind == overlaySearch {
		return w, min(h, 4)
	}
	if m.overlay.kind == overlayPager || m.overlay.kind == overlayHelp {
		return w, min(f.rows, 26)
	}
	return w, h
}

func (m *Model) drawOverlay(lines []string, f frame) {
	w, h := m.overlaySize(f)
	if h < 3 || w < 4 {
		return
	}
	c := m.colors
	content := []string{}
	switch m.overlay.kind {
	case overlaySwitch:
		content = append(content, c.moss.Render("Switch conversation"), m.overlay.filter.View())
		if len(m.overlay.list.VisibleItems()) == 0 {
			content = append(content, c.muted.Render("No matching conversations"))
		} else {
			content = append(content, strings.Split(m.overlay.list.View(), "\n")...)
		}
	case overlayModel:
		title := "Choose model"
		if m.overlay.levels {
			title = "Choose thinking level · " + clean(m.overlay.chosen.Name)
		}
		content = append(content, c.moss.Render(title))
		if !m.overlay.levels {
			content = append(content, m.overlay.filter.View())
		}
		if m.overlay.modelError != "" {
			content = append(content, c.warning.Render(m.overlay.modelError))
		}
		if !m.overlay.modelLoaded {
			content = append(content, c.muted.Render("Loading models…"))
		} else {
			content = append(content, strings.Split(m.overlay.list.View(), "\n")...)
		}
	case overlaySearch:
		content = append(content, c.moss.Render("Find in transcript"), m.search.View())
	case overlayHelp:
		content = m.helpContent(w-2, h-2)
	case overlayPager:
		title := m.overlay.tool.tool
		if m.overlay.tool.target != "" {
			title += " · " + m.overlay.tool.target
		}
		content = append(content, c.rain.Bold(!c.plain).Render(title))
		body := m.pagerLines()
		maxTop := max(0, len(body)-max(1, h-4))
		top := min(m.overlay.pagerTop, maxTop)
		for _, line := range body[top:min(len(body), top+max(1, h-4))] {
			content = append(content, c.tool.Render(line))
		}
		for len(content) < h-3 {
			content = append(content, "")
		}
		footer := c.faint.Render(fmt.Sprintf("%d/%d · / find · y copy · esc close", top+1, len(body)))
		if m.overlay.pagerFind {
			footer = m.search.View()
		}
		content = append(content, footer)
	}
	x, y := f.left+(f.width-w)/2, frameTop+(f.rows-h)/2
	paint := func(line, text string) string {
		before := ansi.Cut(line, 0, x)
		before += strings.Repeat(" ", max(0, x-ansi.StringWidth(before)))
		return before + paintBackground(text, c.composerBackground) + ansi.Cut(line, x+w, m.width)
	}
	lines[y] = paint(lines[y], c.faint.Render("╭"+strings.Repeat("─", w-2)+"╮"))
	for i := 0; i < h-2; i++ {
		body := ""
		if i < len(content) {
			body = ansi.Truncate(content[i], w-2, "")
		}
		lines[y+i+1] = paint(lines[y+i+1], c.faint.Render("│")+body+strings.Repeat(" ", max(0, w-2-ansi.StringWidth(body)))+c.faint.Render("│"))
	}
	lines[y+h-1] = paint(lines[y+h-1], c.faint.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

var helpRows = [][2]string{
	{"Compose", ""},
	{"Enter", "Send or queue a follow-up"},
	{"Alt+Enter / Ctrl+S", "Steer a running conversation"},
	{"Shift+Enter / Ctrl+J", "Insert a newline"},
	{"Esc", "Focus transcript, then confirm stop while running"},
	{"Ctrl+C / Ctrl+Q", "Stop then quit / detach"},
	{"Ctrl+R", "Retry or restore a rejected submission"},
	{"Transcript", ""},
	{"j/k / arrows", "Move one line"},
	{"PgUp/PgDn / Ctrl+U/D", "Move half a page"},
	{"g/G", "Top / bottom and resume following"},
	{"[ / ]", "Previous / next user turn"},
	{"Space / Ctrl+O", "Expand tool / open output pager"},
	{"/ / n/N", "Search / next or previous match"},
	{"y", "Copy a turn, code block or tool output"},
	{"Mouse wheel", "Scroll three lines"},
	{"Shift / Option + drag", "Select text, or start with --no-mouse"},
	{"Ctrl+K", "Switch conversations; Tab there for projects"},
	{"Ctrl+L", "Choose model and thinking level"},
	{"?", "Open or close help"},
}

func (m *Model) helpContent(width, height int) []string {
	c := m.colors
	lines := []string{c.moss.Render("Keyboard shortcuts")}
	for _, row := range helpRows[m.overlay.helpTop:min(len(helpRows), m.overlay.helpTop+max(0, height-1))] {
		if row[1] == "" {
			lines = append(lines, c.moss.Render(row[0]))
		} else if width >= 40 {
			label := c.muted.Render(row[0])
			lines = append(lines, label+strings.Repeat(" ", max(1, 23-ansi.StringWidth(label)))+c.faint.Render(row[1]))
		} else {
			lines = append(lines, c.muted.Render(row[0])+" "+c.faint.Render(row[1]))
		}
	}
	return lines
}

func (m *Model) scrollHelp(delta int) {
	_, height := m.overlaySize(m.frame())
	m.overlay.helpTop = min(max(0, m.overlay.helpTop+delta), max(0, len(helpRows)-max(1, height-3)))
}

type toolOutputMsg struct {
	session     *session
	entry, text string
	err         error
}

func (m *Model) openPager() tea.Cmd {
	b, ok := m.transcript.at(m.transcript.cursor)
	if !ok || b.block.kind != toolBlock {
		return nil
	}
	m.overlay = overlay{kind: overlayPager, tool: b.block, pagerText: b.block.text}
	if !b.block.clipped || b.block.entry == "" || m.api == nil {
		return nil
	}
	m.overlay.pagerBusy = true
	s, conversation, id := m.session, m.conversation.Id, b.block.entry
	return func() tea.Msg {
		text, err := m.api.ToolOutput(m.ctx, conversation, id)
		return toolOutputMsg{s, id, text, err}
	}
}

func (m *Model) receiveOutput(msg toolOutputMsg) {
	if msg.session != m.session || m.overlay.kind != overlayPager || msg.entry != m.overlay.tool.entry {
		return
	}
	m.overlay.pagerBusy = false
	if msg.err != nil {
		m.overlay.pagerError = clean(msg.err.Error())
	} else {
		m.overlay.pagerText = clean(msg.text)
	}
}

func (m *Model) pagerLines() []string {
	w, _ := m.overlaySize(m.frame())
	body := wrap(m.overlay.pagerText, w-2)
	if m.overlay.pagerBusy {
		body = append([]string{"Loading full output…"}, body...)
	} else if m.overlay.pagerError != "" {
		body = append(wrap(m.overlay.pagerError, w-2), body...)
	}
	return body
}

func (m *Model) scrollPager(delta int) {
	_, h := m.overlaySize(m.frame())
	m.overlay.pagerTop = min(max(0, m.overlay.pagerTop+delta), max(0, len(m.pagerLines())-max(1, h-4)))
}

func (m *Model) findInPager(direction int) {
	query := strings.ToLower(m.overlay.pagerQuery)
	if query == "" {
		return
	}
	lines := m.pagerLines()
	for i := 1; i <= len(lines); i++ {
		n := (m.overlay.pagerMatch + direction*i + 2*len(lines)) % len(lines)
		if n >= 0 && strings.Contains(strings.ToLower(lines[n]), query) {
			m.overlay.pagerMatch = n
			m.scrollPager(n - m.overlay.pagerTop)
			return
		}
	}
	m.notice = "No match for " + m.overlay.pagerQuery
}

func (m *Model) updateOverlay(message tea.Msg) tea.Cmd {
	if m.overlay.kind == overlayPager && m.overlay.pagerFind {
		if msg, ok := message.(tea.KeyPressMsg); ok {
			switch msg.String() {
			case "esc":
				m.overlay.pagerFind = false
				return nil
			case "enter":
				m.overlay.pagerFind = false
				m.overlay.pagerQuery = strings.TrimSpace(m.search.Value())
				m.overlay.pagerMatch = m.overlay.pagerTop - 1
				m.findInPager(1)
				return nil
			}
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(message)
		return cmd
	}
	if msg, ok := message.(tea.KeyPressMsg); ok {
		if msg.String() == "esc" {
			m.overlay = overlay{}
			return nil
		}
		switch m.overlay.kind {
		case overlaySearch:
			if msg.String() == "enter" {
				m.query = strings.TrimSpace(m.search.Value())
				m.overlay = overlay{}
				m.searchPending, m.searchBackward = true, false
				return m.finishSearch()
			}
		case overlaySwitch:
			switch msg.String() {
			case "tab":
				project := m.conversation.ProjectId
				return func() tea.Msg { return leaveMsg{project: project} }
			case "enter":
				if item, ok := m.overlay.list.SelectedItem().(conversationItem); ok {
					m.overlay = overlay{}
					if item.conversation.Id != m.conversation.Id {
						m.open(item.conversation)
						return m.listen()
					}
				}
				return nil
			case "up", "down", "pgup", "pgdown", "ctrl+p", "ctrl+n":
				var cmd tea.Cmd
				m.overlay.list, cmd = m.overlay.list.Update(msg)
				return cmd
			}
		case overlayModel:
			if msg.String() == "enter" || msg.String() == "up" || msg.String() == "down" || msg.String() == "pgup" || msg.String() == "pgdown" {
				return m.modelKey(msg)
			}
		case overlayHelp:
			_, h := m.overlaySize(m.frame())
			switch msg.String() {
			case "j", "down":
				m.scrollHelp(1)
			case "k", "up":
				m.scrollHelp(-1)
			case "pgup", "ctrl+u":
				m.scrollHelp(-max(1, h/2))
			case "pgdown", "ctrl+d":
				m.scrollHelp(max(1, h/2))
			case "g":
				m.overlay.helpTop = 0
			case "G":
				m.scrollHelp(len(helpRows))
			}
			return nil
		case overlayPager:
			_, h := m.overlaySize(m.frame())
			page := max(1, (h-4)/2)
			switch msg.String() {
			case "j", "down":
				m.scrollPager(1)
			case "k", "up":
				m.scrollPager(-1)
			case "pgup", "ctrl+u":
				m.scrollPager(-page)
			case "pgdown", "ctrl+d":
				m.scrollPager(page)
			case "g":
				m.overlay.pagerTop = 0
			case "G":
				m.scrollPager(len(m.pagerLines()))
			case "y":
				return tea.SetClipboard(m.overlay.pagerText)
			case "/":
				m.overlay.pagerFind = true
				m.search.SetValue(m.overlay.pagerQuery)
				m.search.CursorEnd()
				m.resizeOverlay()
				return m.search.Focus()
			case "n":
				m.findInPager(1)
			case "N":
				m.findInPager(-1)
			}
			return nil
		}
	}
	switch m.overlay.kind {
	case overlaySearch:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(message)
		return cmd
	case overlayModel, overlaySwitch:
		if m.overlay.kind == overlayModel && m.overlay.levels {
			return nil
		}
		before := m.overlay.filter.Value()
		var cmd tea.Cmd
		m.overlay.filter, cmd = m.overlay.filter.Update(message)
		if m.overlay.filter.Value() != before {
			m.filterSwitcher()
		}
		return cmd
	}
	return nil
}
