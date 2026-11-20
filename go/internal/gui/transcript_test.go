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
		a.transcript(c, p, accent, "1")
	}, 500, 300)
	driver.Frame()
	if p.transcriptScroll.MaxY <= 0 {
		t.Fatal("transcript should scroll")
	}
	if !driver.HasText("Message 79") {
		t.Fatalf("latest Markdown message missing: %v", driver.Texts())
	}
}
