package tui

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/theme"
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
	mayDictate, keyReleases       bool
	spaceHold                     *spaceHold
	holdID                        uint64
	recording                     *tuiRecording
	stopUntil, quitUntil          time.Time
	stopTarget                    string
	sweep                         sweep
	help                          help.Model
	mouseHintUntil                time.Time
	scrollbarUntil                time.Time
	overlay                       overlay
	search                        textinput.Model
	query                         string
	searchPending, searchBackward bool
	directory                     map[string]conversationItem
	catalogEpoch                  int
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
	}
	m.colors.set(m.plain, m.options.Transparent, colorprofile.Env(os.Environ()))
	m.search.Prompt = "/ "
	m.search.CharLimit = 256
	m.search.SetStyles(textinput.Styles{})
	for _, capability := range info.Capabilities {
		switch capability {
		case "dictation":
			m.mayDictate = true
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
type scrollbarFadeMsg struct{}
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
	m.cancelDictation()
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
		s.draft.Prompt = ""
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
	m.streamEpoch = int(streamEpochs.Add(1))
	m.pending = nil
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
	case tea.KeyboardEnhancementsMsg:
		m.keyReleases = msg.SupportsEventTypes()
	case holdExpired:
		cmd = m.startDictation(msg.id)
	case dictationBlink:
		if m.recording == msg.recording && !msg.recording.ready && !msg.recording.released {
			msg.recording.blink = !msg.recording.blink
			cmd = blinkDictation(msg.recording)
		}
	case dictationEvent:
		cmd = m.receiveDictation(msg)
	case tea.KeyReleaseMsg:
		if msg.Code == ' ' {
			cmd = m.releaseSpace()
		}
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
			cmd = m.startSweep()
		}
	case sweepTick:
		cmd = m.stepSweep(msg)
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
	case modelsMsg:
		cmd = m.receiveModels(msg)
	case modelChangedMsg:
		m.receiveModelChanged(msg)
	case catalogMsg:
		cmd = m.receiveCatalog(msg)
	case toolOutputMsg:
		m.receiveOutput(msg)
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	case tea.MouseWheelMsg, tea.MouseClickMsg:
		if m.recording == nil {
			cmd = m.mouse(message)
		}
	case confirmationExpired:
		// One redraw clears the expired confirmation or mouse hint.
	case tea.PasteMsg:
		if m.recording != nil {
			break
		}
		if m.overlay.kind == overlaySearch || m.overlay.kind == overlaySwitch || m.overlay.kind == overlayModel || m.overlay.pagerFind {
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
	m.refresh()
}

// While a turn runs, one Rain cell sweeps back and forth over the context
// meter, as the GUI's wave does; a round trip takes about two seconds.
const sweepStep = theme.SweepPeriod / (2 * (theme.MeterCells - 1))

type sweep struct {
	epoch, step int
	active      bool
}

type sweepTick struct{ epoch int }

// startSweep starts the tick loop for the open conversation, once.
func (m *Model) startSweep() tea.Cmd {
	if m.sweep.active && m.sweep.epoch == m.streamEpoch {
		return nil
	}
	m.sweep = sweep{epoch: m.streamEpoch, active: true}
	return sweepAfter(m.streamEpoch)
}

func (m *Model) stepSweep(msg sweepTick) tea.Cmd {
	if msg.epoch != m.sweep.epoch {
		return nil // A loop for a conversation no longer open.
	}
	if !m.running() {
		m.sweep.active = false
		return nil
	}
	m.sweep.step++
	return sweepAfter(msg.epoch)
}

func sweepAfter(epoch int) tea.Cmd {
	return tea.Tick(sweepStep, func(time.Time) tea.Msg { return sweepTick{epoch} })
}

// sweepCell is the moving cell, or -1 when no turn runs.
func (m *Model) sweepCell() int {
	if !m.running() {
		return -1
	}
	span := theme.MeterCells - 1
	cell := m.sweep.step % (2 * span)
	if cell > span {
		cell = 2*span - cell
	}
	return cell
}

func (s *session) handleSent(err error, mayRetry bool) {
	s.submission.Resolve(err)
	s.notice = ""
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

var streamEpochs atomic.Int64

func (m *Model) stop() {
	m.cancelDictation()
	if m.cancelStream != nil {
		m.cancelStream()
	}
	m.streamEpoch = int(streamEpochs.Add(1))
}
