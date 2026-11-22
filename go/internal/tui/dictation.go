package tui

import (
	"context"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-workspace.local/workspace/internal/microphone"
)

const dictationHold = 350 * time.Millisecond

type spaceHold struct {
	id      uint64
	session *session
	focused bool
}
type holdExpired struct{ id uint64 }
type dictationBlink struct{ recording *tuiRecording }
type dictationEvent struct {
	recording *tuiRecording
	preview   string
	final     bool
	err       error
	ready     bool
}
type tuiRecording struct {
	session  *session
	ctx      context.Context
	base     string
	cancel   context.CancelFunc
	release  chan struct{}
	released bool
	ready    bool
	blink    bool
	events   chan dictationEvent
}

func (m *Model) holdSpace(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.overlay.kind != overlayNone || !m.mayDictate || !m.keyReleases || !microphone.Available || !m.connected || m.api == nil {
		return false, nil
	}
	if msg.Code != ' ' || msg.Mod != 0 {
		return false, nil
	}
	if m.spaceHold != nil || m.recording != nil {
		return true, nil
	}
	m.holdID++
	hold := &spaceHold{id: m.holdID, session: m.session, focused: m.focused}
	m.spaceHold = hold
	return true, tea.Tick(dictationHold, func(time.Time) tea.Msg { return holdExpired{hold.id} })
}
func (m *Model) tapSpace(hold *spaceHold) tea.Cmd {
	if hold == nil || hold.session != m.session {
		return nil
	}
	if hold.focused {
		m.draft.InsertString(" ")
		m.resizeComposer()
		return nil
	}
	_, cmd := m.transcriptKey(tea.KeyPressMsg{Code: ' ', Text: " "})
	return cmd
}
func (m *Model) releaseSpace() tea.Cmd {
	hold := m.spaceHold
	m.spaceHold = nil
	if hold == nil {
		return nil
	}
	if m.recording != nil && m.recording.session == hold.session {
		if !m.recording.released {
			m.recording.released = true
			close(m.recording.release)
		}
		return nil
	}
	return m.tapSpace(hold)
}
func (m *Model) startDictation(id uint64) tea.Cmd {
	hold := m.spaceHold
	if hold == nil || hold.id != id || hold.session != m.session || m.recording != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	r := &tuiRecording{session: m.session, ctx: ctx, base: m.draft.Value(), cancel: cancel, release: make(chan struct{}), events: make(chan dictationEvent, 32)}
	m.recording = r
	r.session.notice = ""
	r.blink = true
	samples := make(chan []byte, 100)
	ready := make(chan struct{}, 1)
	micErr := make(chan error, 1)
	send := func(e dictationEvent) {
		select {
		case r.events <- e:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(samples)
		err := microphone.Capture(ctx, r.release, samples, ready)
		micErr <- err
		if err != nil {
			send(dictationEvent{recording: r, final: true, err: err})
		}
	}()
	go func() {
		select {
		case <-ready:
			send(dictationEvent{recording: r, ready: true})
		case <-ctx.Done():
		}
	}()
	api := m.api
	go func() {
		result, err := api.Dictate(ctx, &audioReader{ctx: ctx, samples: samples}, func(text string) { send(dictationEvent{recording: r, preview: text}) })
		select {
		case mic := <-micErr:
			if mic != nil {
				err = mic
			}
		default:
		}
		send(dictationEvent{recording: r, preview: result, final: true, err: err})
	}()
	return tea.Batch(m.listenDictation(r), blinkDictation(r))
}
func blinkDictation(r *tuiRecording) tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return dictationBlink{recording: r} })
}
func (m *Model) listenDictation(r *tuiRecording) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-r.events:
			return ev
		case <-r.ctx.Done():
			return nil
		}
	}
}
func (m *Model) receiveDictation(ev dictationEvent) tea.Cmd {
	r := ev.recording
	if m.recording != r || m.session != r.session {
		return nil
	}
	if ev.ready && !r.released {
		r.ready = true
	}
	if ev.final {
		m.recording = nil
		m.spaceHold = nil
		r.cancel()
		if ev.err != nil {
			r.session.draft.SetValue(r.base)
			r.session.notice = "Dictation failed: " + clean(ev.err.Error())
		} else {
			r.session.draft.SetValue(dictationText(r.base, ev.preview))
			r.session.notice = ""
			m.focusComposer()
		}
		m.resizeComposer()
		return nil
	}
	if ev.preview != "" {
		r.session.draft.SetValue(dictationText(r.base, ev.preview))
		m.resizeComposer()
	}
	return m.listenDictation(r)
}
func (m *Model) cancelDictation() {
	m.spaceHold = nil
	if r := m.recording; r != nil {
		m.recording = nil
		r.cancel()
		r.session.draft.SetValue(r.base)
		r.session.notice = ""
	}
}
func dictationText(base, spoken string) string {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return base
	}
	if base != "" && !strings.HasSuffix(base, " ") && !strings.HasSuffix(base, "\n") {
		base += " "
	}
	return base + spoken
}

type audioReader struct {
	ctx     context.Context
	samples <-chan []byte
	pending []byte
}

func (r *audioReader) Read(dst []byte) (int, error) {
	if len(r.pending) == 0 {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case block, ok := <-r.samples:
			if !ok {
				return 0, io.EOF
			}
			r.pending = block
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
