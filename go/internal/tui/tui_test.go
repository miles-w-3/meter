package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func testModel() *Model {
	return New(context.Background(), nil, protocol.Conversation{Id: "1", Cwd: "/tmp/city-map"}, protocol.ServerInfo{Provider: "fixture"}, Options{})
}

func TestReconnectAndSwitchKeepDraft(t *testing.T) {
	model := testModel()
	model.draft.InsertString("unsent draft")
	model.Update(streamMsg{model.streamEpoch, client.Event{Err: errors.New("connection dropped")}})
	model.Update(streamMsg{model.streamEpoch, client.Event{Reset: true, View: &protocol.ConversationView{}}})
	if model.connected {
		t.Fatal("the stream should be batched until the frame tick")
	}
	model.Update(frameMsg{})
	if !model.connected || model.draft.Value() != "unsent draft" {
		t.Fatal("reconnect changed draft")
	}
	previous := model.streamEpoch
	model.open(protocol.Conversation{Id: "2"})
	model.Update(streamMsg{previous, client.Event{View: &protocol.ConversationView{}}})
	model.Update(frameMsg{})
	if model.draft.Value() != "" || model.connected {
		t.Fatal("superseded stream changed the active conversation")
	}
	model.open(protocol.Conversation{Id: "1"})
	if model.draft.Value() != "unsent draft" {
		t.Fatal("switching lost the draft")
	}
}

func TestFrameAndScrollAnchor(t *testing.T) {
	m := testModel()
	m.connected = true
	text := strings.Repeat("long string to wrap at a different width ", 4)
	for i := range 15 {
		m.state.Current.Entries = append(m.state.Current.Entries, protocol.Entry{Id: fmt.Sprint(i), Kind: "pi.user", Text: text})
	}
	m.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	m.focusTranscript()
	m.scroll(-12, true)
	anchor, _ := m.transcript.anchor(m.transcript.top)
	m.state.Current.Entries = append(m.state.Current.Entries, protocol.Entry{Id: "new", Kind: "pi.assistant", Text: "A new answer"})
	m.refresh()
	if m.transcript.follow || !m.transcript.unseen {
		t.Fatal("new output should leave an upward scroll detached")
	}
	m.Update(tea.WindowSizeMsg{Width: 55, Height: 18})
	if got, _ := m.transcript.anchor(m.transcript.top); got != anchor {
		t.Fatalf("resize moved the viewport from %s to %s", anchor, got)
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "fixture") || m.frame().hint || m.frame().scrollbar {
		t.Fatal("small terminal should hide the provider, hint row and scrollbar")
	}
	if got := strings.Count(view, "\n") + 1; got != 18 {
		t.Fatalf("frame has %d rows", got)
	}
	m.transcript.toBottom(m.frame().rows)
	if !m.transcript.follow || m.transcript.unseen {
		t.Fatal("jumping to the bottom should resume following")
	}
}

func TestToolPreviewAndSearch(t *testing.T) {
	m := testModel()
	name, success := "read", true
	m.state.Current.Entries = []protocol.Entry{
		{Id: "1", Kind: "pi.user", Text: "Read notes"},
		{Id: "2", Kind: "pi.tool-result", Text: strings.Repeat("line\n", 32) + "searchable output", ToolName: &name, ToolSucceeded: &success},
	}
	m.refresh()
	tool := m.transcript.blocks[1]
	m.focusTranscript()
	m.toggleTool(tool.start)
	lines := m.transcript.blocks[1].lines
	if len(lines) != 22 || !strings.Contains(lines[len(lines)-1], "13 more") {
		t.Fatalf("unexpected preview length: %v", lines)
	}
	m.query, m.searchPending = "searchable output", true
	m.finishSearch()
	if m.transcript.cursor != tool.start {
		t.Fatal("search did not locate tool output past the preview")
	}
	m.openPager()
	if m.overlay.kind != overlayPager || !strings.Contains(m.overlay.pagerText, "searchable output") {
		t.Fatal("pager did not include the remaining output")
	}
}

func TestMarkdownCodeAndGrowingComposer(t *testing.T) {
	m := testModel()
	source := "# Heading\n\n```go\nfirst()\n\nsecond()\n```\n\nDone."
	m.state.Current.Entries = []protocol.Entry{{Id: "code", Kind: "pi.assistant", Text: source}}
	m.refresh()
	body := ansi.Strip(strings.Join(m.transcript.blocks[0].lines, "\n"))
	if !strings.Contains(body, "│ first()") || !strings.Contains(body, "│ second()") ||
		strings.ContainsAny(body, codeStart+codeEnd) {
		t.Fatalf("missing indented code: %q", body)
	}
	if got := codeAt(source, "│ second()"); got != "first()\n\nsecond()" {
		t.Fatalf("incorrect code selection: %q", got)
	}
	m.Update(tea.PasteMsg{Content: strings.Repeat("draft\n", 9)})
	if m.draft.Height() != 6 || m.draft.LineCount() < 9 {
		t.Fatalf("expected six visible rows and a longer draft, got %d/%d", m.draft.Height(), m.draft.LineCount())
	}
}

