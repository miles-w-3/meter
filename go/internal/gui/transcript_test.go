package gui

import (
	"fmt"
	"testing"

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
