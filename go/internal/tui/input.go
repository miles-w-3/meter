package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/protocol"
)

func (m *Model) focusComposer() tea.Cmd {
	m.focused = true
	m.stopUntil = time.Time{}
	return m.draft.Focus()
}

func (m *Model) focusTranscript() {
	m.focused = false
	m.draft.Blur()
}

func (m *Model) resizeComposer() {
	m.draft.SetStyles(textarea.Styles{
		Focused: textarea.StyleState{Prompt: m.colors.moss, Text: m.colors.fog, Placeholder: m.colors.muted, Selection: m.colors.moss.Reverse(!m.plain)},
		Blurred: textarea.StyleState{Prompt: m.colors.faint, Text: m.colors.fog, Placeholder: m.colors.faint},
	})
	capacity, _, _ := composerLayout(m.height)
	m.draft.MinHeight, m.draft.MaxHeight = min(2, capacity), capacity
	m.draft.SetWidth(max(3, m.frame().width))
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	name := msg.String()
	now := time.Now()
	if name == "?" && (m.overlay.kind == overlayHelp || (m.overlay.kind == overlayNone && (!m.focused || m.draft.Value() == ""))) {
		if m.overlay.kind == overlayHelp {
			m.overlay = overlay{}
		} else {
			m.overlay = overlay{kind: overlayHelp}
		}
		return nil
	}
	switch name {
	case "ctrl+q":
		return tea.Quit // Detach without interrupting the daemon.
	case "ctrl+c":
		if now.Before(m.quitUntil) || !m.running() {
			return tea.Quit
		}
		m.quitUntil = now.Add(2 * time.Second)
		return tea.Batch(m.abort(), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return confirmationExpired{} }))
	case "ctrl+k":
		if m.overlay.kind == overlaySwitch {
			m.overlay = overlay{}
			return nil
		}
		return m.openSwitcher()
	}
	if m.overlay.kind != overlayNone {
		return m.updateOverlay(msg)
	}
	switch name {
	case "ctrl+r":
		if m.submission.Uncertain() && m.mayRetry && m.connected {
			return m.send()
		}
		if m.rejected {
			rejected := m.submission.Restore()
			if m.draft.Value() != "" {
				rejected += "\n" + m.draft.Value()
			}
			m.draft.SetValue(rejected)
			m.rejected, m.notice = false, ""
			m.resizeComposer()
			m.transcript.clamp(m.frame().rows)
			return m.focusComposer()
		}
		return nil
	case "ctrl+x":
		return m.abort()
	case "esc":
		if m.focused {
			m.focusTranscript()
			return nil
		}
		if m.running() && m.mayAbort && m.state.Current.AbortTarget != nil {
			target := *m.state.Current.AbortTarget
			if now.Before(m.stopUntil) && target == m.stopTarget {
				m.stopUntil = time.Time{}
				return m.abort()
			}
			m.stopUntil, m.stopTarget = now.Add(time.Second), target
			return tea.Tick(time.Second, func(time.Time) tea.Msg { return confirmationExpired{} })
		}
	}
	if !m.focused {
		if handled, cmd := m.transcriptKey(msg); handled {
			return cmd
		}
		if msg.Key().Text == "" {
			return nil
		}
		m.focusComposer()
	}
	switch name {
	case "enter":
		mode := protocol.SubmitInputMode("prompt")
		if m.running() {
			mode = "followup"
		}
		return m.submit(mode)
	case "alt+enter", "ctrl+s":
		return m.submit("steer")
	case "ctrl+f":
		return m.submit("followup")
	case "shift+enter", "ctrl+j":
		m.draft.InsertString("\n")
	case "pgup":
		m.focusTranscript()
		return m.scroll(-max(1, m.frame().rows/2), false)
	case "esc":
		return nil
	default:
		var cmd tea.Cmd
		m.draft, cmd = m.draft.Update(msg)
		m.resizeComposer()
		m.transcript.clamp(m.frame().rows)
		return cmd
	}
	m.resizeComposer()
	m.transcript.clamp(m.frame().rows)
	return nil
}