func TestGridAndBottomAnchoredToolSelection(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	m := testModel()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	m.connected = true
	read, write, target, result, success, ms := "read", "write", "notes.txt", "result.txt", true, 200
	m.state.Current.Entries = []protocol.Entry{
		{Id: "u", Kind: "pi.user", Text: "Question"},
		{Id: "r", ToolName: &read, ToolTarget: &target, ToolSucceeded: &success, DurationMs: &ms, Text: "Note body"},
		{Id: "w", ToolName: &write, ToolTarget: &result, ToolSucceeded: &success, DurationMs: &ms, Text: "Written"},
		{Id: "a", Kind: "pi.assistant", Text: "Answer", DurationMs: &ms},
	}
	m.refresh()
	f := m.frame()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if f.width != 88 || !strings.HasPrefix(lines[0], "  meter") || strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("unexpected grid or header: %#v %q", f, lines[:2])
	}
	if !strings.HasPrefix(lines[f.transcriptEnd()-2], "    Answer") || !strings.HasPrefix(lines[f.transcriptEnd()-1], "    0.2s") {
		t.Fatal("answer and stats should share a column at the bottom of the transcript")
	}
	if f.composerRows != 2 || f.composerGap != 2 || strings.TrimSpace(strings.Join(lines[f.transcriptEnd():f.composerStart()], "")) != "" {
		t.Fatal("expected two blank rows above a two-line composer")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "    Question") || strings.Contains(strings.Join(lines[:f.transcriptEnd()], "\n"), "▎") {
		t.Fatal("user text should stay aligned without a side accent")
	}
	toolRow := frameTop + f.transcriptPad(m.transcript.total) + m.transcript.blocks[1].start
	if !strings.HasPrefix(lines[toolRow], "  ✓ read   notes.txt") || !strings.HasPrefix(lines[toolRow+1], "  ✓ write  result.txt") {
		t.Fatalf("unaligned tool rows: %q %q", lines[toolRow], lines[toolRow+1])
	}
	if got := ansi.StringWidth(strings.TrimRight(lines[toolRow], " ")); got != 90 {
		t.Fatalf("tool duration ends at column %d, want 90", got)
	}
	m.mouse(tea.MouseClickMsg{X: 5, Y: toolRow, Button: tea.MouseLeft})
	if !m.transcript.expanded["r"] || !strings.Contains(ansi.Strip(m.View().Content), "✓▾read") {
		t.Fatal("clicking the bottom-anchored tool should expand it and mark the selected row")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "      Note body") {
		t.Fatal("expanded tool text should be indented without a side accent")
	}
}

func TestForestBackgroundFallbackAndMouseHint(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	m := testModel()
	m.plain = false
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	lines := strings.Split(m.View().Content, "\n")
	f := m.frame()
	for y, bg := range map[int]string{0: background("#0e1511"), frameTop: background("#141d18"), f.composerStart(): background("#1a251f")} {
		if !strings.HasPrefix(lines[y], bg) || ansi.StringWidth(lines[y]) != m.width {
			t.Fatalf("row %d should be painted across the terminal: %q", y, lines[y])
		}
	}
	tool := "read"
	m.state.Current.Entries = []protocol.Entry{
		{Id: "u", Kind: "pi.user", Text: "Question"},
		{Id: "t", ToolName: &tool, Text: "Output"},
		{Id: "a", Kind: "pi.assistant", Text: "Answer"},
	}
	m.refresh()
	lines = strings.Split(m.View().Content, "\n")
	first := frameTop + f.transcriptPad(m.transcript.total)
	for i, bg := range []string{"#212c23", "#19252c", "#141d18"} {
		row := lines[first+m.transcript.blocks[i].start]
		if !strings.Contains(row, background(bg)) || ansi.StringWidth(row) != m.width {
			t.Fatalf("block %d should use surface %s: %q", i, bg, row)
		}
	}
	for _, options := range []struct {
		plain, transparent bool
		profile            colorprofile.Profile
	}{{true, false, colorprofile.TrueColor}, {false, true, colorprofile.TrueColor}, {false, false, colorprofile.ANSI256}} {
		m.plain, m.options.Transparent = options.plain, options.transparent
		m.Update(tea.ColorProfileMsg{Profile: options.profile})
		if strings.Contains(m.View().Content, "\x1b[48;") {
			t.Fatal("fallback should leave the terminal background alone")
		}
	}
	if strings.Contains(m.status(f.width), "Hold Shift") {
		t.Fatal("mouse notice appeared before scrolling")
	}
	m.mouse(tea.MouseWheelMsg{Y: frameTop, Button: tea.MouseWheelDown})
	if !strings.Contains(m.status(f.width), "Hold Shift") {
		t.Fatal("first scroll did not show the selection notice")
	}
	m.mouseHintUntil = time.Now().Add(-time.Second)
	m.mouse(tea.MouseWheelMsg{Y: frameTop, Button: tea.MouseWheelDown})
	if strings.Contains(m.status(f.width), "Hold Shift") {
		t.Fatal("selection notice should not repeat")
	}
}
