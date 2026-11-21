package tui

import (
	"context"
	"os"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

type Options struct {
	NoMouse, Transparent bool
	Workspace            string
}

type session struct {
	conversation                               protocol.Conversation
	state                                      client.State
	draft                                      textarea.Model
	submission                                 client.Submission
	loading, connected, reconnecting, rejected bool
	notice                                     string
	transcript                                 transcript
}

type Model struct {
	*session
	ctx                           context.Context
	api                           *client.Client
	sessions                      map[string]*session
	info                          protocol.ServerInfo
	options                       Options
	cancelStream                  context.CancelFunc
	streamEpoch                   int
	events                        <-chan client.Event
	pending                       []client.Event
	ticking                       bool
	width, height                 int
	plain, dumb                   bool
	colors                        terminalTheme
	focused                       bool
	mayAbort, mayRetry            bool
	stopUntil, quitUntil          time.Time
	stopTarget                    string
	spinner                       spinner.Model
	help                          help.Model
	mouseHintUntil                time.Time
	overlay                       overlay
	search                        textinput.Model
	query                         string
	searchPending, searchBackward bool
	directory                     map[string]conversationItem
	catalogEpoch                  int
	spark                         []float64
	lastPartial                   int
	lastDelta                     time.Time
	runTarget                     string
	rate                          float64
}

func New(ctx context.Context, api *client.Client, conversation protocol.Conversation, info protocol.ServerInfo, options Options) *Model {
	if options.Workspace == "" {
		options.Workspace = "default"
	}
	dumb := os.Getenv("TERM") == "dumb"
	_, noColor := os.LookupEnv("NO_COLOR")
	m := &Model{
		ctx: ctx, api: api, info: info, options: options, dumb: dumb, plain: dumb || noColor,
		width: 80, height: 24, focused: true,
		sessions: make(map[string]*session), help: help.New(), search: textinput.New(),
		spinner: spinner.New(spinner.WithSpinner(spinner.Spinner{
			Frames: strings.Split("▁▃▄▅▆▇▆▅▄▃", ""), FPS: 100 * time.Millisecond,
		})),
	}
	m.colors.set(m.plain, m.options.Transparent, colorprofile.Env(os.Environ()))
	m.search.Prompt = "/ "
	m.search.CharLimit = 256
	m.search.SetStyles(textinput.Styles{})
	for _, capability := range info.Capabilities {
		switch capability {
		case "abort-target":
			m.mayAbort = true
		case "retry-submit":
			m.mayRetry = true
		}
	}
	m.open(conversation)
	return m
}

type streamMsg struct {
	epoch int
	event client.Event
}
type frameMsg struct{}
type sentMsg struct {
	session *session
	err     error
}
type actionMsg struct {
	session *session
	err     error
}
type historyMsg struct {
	session *session
	cursor  *string
	page    protocol.HistoryPage
	err     error
}
type confirmationExpired struct{}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.listen(), m.fetchCatalog())
}

func (m *Model) ConversationID() string { return m.conversation.Id }

func (m *Model) open(conversation protocol.Conversation) {
	if m.cancelStream != nil {
		m.cancelStream()
	}
	if m.session != nil {
		m.flush(time.Now())
		m.draft.Blur()
	}
	s := m.sessions[conversation.Id]
	if s == nil {
		s = &session{transcript: transcript{follow: true}, draft: textarea.New()}
		s.draft.Prompt = "▎ "
		s.draft.Placeholder = "Message…"
		s.draft.ShowLineNumbers = false
		s.draft.EndOfBufferCharacter = ' '
		s.draft.CharLimit = 16_000
		s.draft.DynamicHeight = true
		s.draft.MaxHeight = 6
		s.draft.MaxContentHeight = s.draft.CharLimit
		s.draft.MaxWidth = 0
		s.draft.SetVirtualCursor(false)
		m.sessions[conversation.Id] = s
	}
	m.session = s
	m.conversation = conversation
	m.connected, m.reconnecting = false, false
	m.streamEpoch++
	m.pending = nil
	m.spark, m.lastPartial, m.lastDelta, m.rate, m.runTarget = nil, 0, time.Time{}, 0, ""
	m.stopUntil, m.quitUntil, m.stopTarget = time.Time{}, time.Time{}, ""
	m.query, m.searchPending = "", false
	m.focusComposer()
	m.resizeComposer()
	m.refresh()
	if m.api != nil {
		ctx, cancel := context.WithCancel(m.ctx)
		m.cancelStream = cancel
		m.events = m.api.Subscribe(ctx, conversation.Id)
	}
}

func (m *Model) listen() tea.Cmd {
	events, epoch := m.events, m.streamEpoch
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return nil
		}
		return streamMsg{epoch, event}
	}
}

