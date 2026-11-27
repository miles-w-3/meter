package gui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestTranscriptMarkdownAndFade(t *testing.T) {
	p := &pane{connected: true}
	for i := range 80 {
		p.state.Current.Entries = append(p.state.Current.Entries, protocol.Entry{
			Id: fmt.Sprint(i), Kind: "pi.assistant", Text: fmt.Sprintf("**Message %d**", i),
		})
	}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.transcript(c, p, "1")
	}, 500, 300)
	driver.Frame()
	if p.transcriptScroll.MaxY <= 0 {
		t.Fatal("transcript should scroll")
	}
	if !driver.HasText("Message 79") {
		t.Fatalf("latest Markdown message missing: %v", driver.Texts())
	}
}

func TestTranscriptAssistantErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, failure string
	}{
		{"error only", "", "prefill memory exceeded"},
		{"partial text and error", "Partial response", "connection lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pane{connected: true}
			p.state.Current.Entries = []protocol.Entry{{Id: "1", Kind: "pi.assistant", Text: tc.body, Error: &tc.failure}}
			a := &app{}
			driver := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				a.transcript(c, p, "1")
			}, 600, 300)
			driver.Frame()
			if !driver.HasText("Error: " + tc.failure) {
				t.Fatalf("error missing: %v", driver.Texts())
			}
			if tc.body != "" && !driver.HasText(tc.body) {
				t.Fatalf("partial text missing: %v", driver.Texts())
			}
		})
	}
}

func TestTranscriptTableFitsAndScrollsOnResize(t *testing.T) {
	p := &pane{connected: true}
	p.state.Current.Entries = []protocol.Entry{{Id: "1", Kind: "pi.assistant", Text: `| Area | Finding | Next step | Status |
| --- | --- | --- | --- |
| Transcript | A long description that should wrap inside its own column | Another long description for the next step | Ready |
`}}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.transcript(c, p, "1")
	}, 500, 350)
	checkFits := func() {
		t.Helper()
		last, ok := driver.Find("Status")
		if !ok || last.W <= 0 || last.X+last.W > 476 {
			t.Fatalf("last header should fit within the message inset: %+v (found: %v)", last, ok)
		}
	}
	driver.Frame()
	checkFits()
	driver.SetSize(290, 350)
	driver.Frame()
	first, ok := driver.Find("Area")
	if !ok {
		t.Fatal("missing first header")
	}
	driver.Scroll(first.X+first.W/2, first.Y+first.H/2, 500, 0)
	last, ok := driver.Find("Status")
	if !ok || last.W <= 0 || last.X+last.W > 266 || last.X < 36 {
		t.Fatalf("horizontal scroll should bring the last header into view: %+v (found: %v)", last, ok)
	}
	driver.SetSize(500, 350)
	driver.Frame()
	checkFits()
}

func TestTranscriptCachesCommittedMessages(t *testing.T) {
	view := func(texts ...string) client.Event {
		entries := []protocol.Entry{}
		for i, text := range texts {
			entries = append(entries, protocol.Entry{Id: fmt.Sprint(i + 1), Kind: "pi.assistant", Text: text})
		}
		return client.Event{View: &protocol.ConversationView{ConversationId: "1", Entries: entries}}
	}
	p := &pane{connected: true}
	p.state.Apply(view("First"))
	a := &app{}
	first, _ := a.committedMessages(p, &p.state)
	again, _ := a.committedMessages(p, &p.state)
	if len(first) != 1 || &first[0] != &again[0] {
		t.Fatal("an unchanged state rebuilt its messages")
	}
	p.state.Apply(view("First", "Second"))
	next, _ := a.committedMessages(p, &p.state)
	if len(next) != 2 || next[1].text != "Second" {
		t.Fatalf("new entries missing: %+v", next)
	}
	a.hideThinking = true
	if hidden, _ := a.committedMessages(p, &p.state); &hidden[0] == &next[0] {
		t.Fatal("a display setting change reused stale messages")
	}
}

