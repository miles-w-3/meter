package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

type Model struct {
	ctx          context.Context
	api          *client.Client
	conversation protocol.Conversation
	events       <-chan client.Event
	state        client.State
	width        int
	height       int
	offset       int
	draft        []rune
	cursor       int
	submission   client.Submission
	loading      bool
	connected    bool
	mayAbort     bool
	mayRetry     bool
	notice       string
}

func New(ctx context.Context, api *client.Client, conversation protocol.Conversation, info protocol.ServerInfo) *Model {
	m := &Model{ctx: ctx, api: api, conversation: conversation, width: 80, height: 24}
	for _, capability := range info.Capabilities {
		switch capability {
		case "abort-target":
			m.mayAbort = true
		case "retry-submit":
			m.mayRetry = true
		}
	}
	m.events = api.Subscribe(ctx, conversation.Id)
	return m
}

type streamMsg client.Event
type sentMsg struct{ err error }
type actionMsg struct{ err error }
type historyMsg struct {
	cursor *string
	page   protocol.HistoryPage
	err    error
}

func (m *Model) Init() tea.Cmd { return m.listen() }

func (m *Model) listen() tea.Cmd {
	return func() tea.Msg {
		event, open := <-m.events
		if !open {
			return nil
		}
		return streamMsg(event)
	}
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case streamMsg:
		m.connected = msg.View != nil
		m.state.Apply(client.Event(msg))
		return m, m.listen()
	case sentMsg:
		m.submission.Resolve(msg.err)
		if msg.err == nil {
			m.notice = "Input accepted"
			break
		}
		m.notice = msg.err.Error()
		if m.submission.Uncertain() {
			m.notice = "Submission outcome unknown"
			if m.mayRetry {
				m.notice += " · Ctrl+R retries the same request"
			}
		} else {
			if len(m.draft) == 0 {
				m.draft = []rune(m.submission.Restore())
				m.cursor = len(m.draft)
			} else {
				m.notice += " · Esc discards the rejected input"
			}
		}
	case actionMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = "Abort requested"
		}
	case historyMsg:
		m.loading = false
		if msg.err != nil {
			m.notice = msg.err.Error()
			break
		}
		m.state.Prepend(msg.cursor, msg.page)
	case tea.PasteMsg:
		m.insert(clean(msg.Content))
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		return tea.Quit
	case "enter":
		return m.submit("prompt")
	case "ctrl+f":
		return m.submit("followup")
	case "ctrl+s":
		return m.submit("steer")
	case "ctrl+r":
		if m.submission.Uncertain() && m.mayRetry && m.connected {
			return m.send()
		}
	case "esc":
		if m.submission.Restore() != "" {
			m.notice = ""
		}
	case "ctrl+x":
		if m.connected && m.mayAbort && m.state.Current.AbortTarget != nil {
			target := *m.state.Current.AbortTarget
			return func() tea.Msg { return actionMsg{m.api.Abort(m.ctx, m.conversation.Id, target)} }
		}
	case "pgup":
		m.offset = min(m.offset+max(1, m.height-8), max(0, len(m.transcript())-1))
		if m.offset >= max(0, len(m.transcript())-max(1, m.height-8)) {
			return m.loadHistory()
		}
	case "pgdown":
		m.offset = max(0, m.offset-max(1, m.height-8))
	case "left":
		m.cursor = max(0, m.cursor-1)
	case "right":
		m.cursor = min(len(m.draft), m.cursor+1)
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = len(m.draft)
	case "backspace":
		if m.cursor > 0 {
			m.draft = append(m.draft[:m.cursor-1], m.draft[m.cursor:]...)
			m.cursor--
		}
	case "delete":
		if m.cursor < len(m.draft) {
			m.draft = append(m.draft[:m.cursor], m.draft[m.cursor+1:]...)
		}
	default:
		if text := msg.Key().Text; text != "" {
			m.insert(clean(text))
		}
	}
	return nil
}

