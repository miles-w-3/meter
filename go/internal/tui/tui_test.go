package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/theme"
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
	if len(lines) != 23 || !strings.Contains(lines[len(lines)-1], "13 more") {
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
	if f.width != 80 || f.left != 15 || !strings.HasPrefix(lines[1], strings.Repeat(" ", 15)+"meter") || strings.TrimSpace(lines[0]+lines[2]+lines[3]) != "" {
		t.Fatalf("unexpected centered grid or header: %#v %q", f, lines[:4])
	}
	if f.composerRows != 1 || f.composerGap != 1 {
		t.Fatalf("unexpected composer layout: %#v", f)
	}
	first := frameTop + f.transcriptPad(m.transcript.total)
	toolRow := first + m.transcript.blocks[1].start
	if !strings.HasPrefix(lines[toolRow], strings.Repeat(" ", 16)+"✓ read   notes.txt") || !strings.HasPrefix(lines[toolRow+1], strings.Repeat(" ", 16)+"✓ write  result.txt") {
		t.Fatalf("unaligned tool rows: %q %q", lines[toolRow], lines[toolRow+1])
	}
	if got := ansi.StringWidth(strings.TrimRight(lines[toolRow], " ")); got != f.left+f.width {
		t.Fatalf("tool duration ends at %d", got)
	}
	m.mouse(tea.MouseClickMsg{X: f.left + 2, Y: toolRow, Button: tea.MouseLeft})
	if !m.transcript.expanded["r"] || strings.Contains(ansi.Strip(m.View().Content), "▾") {
		t.Fatal("click should expand without a chevron")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), strings.Repeat(" ", f.left+5)+"Note body") {
		t.Fatal("expanded body should be indented")
	}

}