func TestToolDiffsLoadWhenTheRowIsBuilt(t *testing.T) {
	tool, count := "bash", 1
	p := &pane{connected: true}
	p.state.Current.Entries = []protocol.Entry{{Id: "7", Kind: "pi.tool", Text: "done", ToolName: &tool, DiffCount: &count}}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.transcript(c, p, "1")
	}, 600, 300)
	driver.Frame()
	// Without a backend the fetch fails, and the row offers a retry.
	if !driver.HasText("Diff unavailable · Retry") {
		t.Fatalf("missing diff status: %v", driver.Texts())
	}
	p.transcriptDiffs["7"] = &transcriptDiffs{files: []fileDiff{{"notes.txt", "old\n", "new\n", true}}}
	driver.Frame()
	if !driver.HasText("notes.txt") || driver.HasText("Diff unavailable · Retry") {
		t.Fatalf("loaded diff not shown: %v", driver.Texts())
	}
}

func TestToolDiffTogglesSeparatelyFromToolOutput(t *testing.T) {
	p := &pane{}
	msg := message{id: "edit-1", label: "edit", target: "src/greeting.go", text: "edit completed", tool: true, diffPatch: `--- a/src/greeting.go
+++ b/src/greeting.go
@@ -1 +1 @@
-before
+after
`}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		toolRow(c, p, msg)
	}, 600, 400)
	driver.Frame()
	if !driver.HasText("+1") || !driver.HasText("−1") {
		t.Fatalf("diff summary missing: %v", driver.Texts())
	}
	if p.expandedTools[msg.id] {
		t.Fatal("tool output should start collapsed")
	}
	if err := driver.Click("Toggle file diff"); err != nil {
		t.Fatal(err)
	}
	if p.expandedTools[msg.id] {
		t.Fatal("diff toggle must not expand tool output")
	}
}

func TestToolInputExpandsWithoutOutput(t *testing.T) {
	p := &pane{}
	cmd := "printf 'first'\nprintf 'last'"
	driver := ui.NewTester(func(c *ui.Context) {
		toolRow(c, p, message{id: "live", label: "bash · running", toolCommand: cmd})
	}, 500, 180)
	if err := driver.Click("bash · running"); err != nil {
		t.Fatal(err)
	}
	if !driver.HasText("$ " + cmd) {
		t.Fatalf("full command missing: %v", driver.Texts())
	}
}

// A deterministic transcript fixture exercises both timestamp and cache notice rendering.
func TestTranscriptCacheFixture(t *testing.T) {
	ptr := func(n int) *int { return &n }
	model := "fixture/model"
	p := &pane{connected: true}
	p.state.Current.Entries = []protocol.Entry{
		{Id: "1", Kind: "pi.assistant", Text: "First response", CacheInput: ptr(0), CacheRead: ptr(0), CacheWrite: ptr(5000), CacheModel: &model},
		{Id: "2", Kind: "pi.assistant", Text: "Second response", CacheInput: ptr(5100), CacheRead: ptr(0), CacheWrite: ptr(0), CacheModel: &model, Timestamp: ptr(1700000000000), Tokens: ptr(5100)},
	}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.transcript(c, p, "1") }, 600, 400)
	driver.Frame()
	if !driver.HasText("Possible cache miss: ~5.0k tokens reprocessed") {
		t.Fatalf("notice missing: %v", driver.Texts())
	}
	if !strings.Contains(messageStats(p.state.Current.Entries[1]), time.UnixMilli(1700000000000).Local().Format("Jan 2, 3:04 PM")) {
		t.Fatal("timestamp missing")
	}
}