func (m *Model) insert(text string) {
	runes := []rune(text)
	if len(runes)+len(m.draft) > 16_000 {
		m.notice = "Draft limit reached"
		return
	}
	tail := append([]rune{}, m.draft[m.cursor:]...)
	m.draft = append(append(m.draft[:m.cursor], runes...), tail...)
	m.cursor += len(runes)
}

func (m *Model) submit(mode protocol.SubmitInputMode) tea.Cmd {
	if !m.connected || !m.submission.Begin(mode, string(m.draft)) {
		return nil
	}
	m.draft, m.cursor, m.offset = nil, 0, 0
	return m.send()
}

func (m *Model) send() tea.Cmd {
	input, ok := m.submission.Take()
	if !ok {
		return nil
	}
	m.notice = "Submitting…"
	return func() tea.Msg {
		_, err := m.api.Submit(m.ctx, m.conversation.Id, input)
		return sentMsg{err}
	}
}

func (m *Model) loadHistory() tea.Cmd {
	if m.loading || m.state.Before == nil {
		return nil
	}
	m.loading = true
	cursor := m.state.Before
	return func() tea.Msg {
		page, err := m.api.History(m.ctx, m.conversation.Id, cursor)
		return historyMsg{cursor, page, err}
	}
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func (m *Model) transcript() []string {
	var text strings.Builder
	for _, entry := range m.state.Entries() {
		if entry.Text == "" {
			continue
		}
		label := strings.TrimPrefix(entry.Kind, "pi.")
		if entry.ToolName != nil {
			label = "tool · " + *entry.ToolName
		}
		fmt.Fprintf(&text, "%s\n%s\n\n", clean(label), clean(entry.Text))
	}
	for _, tool := range m.state.Current.Tools {
		fmt.Fprintf(&text, "%s · %s\n", clean(tool.Name), clean(tool.Status))
		if tool.Output != nil {
			fmt.Fprintln(&text, clean(*tool.Output))
		}
	}
	if partial := m.state.Current.Partial; partial != nil {
		fmt.Fprintf(&text, "assistant · streaming\n%s", clean(*partial))
	}
	return strings.Split(ansi.Hardwrap(strings.TrimRight(text.String(), "\n"), max(1, m.width), true), "\n")
}

func (m *Model) View() tea.View {
	width := max(1, m.width)
	status := string(m.state.Current.Status)
	if !m.connected {
		status = "connecting or reconnecting"
	} else if failure := m.state.Current.Failure; failure != nil && status == "failed" {
		status += " (" + clean(*failure) + ")"
	}
	lines := m.transcript()
	bottom := max(1, len(lines)-m.offset)
	start := max(0, bottom-max(1, m.height-7))
	help := "Enter send · Ctrl+F queue · Ctrl+S steer · PgUp/PgDn history · Ctrl+C detach"
	if m.mayAbort {
		help = "Ctrl+X abort · " + help
	}
	draftBefore := strings.ReplaceAll(string(m.draft[:m.cursor]), "\n", "↵")
	draftAfter := strings.ReplaceAll(string(m.draft[m.cursor:]), "\n", "↵")
	draftLine := ansi.TruncateLeft(draftBefore, max(0, ansi.StringWidth(draftBefore)-max(0, width-4)), "") + "▏" + draftAfter
	header := fmt.Sprintf("Workspace  #%s · %s · %s", m.conversation.Id, status, clean(m.conversation.Model))
	content := []string{header, clean(m.conversation.Cwd), ""}
	content = append(content, lines[start:bottom]...)
	content = append(content, "", "> "+draftLine, clean(m.notice), help)
	for i, line := range content {
		content[i] = ansi.Truncate(strings.ReplaceAll(line, "\t", "    "), width, "…")
	}
	view := tea.NewView(strings.Join(content, "\n"))
	view.AltScreen = true
	return view
}