func TestForestBackgroundFallbackAndMouseHint(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	m := testModel()
	m.plain = false
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	lines := strings.Split(m.View().Content, "\n")
	f := m.frame()
	for y, bg := range map[int]string{0: background("#0e1511"), frameTop: background("#141d18")} {
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
	for i, bg := range []string{"#1a251f", "#141d18", "#141d18"} {
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

func TestLayoutRhythmAndMeter(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	m := testModel()
	m.plain = false
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	tool, ok := "read", true
	duration := 100
	m.state.Current.Entries = []protocol.Entry{
		{Id: "u", Kind: "pi.user", Text: "Question"},
		{Id: "t", ToolName: &tool, ToolSucceeded: &ok, Text: "Output"},
		{Id: "a", Kind: "pi.assistant", Text: "Answer", DurationMs: &duration},
	}
	m.transcript.expanded["t"] = true
	m.refresh()
	f := m.frame()
	lines := strings.Split(m.View().Content, "\n")
	first := frameTop + f.transcriptPad(m.transcript.total)
	user := first + m.transcript.blocks[0].start
	toolRow := first + m.transcript.blocks[1].start
	answer := first + m.transcript.blocks[2].start
	for _, row := range []int{user, user + 1, user + 2} {
		if !strings.Contains(lines[row], background(theme.Panel)) {
			t.Fatalf("user row %d has no band: %q", row, lines[row])
		}
	}
	if strings.TrimSpace(ansi.Strip(lines[toolRow+1])) != "" || !strings.Contains(ansi.Strip(lines[toolRow+2]), "Output") || strings.TrimSpace(ansi.Strip(lines[answer-1])) != "" {
		t.Fatal("expanded tool spacing is incorrect")
	}
	for row := f.composerStart(); row < f.composerStart()+3; row++ {
		if !strings.Contains(lines[row], background(theme.Panel)) {
			t.Fatalf("composer row %d has no band", row)
		}
	}
	if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) != "" || strings.Contains(ansi.Strip(lines[len(lines)-2]), "? help") {
		t.Fatal("status row or bottom padding incorrect")
	}
	for _, test := range []struct {
		percent float64
		fill    string
	}{{0, ""}, {1, theme.Muted}, {71, theme.Chanterelle}, {91, theme.Madrone}} {
		meter := m.colors.contextMeter(test.percent, -1)
		if ansi.StringWidth(meter) != 10 {
			t.Fatalf("meter width: %q", meter)
		}
		if test.fill != "" && !strings.Contains(meter, m.colors.fg(test.fill).Render("━")) {
			t.Fatalf("wrong fill for %v", test.percent)
		}
	}
}

func TestStatusNoticeTakesPriorityOverHints(t *testing.T) {
	for _, width := range []int{96, 55, 24} {
		m := testModel()
		m.connected = true
		m.notice = "Submission failed"
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, m.notice) {
			t.Errorf("width %d: notice missing from view:\n%s", width, view)
		}
	}
}

func TestStatusPathAndModel(t *testing.T) {
	m := testModel()
	m.connected = true
	m.conversation.Cwd = "/some/long/project/path"
	m.conversation.Model = "test-model"
	status := ansi.Strip(m.status(80))
	if !strings.Contains(status, "/some/long/project/path") || !strings.Contains(status, "test-model") || strings.Index(status, "test-model") < strings.Index(status, "/some/long/project/path") {
		t.Fatalf("unexpected status %q", status)
	}
	narrow := ansi.Strip(m.status(30))
	if !strings.Contains(narrow, "test-model") || !strings.Contains(narrow, "path") {
		t.Fatalf("unexpected narrow status %q", narrow)
	}
}
func TestSuccessfulSubmissionIsQuiet(t *testing.T) {
	m := testModel()
	m.notice = "old"
	m.handleSent(nil, false)
	if m.notice != "" {
		t.Fatalf("success left notice %q", m.notice)
	}
}
func TestModelPickerListsAndSelectsLevels(t *testing.T) {
	m := testModel()
	m.overlay = overlay{kind: overlayModel}
	m.overlay.list = list.New(nil, modelDelegate{m.colors}, 60, 5)
	m.receiveModels(modelsMsg{session: m.session, inventory: protocol.ModelInventory{Providers: []protocol.ProviderInfo{{Id: "ok", Status: "configured"}}, Models: []protocol.ModelInfo{{Name: "Model", ModelId: "id", Provider: "ok", ThinkingLevels: []protocol.ModelInfoThinkingLevels{"off", "high"}}}}})
	m.modelKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.overlay.levels || len(m.overlay.list.Items()) != 2 {
		t.Fatal("model did not expose its thinking levels")
	}
}

func TestRunningStatusSweepsTheMeter(t *testing.T) {
	m := testModel()
	m.connected = true
	m.conversation.Model = "model"
	m.state.Current.Status = "running"
	status := ansi.Strip(m.status(80))
	if !strings.Contains(status, "ctx ") || strings.Contains(status, "running") || !strings.Contains(status, "model") {
		t.Fatalf("a running turn should show the meter before usage arrives: %q", status)
	}
	m.plain = true
	m.colors.set(true, false, colorprofile.TrueColor)
	var cells []int
	for range 2 * (theme.MeterCells - 1) {
		cell := -1
		for i, r := range []rune(ansi.Strip(m.colors.contextMeter(0, m.sweepCell()))) {
			if r == '█' {
				cell = i
			}
		}
		cells = append(cells, cell)
		m.stepSweep(sweepTick{m.sweep.epoch})
	}
	if cells[0] != 0 || cells[theme.MeterCells-1] != theme.MeterCells-1 || cells[len(cells)-1] != 1 {
		t.Fatalf("the sweep should go out and back across the meter: %v", cells)
	}
	if cmd := m.startSweep(); cmd == nil || m.startSweep() != nil {
		t.Fatal("one sweep loop should run per conversation")
	}
	if m.stepSweep(sweepTick{m.sweep.epoch - 1}) != nil {
		t.Fatal("a stale loop should stop")
	}
	m.state.Current.Status = "idle"
	if m.sweepCell() != -1 || strings.Contains(ansi.Strip(m.status(80)), "ctx ") {
		t.Fatal("the sweep and the empty meter should go after the run")
	}
	if m.stepSweep(sweepTick{m.sweep.epoch}) != nil || m.sweep.active {
		t.Fatal("the loop should stop after the run")
	}
}
