package gui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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
	if len(cacheNotices(p.state.Current.Entries[:1])) != 0 {
		t.Fatal("first request must not warn")
	}
}

func TestCacheNoticesSuppressNoiseAndReset(t *testing.T) {
	p := func(v int) *int { return &v }
	entry := func(id string, input, read, write int) protocol.Entry {
		return protocol.Entry{Id: id, CacheInput: p(input), CacheRead: p(read), CacheWrite: p(write)}
	}
	cases := []struct {
		name    string
		entries []protocol.Entry
		want    int
	}{
		{"unreported", []protocol.Entry{entry("1", 5000, 0, 0), entry("2", 5000, 0, 0)}, 0},
		{"noise floor", []protocol.Entry{entry("1", 0, 0, 5000), entry("2", 1024, 3976, 0)}, 0},
		{"miss", []protocol.Entry{entry("1", 0, 0, 5000), entry("2", 2000, 3000, 0)}, 1},
		{"reset", []protocol.Entry{entry("1", 0, 0, 5000), {Id: "c", Kind: "pi.compaction"}, entry("2", 5000, 0, 0)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(cacheNotices(tc.entries)); got != tc.want {
				t.Fatalf("got %d notices, want %d", got, tc.want)
			}
		})
	}
}