func (m *Model) running() bool {
	return m.connected && m.state.Current.Status == protocol.Running
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeComposer()
		m.refresh()
		m.resizeOverlay()
	case tea.ColorProfileMsg:
		m.colors.set(m.plain, m.options.Transparent, msg.Profile)
		m.resizeComposer()
		m.refresh()
		m.styleSwitcher()
	case streamMsg:
		if msg.epoch != m.streamEpoch {
			break
		}
		m.pending = append(m.pending, msg.event)
		cmd = m.listen()
		if !m.ticking {
			m.ticking = true
			cmd = tea.Batch(cmd, tea.Tick(time.Second/30, func(time.Time) tea.Msg { return frameMsg{} }))
		}
	case frameMsg:
		m.ticking = false
		wasRunning := m.running()
		m.flush(time.Now())
		if !wasRunning && m.running() {
			cmd = m.spinner.Tick
		}
	case spinner.TickMsg:
		if m.running() {
			m.spinner, cmd = m.spinner.Update(msg)
			m.addSpark(time.Now())
		}
	case sentMsg:
		msg.session.handleSent(msg.err, m.mayRetry)
		m.resizeComposer()
		m.transcript.clamp(m.frame().rows)
	case actionMsg:
		msg.session.notice = "Abort requested"
		if msg.err != nil {
			msg.session.notice = clean(msg.err.Error())
		}
	case historyMsg:
		msg.session.loading = false
		if msg.err != nil {
			msg.session.notice = clean(msg.err.Error())
			if msg.session == m.session {
				m.searchPending = false
			}
			break
		}
		msg.session.state.Prepend(msg.cursor, msg.page)
		if msg.session == m.session {
			m.refresh()
			if m.searchPending {
				cmd = m.finishSearch()
			}
		}
	case catalogMsg:
		cmd = m.receiveCatalog(msg)
	case toolOutputMsg:
		m.receiveOutput(msg)
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	case tea.MouseWheelMsg, tea.MouseClickMsg:
		cmd = m.mouse(message)
	case confirmationExpired:
		// One redraw clears the expired confirmation or mouse hint.
	case tea.PasteMsg:
		if m.overlay.kind == overlaySearch || m.overlay.kind == overlaySwitch || m.overlay.pagerFind {
			cmd = m.updateOverlay(tea.PasteMsg{Content: clean(msg.Content)})
		} else if m.overlay.kind == overlayNone {
			m.focusComposer()
			m.draft.InsertString(clean(msg.Content))
			m.resizeComposer()
			m.transcript.clamp(m.frame().rows)
		}
	default:
		if m.overlay.kind != overlayNone {
			cmd = m.updateOverlay(message)
		}
	}
	return m, cmd
}

func (m *Model) flush(now time.Time) {
	if len(m.pending) == 0 {
		return
	}
	for _, event := range m.pending {
		m.connected = event.View != nil
		m.reconnecting = !m.connected && event.Err != nil
		m.state.Apply(event)
	}
	m.pending = nil
	m.trackDelta(now)
	m.refresh()
}

func (m *Model) trackDelta(now time.Time) {
	target := value(m.state.Current.AbortTarget)
	if target != m.runTarget || !m.running() {
		m.runTarget, m.spark, m.rate, m.lastPartial, m.lastDelta = target, nil, 0, 0, time.Time{}
	}
	length := len([]rune(value(m.state.Current.Partial)))
	if m.running() && length != m.lastPartial {
		if elapsed := now.Sub(m.lastDelta).Seconds(); !m.lastDelta.IsZero() && elapsed > 0 && length > m.lastPartial {
			m.rate = float64(length-m.lastPartial) / elapsed / 4
		}
		m.lastPartial, m.lastDelta = length, now
	}
}

func (m *Model) addSpark(now time.Time) {
	if now.Sub(m.lastDelta) > 350*time.Millisecond {
		m.rate = 0
	}
	m.spark = append(m.spark, m.rate)
	if len(m.spark) > 8 {
		m.spark = m.spark[1:]
	}
}

func (s *session) handleSent(err error, mayRetry bool) {
	s.submission.Resolve(err)
	s.notice = "Input accepted"
	if err == nil {
		return
	}
	s.notice = clean(err.Error())
	if s.submission.Uncertain() {
		s.notice = "Submission outcome unknown"
		if mayRetry {
			s.notice += " · ctrl+r retries"
		}
	} else if s.draft.Value() == "" {
		s.draft.SetValue(s.submission.Restore())
	} else {
		s.rejected = true
		s.notice += " · ctrl+r restores rejected input"
	}
}

func (m *Model) submit(mode protocol.SubmitInputMode) tea.Cmd {
	if !m.connected || (mode == "steer" && !m.running()) || !m.submission.Begin(mode, m.draft.Value()) {
		return nil
	}
	m.draft.Reset()
	m.resizeComposer()
	m.transcript.toBottom(m.frame().rows)
	return m.send()
}

func (m *Model) send() tea.Cmd {
	input, ok := m.submission.Take()
	if !ok {
		return nil
	}
	s, id := m.session, m.conversation.Id
	s.notice = "Submitting…"
	return func() tea.Msg {
		_, err := m.api.Submit(m.ctx, id, input)
		return sentMsg{s, err}
	}
}

func (m *Model) abort() tea.Cmd {
	if !m.running() || !m.mayAbort || m.state.Current.AbortTarget == nil {
		return nil
	}
	s, id, target := m.session, m.conversation.Id, *m.state.Current.AbortTarget
	return func() tea.Msg { return actionMsg{s, m.api.Abort(m.ctx, id, target)} }
}

func (m *Model) loadHistory() tea.Cmd {
	if m.api == nil || m.loading || m.state.Before == nil {
		return nil
	}
	s, id, cursor := m.session, m.conversation.Id, m.state.Before
	s.loading = true
	return func() tea.Msg {
		page, err := m.api.History(m.ctx, id, cursor)
		return historyMsg{s, cursor, page, err}
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

func value[T any](p *T) (v T) {
	if p != nil {
		return *p
	}
	return v
}