func TestThinkingTranscriptFixture(t *testing.T) {
	var entry protocol.Entry
	if err := json.Unmarshal([]byte(`{"id":"thinking-1","kind":"pi.assistant","text":"Answer","blocks":[{"type":"thinking","text":"Visible reasoning"},{"type":"text","text":"Answer"}]}`), &entry); err != nil {
		t.Fatal(err)
	}
	p := &pane{connected: true}
	p.state.Current.Entries = []protocol.Entry{entry}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.transcript(c, p, "1") }, 600, 400)
	driver.Frame()
	if driver.HasText("Thinking") || !driver.HasText("Answer") || !driver.HasText("Visible reasoning") {
		t.Fatalf("expected thinking expanded by default: %v", driver.Texts())
	}
	if err := driver.Click("Collapse thinking"); err != nil {
		t.Fatal(err)
	}
	if driver.HasText("Visible reasoning") {
		t.Fatalf("thinking should collapse: %v", driver.Texts())
	}
	if err := driver.Click("Expand thinking"); err != nil {
		t.Fatal(err)
	}
	if !driver.HasText("Visible reasoning") {
		t.Fatalf("thinking should expand: %v", driver.Texts())
	}
	a.hideThinking = true
	driver.Frame()
	if driver.HasText("Visible reasoning") || driver.HasText("Thinking") || !driver.HasText("Answer") {
		t.Fatalf("thinking must be hidden without hiding answer: %v", driver.Texts())
	}
	a.hideThinking = false
	driver.Frame()
	if !driver.HasText("Visible reasoning") {
		t.Fatalf("thinking should return when enabled: %v", driver.Texts())
	}
}

func TestStreamingThinkingOpensForEachRun(t *testing.T) {
	p := &pane{connected: true}
	if err := json.Unmarshal([]byte(`{"conversationId":"1","status":"running","partial":null,"abortTarget":"run-1","entries":[],"tools":[],"partialBlocks":[{"type":"thinking","text":"Reasoning in progress"}]}`), &p.state.Current); err != nil {
		t.Fatal(err)
	}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.transcript(c, p, "1") }, 600, 400)
	driver.Frame()
	if !driver.HasText("Reasoning in progress") {
		t.Fatalf("streaming thinking should start open: %v", driver.Texts())
	}
	if err := driver.Click("Collapse thinking"); err != nil {
		t.Fatal(err)
	}
	if driver.HasText("Reasoning in progress") {
		t.Fatal("manual collapse was ignored")
	}
	next := "run-2"
	p.state.Current.AbortTarget = &next
	driver.Frame()
	if !driver.HasText("Reasoning in progress") {
		t.Fatal("a new run should start open")
	}
}

func TestDiffCopiesFilePathWithoutToggling(t *testing.T) {
	cases := []struct {
		name     string
		msg      message
		computed *fileDiff
		want     string
	}{
		{"patch", message{target: "src/greeting.go", diffPatch: "--- a/src/greeting.go\n+++ b/src/greeting.go\n@@ -1 +1 @@\n-before\n+after\n"}, nil, "src/greeting.go"},
		{"computed", message{target: "ignored"}, &fileDiff{path: "dir with spaces/file.txt", before: "before\n", after: "after\n", computed: true}, "dir with spaces/file.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driver := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				toolDiff(c, tc.msg, tc.computed)
			}, 600, 400)
			driver.Frame()
			if !driver.HasText("⌄") {
				t.Fatalf("diff should start open: %v", driver.Texts())
			}
			if err := driver.Click("Copy file path"); err != nil {
				t.Fatal(err)
			}
			if got := driver.Clipboard(); got != tc.want {
				t.Fatalf("clipboard = %q, want %q", got, tc.want)
			}
			if !driver.HasText("⌄") {
				t.Fatal("copying path toggled diff")
			}
			if err := driver.Click("Toggle file diff"); err != nil {
				t.Fatal(err)
			}
			if !driver.HasText("›") {
				t.Fatal("diff did not close")
			}
		})
	}
}