func (m *Model) transcriptKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	t := &m.transcript
	page := max(1, m.frame().rows/2)
	switch msg.String() {
	case "j", "down":
		return true, m.scroll(1, false)
	case "k", "up":
		return true, m.scroll(-1, false)
	case "ctrl+d", "pgdown":
		return true, m.scroll(page, false)
	case "ctrl+u", "pgup":
		return true, m.scroll(-page, false)
	case "g":
		t.follow = false
		m.moveTo(0)
		return true, m.loadHistory()
	case "G":
		t.toBottom(m.frame().rows)
	case "[", "]":
		backwards := msg.String() == "["
		target := -1
		for _, b := range t.blocks {
			if b.block.kind != userBlock {
				continue
			}
			if backwards && b.start < t.cursor {
				target = b.start
			} else if !backwards && b.start > t.cursor {
				target = b.start
				break
			}
		}
		if target >= 0 {
			t.follow = false
			m.moveTo(target)
		} else if backwards {
			return true, m.loadHistory()
		}
	case "space":
		m.toggleTool(t.cursor)
	case "y":
		return true, m.yank()
	case "/":
		m.overlay = overlay{kind: overlaySearch}
		m.search.SetValue(m.query)
		m.search.CursorEnd()
		m.resizeOverlay()
		return true, m.search.Focus()
	case "n", "N":
		m.searchPending, m.searchBackward = true, msg.String() == "N"
		return true, m.finishSearch()
	case "ctrl+o":
		return true, m.openPager()
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) mouse(message tea.Msg) tea.Cmd {
	if m.options.NoMouse || m.dumb {
		return nil
	}
	mouse := message.(tea.MouseMsg).Mouse()
	if m.overlay.kind != overlayNone {
		if _, wheel := message.(tea.MouseWheelMsg); wheel {
			delta := 3
			if mouse.Button == tea.MouseWheelUp {
				delta = -3
			}
			if m.overlay.kind == overlayPager {
				m.scrollPager(delta)
			} else if m.overlay.kind == overlayHelp {
				m.scrollHelp(delta)
			} else if m.overlay.kind == overlaySwitch {
				for range 3 {
					if delta < 0 {
						m.overlay.list.CursorUp()
					} else {
						m.overlay.list.CursorDown()
					}
				}
			}
		}
		return nil
	}
	f := m.frame()
	switch message.(type) {
	case tea.MouseWheelMsg:
		if mouse.Y < frameTop || mouse.Y >= f.transcriptEnd() {
			return nil
		}
		delta := 3
		switch mouse.Button {
		case tea.MouseWheelUp:
			delta = -3
		case tea.MouseWheelDown:
		default:
			return nil
		}
		var notice tea.Cmd
		if m.mouseHintUntil.IsZero() {
			m.mouseHintUntil = time.Now().Add(3 * time.Second)
			notice = tea.Tick(3*time.Second, func(time.Time) tea.Msg { return confirmationExpired{} })
		}
		m.focusTranscript()
		return tea.Batch(notice, m.scroll(delta, true))
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			break
		}
		if mouse.Y >= frameTop && mouse.Y < f.transcriptEnd() && mouse.X >= f.left && mouse.X < f.left+f.width {
			m.focusTranscript()
			line := m.transcript.top + mouse.Y - frameTop - f.transcriptPad(m.transcript.total)
			if line < 0 {
				return nil
			}
			m.moveTo(line)
			if b, ok := m.transcript.at(line); ok && b.start == line {
				m.toggleTool(line)
			}
		} else if mouse.Y >= f.composerStart() && mouse.Y < f.composerStart()+f.composerRows {
			return m.focusComposer()
		}
	}
	return nil
}

