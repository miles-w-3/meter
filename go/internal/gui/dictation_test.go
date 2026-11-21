package gui

import (
	"testing"
	"time"

	"github.com/Cyenoch/mygoui/form"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
)

func TestDictationDraft(t *testing.T) {
	for _, tc := range []struct{ base, spoken, want string }{
		{"hello", " world ", "hello world"},
		{"hello ", "world", "hello world"},
		{"hello\n", "world", "hello\nworld"},
		{"hello", " ", "hello"},
	} {
		if got := dictationDraft(tc.base, tc.spoken); got != tc.want {
			t.Errorf("dictationDraft(%q, %q) = %q, want %q", tc.base, tc.spoken, got, tc.want)
		}
	}
}

func TestDictationStatusTracksCaptureAndRelease(t *testing.T) {
	p := &pane{}
	r := &dictationRecording{pane: p}
	a := &app{dictation: r}
	driver := ui.NewTester(func(c *ui.Context) {
		a.composer(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send"})
	}, 500, 100)
	driver.Frame()
	if !driver.HasText("Starting…") {
		t.Fatal("missing starting state")
	}
	r.live = true
	driver.Frame()
	if !driver.HasText("Listening") {
		t.Fatal("missing live state")
	}
	r.released = true
	driver.Frame()
	if !driver.HasText("Transcribing…") || driver.HasText("Listening") {
		t.Fatal("release did not exit live state")
	}
}

func TestDictationSpaceTapPreservesTyping(t *testing.T) {
	p := &pane{}
	a := &app{mayDictate: true}
	driver := ui.NewTester(func(c *ui.Context) {
		a.composer(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send"})
	}, 400, 100)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.Type("hello")
	driver.TypeKey(0, ui.KeySpace, " ")
	driver.Type("world")
	if p.draft != "hello world" {
		t.Fatalf("space tap changed typing: %q", p.draft)
	}
	if a.spaceHold != nil || a.dictation != nil {
		t.Fatal("a quick tap must not start dictation")
	}
}

func TestDictationSpaceHoldRestoresDraftAndReleases(t *testing.T) {
	p := &pane{draft: "hello"}
	a := &app{mayDictate: true}
	starts := 0
	driver := ui.NewTester(func(c *ui.Context) {
		input := ui.TextAreaBase(c, &p.draft).Label("Message")
		a.dictationInput(c, input, p, false, func() {
			starts++
			a.dictation = &dictationRecording{pane: p, release: make(chan struct{})}
		})
	}, 400, 100)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.KeyDown(ui.KeySpace)
	driver.Type(" ")
	if a.spaceHold == nil {
		t.Fatal("space press was not tracked")
	}
	a.spaceHold.at = time.Now().Add(-dictationHold)
	driver.Frame()
	if starts != 1 || p.draft != "hello" {
		t.Fatalf("hold should start once and restore the draft: starts=%d draft=%q", starts, p.draft)
	}
	recording := a.dictation
	driver.KeyUp(ui.KeySpace)
	if !recording.released || a.spaceHold != nil {
		t.Fatal("space release did not stop dictation")
	}
	driver.Frame()
	if p.draft != "hello" {
		t.Fatalf("held space remained in the draft: %q", p.draft)
	}
}