func TestDiffHeaderTruncatesLongPathBeforeControls(t *testing.T) {
	path := "a/very/long/project/directory/" + strings.Repeat("nested-directory/", 12) + "filename.txt"
	diff := &fileDiff{path: path, before: "before\n", after: "after\n", computed: true}
	for _, width := range []int{320, 600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			driver := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				toolDiff(c, message{label: "bash"}, diff)
			}, width, 300)
			driver.Frame()
			labels := []string{path, "Copy file path", "Computed", "+1", "−1"}
			var right float32
			for _, label := range labels {
				bounds, ok := driver.Find(label)
				if !ok || bounds.W <= 0 {
					t.Fatalf("missing %q: %+v", label, bounds)
				}
				if bounds.X < right-0.5 || bounds.X+bounds.W > float32(width)-0.5 {
					t.Fatalf("%q overflows or overlaps: %+v (previous right %v)", label, bounds, right)
				}
				if bounds.H > 28 {
					t.Fatalf("%q wrapped: %+v", label, bounds)
				}
				right = bounds.X + bounds.W
			}
			if err := driver.Click("Copy file path"); err != nil {
				t.Fatal(err)
			}
			if got := driver.Clipboard(); got != path {
				t.Fatalf("copied truncated path: %q", got)
			}
		})
	}
}

func TestThinkingRendersMarkdown(t *testing.T) {
	p := &pane{connected: true}
	if err := json.Unmarshal([]byte(`{"conversationId":"1","status":"running","partial":null,"abortTarget":"run-1","entries":[],"tools":[],"partialBlocks":[{"type":"thinking","text":"**Clarifying environment marker**\n\n- First item\n- Second item"}]}`), &p.state.Current); err != nil {
		t.Fatal(err)
	}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.transcript(c, p, "1") }, 600, 400)
	driver.Frame()
	joined := strings.Join(driver.Texts(), " ")
	if !strings.Contains(joined, "Clarifying environment marker") || !strings.Contains(joined, "First item") || !strings.Contains(joined, "Second item") {
		t.Fatalf("missing markdown text: %v", driver.Texts())
	}
	if strings.Contains(joined, "**Clarifying") || strings.Contains(joined, "- First item") {
		t.Fatalf("raw markdown markers visible: %v", driver.Texts())
	}
	(*p.state.Current.PartialBlocks)[0].Text = "**Updated reasoning**"
	driver.Frame()
	joined = strings.Join(driver.Texts(), " ")
	if !strings.Contains(joined, "Updated reasoning") || strings.Contains(joined, "**Updated") {
		t.Fatalf("streaming markdown did not update: %v", driver.Texts())
	}
	if err := driver.Click("Collapse thinking"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(driver.Texts(), " "), "Updated reasoning") {
		t.Fatal("collapsed thinking remains visible")
	}
}

func TestRunningToolElapsedDuration(t *testing.T) {
	p := &pane{connected: true}
	payload := fmt.Sprintf(`{"conversationId":"1","status":"running","partial":null,"entries":[],"tools":[{"name":"bash","status":"running","startedAt":%d}]}`, time.Now().Add(-11*time.Second).UnixMilli())
	if err := json.Unmarshal([]byte(payload), &p.state.Current); err != nil {
		t.Fatal(err)
	}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.transcript(c, p, "1")
	}, 600, 300)
	driver.Frame()
	if !driver.HasText("11s") {
		t.Fatalf("running tool duration missing: %v", driver.Texts())
	}
}

func TestTranscriptDisplaysRunFailure(t *testing.T) {
	p := &pane{connected: true}
	failure := "Run ended early"
	p.state.Current.Status = protocol.Failed
	p.state.Current.Failure = &failure
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.transcript(c, p, "1") }, 600, 400)
	driver.Frame()
	if !driver.HasText(failure) {
		t.Fatalf("missing run failure: %v", driver.Texts())
	}
}

func TestCommittedCacheSurvivesStreamingUpdate(t *testing.T) {
	a := &app{}
	p := &pane{}
	view := protocol.ConversationView{Entries: []protocol.Entry{{Id: "1", Kind: "pi.assistant", Text: "hello"}}}
	p.state.Apply(client.Event{View: &view})
	first, _ := a.committedMessages(p, &p.state)
	partial := "more"
	view.Partial = &partial
	p.state.Apply(client.Event{View: &view, EntriesUnchanged: true})
	next, _ := a.committedMessages(p, &p.state)
	if &first[0] != &next[0] {
		t.Fatal("streaming-only update rebuilt committed transcript")
	}
}