func (m *Model) toggleTool(line int) {
	if b, ok := m.transcript.at(line); ok && b.block.kind == toolBlock && b.block.text != "" {
		m.transcript.follow = false
		m.transcript.expanded[b.block.id] = !m.transcript.expanded[b.block.id]
		m.refresh()
	}
}

func (m *Model) finishSearch() tea.Cmd {
	t := &m.transcript
	query := strings.ToLower(m.query)
	if query == "" {
		m.searchPending = false
		return nil
	}
	first, next := -1, -1
	// Search the materialized transcript, including the body of collapsed tools.
	for _, b := range t.blocks {
		hit := false
		for i, line := range b.lines {
			if !strings.Contains(strings.ToLower(ansi.Strip(line)), query) {
				continue
			}
			hit = true
			pos := b.start + i
			if first < 0 || m.searchBackward {
				first = pos
			}
			if m.searchBackward && pos < t.cursor || !m.searchBackward && pos > t.cursor && next < 0 {
				next = pos
			}
		}
		if !hit && strings.Contains(strings.ToLower(b.block.text), query) {
			if first < 0 || m.searchBackward {
				first = b.start
			}
			if m.searchBackward && b.start < t.cursor || !m.searchBackward && b.start > t.cursor && next < 0 {
				next = b.start
			}
		}
	}
	if next < 0 && m.state.Before != nil && (first < 0 || m.searchBackward) {
		m.notice = "Searching older history…"
		if m.api != nil {
			return m.loadHistory()
		}
	}
	m.searchPending = false
	if next < 0 {
		next = first
	}
	if next < 0 {
		m.notice = "No match for " + m.query
		return nil
	}
	m.notice = "/" + m.query + " · n/N next/previous"
	t.follow = false
	m.focusTranscript()
	m.moveTo(next)
	if b, ok := t.at(next); ok && b.block.kind == toolBlock && !t.expanded[b.block.id] &&
		strings.Contains(strings.ToLower(b.block.text), query) {
		t.expanded[b.block.id] = true
		m.refresh()
		for i, line := range t.cache[b.block.id].lines {
			if strings.Contains(strings.ToLower(ansi.Strip(line)), query) {
				m.moveTo(b.start + i)
				break
			}
		}
	}
	return nil
}

func (m *Model) yank() tea.Cmd {
	t := &m.transcript
	b, ok := t.at(t.cursor)
	if !ok {
		return nil
	}
	if b.block.kind == assistantBlock {
		line := strings.TrimSpace(ansi.Strip(b.lines[t.cursor-b.start]))
		if code := codeAt(b.block.text, line); code != "" {
			m.notice = "Code copied"
			return tea.SetClipboard(code)
		}
	}
	if b.block.kind == toolBlock && t.cursor > b.start {
		m.notice = "Tool output copied"
		return tea.SetClipboard(b.block.text)
	}
	start := 0
	for i, block := range t.blocks {
		if block.start > t.cursor {
			break
		}
		if block.block.kind == userBlock {
			start = i
		}
	}
	var parts []string
	for i, block := range t.blocks[start:] {
		if i > 0 && block.block.kind == userBlock {
			break
		}
		parts = append(parts, block.block.text)
	}
	m.notice = "Turn copied"
	return tea.SetClipboard(strings.Join(parts, "\n\n"))
}

func codeAt(markdown, rendered string) string {
	line, ok := strings.CutPrefix(rendered, "│")
	if !ok || strings.TrimSpace(line) == "" {
		return ""
	}
	var fence string
	var code []string
	for raw := range strings.SplitSeq(markdown, "\n") {
		marker := fenceMarker(raw)
		if fence == "" && marker != "" {
			fence = marker
			code = nil
		} else if fence != "" && strings.HasPrefix(marker, fence) {
			for _, candidate := range code {
				if strings.Contains(strings.TrimSpace(candidate), strings.TrimSpace(line)) {
					return strings.Join(code, "\n")
				}
			}
			fence = ""
		} else if fence != "" {
			code = append(code, raw)
		}
	}
	return ""
}
