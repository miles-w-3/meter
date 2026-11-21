package gui

import (
	"testing"
	"time"

	"github.com/Cyenoch/mygoui/form"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
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

func TestPaneSpaceHoldDictatesWithoutComposerFocus(t *testing.T) {
	if !canCaptureAudio {
		t.Skip("audio capture unavailable")
	}
	id := uuid.New()
	p := &pane{draft: "existing"}
	a := &app{mayDictate: true, selectedPane: id, panes: map[uuid.UUID]*pane{id: p}}
	starts := 0
	first := true
	driver := ui.NewTester(func(c *ui.Context) {
		region := ui.Column(c).Fill()
		a.observePaneDictation(region)
		region.Children(func() {
			sidebar := ui.ButtonBase(c).Label("Sidebar control")
			if first {
				sidebar.Focus()
				first = false
			}
		})
		a.tickPaneDictation(c, region.FocusWithin(), func(_ uuid.UUID, _ *pane) {
			starts++
			a.dictation = &dictationRecording{pane: p, release: make(chan struct{}), cancel: func() {}}
		})
	}, 400, 100)
	driver.Frame()
	driver.KeyDown(ui.KeySpace)
	if a.spaceHold == nil || !a.spaceHold.paneOnly {
		t.Fatal("pane space press was not tracked")
	}
	a.spaceHold.at = time.Now().Add(-dictationHold)
	driver.Frame()
	if starts != 1 || p.draft != "existing" {
		t.Fatalf("hold did not start with existing draft: starts=%d draft=%q", starts, p.draft)
	}
	recording := a.dictation
	driver.KeyUp(ui.KeySpace)
	if !recording.released || a.spaceHold != nil {
		t.Fatal("release did not stop pane dictation")
	}
}

func TestPaneDictationDoesNotCaptureOtherTextFields(t *testing.T) {
	if !canCaptureAudio {
		t.Skip("audio capture unavailable")
	}
	id := uuid.New()
	p := &pane{}
	a := &app{mayDictate: true, selectedPane: id, panes: map[uuid.UUID]*pane{id: p}}
	filter := ""
	starts := 0
	driver := ui.NewTester(func(c *ui.Context) {
		a.dictationTextFocused = false
		root := ui.Column(c).Fill()
		a.observePaneDictation(root)
		root.Children(func() {
			input := ui.TextInputBase(c, &filter).Label("Filter")
			if input.Focused() {
				a.dictationTextFocused = true
			}
		})
		a.tickPaneDictation(c, root.FocusWithin(), func(uuid.UUID, *pane) { starts++ })
	}, 400, 100)
	if err := driver.Click("Filter"); err != nil {
		t.Fatal(err)
	}
	driver.KeyDown(ui.KeySpace)
	driver.Type(" ")
	driver.KeyUp(ui.KeySpace)
	if filter != " " || a.spaceHold != nil || starts != 0 {
		t.Fatalf("text field lost normal space input: filter=%q hold=%v starts=%d", filter, a.spaceHold, starts)
	}
}

func TestSelectedPaneDictationAfterClickingWorkspaceBackground(t *testing.T) {
	if !canCaptureAudio {
		t.Skip("audio capture unavailable")
	}
	project := uuid.New()
	a := &app{mayDictate: true, loaded: true, panes: map[uuid.UUID]*pane{}, catalog: protocol.Catalog{Projects: []protocol.Project{{Id: project.String(), Name: "Project"}}}}
	selectProject(&a.layout, project)
	addTab(&a.layout, project, "Tab")
	id := orderedPanes(a.activeTab())[0].Id
	a.panes[id] = &pane{}
	driver := ui.NewTester(a.view, 1200, 700)
	driver.Frame()
	for _, point := range []struct {
		name string
		x, y float32
	}{
		{"pane background", 600, 350},
		{"sidebar background", 5, 680},
	} {
		driver.Press(point.x, point.y)
		driver.Release(point.x, point.y)
		driver.KeyDown(ui.KeySpace)
		if a.spaceHold == nil || !a.spaceHold.paneOnly {
			t.Fatalf("selected pane did not receive space after clicking %s", point.name)
		}
		driver.KeyUp(ui.KeySpace)
	}
}

// Exercise the complete view, including its end-of-frame visibility check. A
// pane recording starts after the composer has already rendered that frame.
func TestSelectedPaneRecordingSurvivesFirstViewFrame(t *testing.T) {
	if !canCaptureAudio {
		t.Skip("audio capture unavailable")
	}
	project := uuid.New()
	a := &app{mayDictate: true, loaded: true, panes: map[uuid.UUID]*pane{}, catalog: protocol.Catalog{Projects: []protocol.Project{{Id: project.String(), Name: "Project"}}}}
	selectProject(&a.layout, project)
	addTab(&a.layout, project, "Tab")
	id := orderedPanes(a.activeTab())[0].Id
	p := &pane{draft: "existing"}
	a.panes[id] = p
	canceled := false
	starts := 0
	driver := ui.NewTester(func(c *ui.Context) {
		a.viewWithDictation(c, func(got uuid.UUID, target *pane) {
			if got != id || target != p {
				t.Fatalf("wrong target: %s %p", got, target)
			}
			starts++
			a.dictation = &dictationRecording{pane: target, base: target.draft, release: make(chan struct{}), cancel: func() { canceled = true }}
		})
	}, 1200, 700)
	driver.Frame()
	driver.Press(5, 680)
	driver.Release(5, 680)
	driver.KeyDown(ui.KeySpace)
	if a.spaceHold == nil {
		t.Fatal("space hold not tracked")
	}
	a.spaceHold.at = time.Now().Add(-dictationHold)
	driver.Frame()
	if starts != 1 || a.dictation == nil || canceled {
		t.Fatalf("recording did not survive start: starts=%d recording=%v canceled=%v", starts, a.dictation, canceled)
	}
	driver.Frame()
	if a.dictation == nil || canceled || !driver.HasText("Starting…") {
		t.Fatal("recording was not visible on the next frame")
	}
	recording := a.dictation
	driver.KeyUp(ui.KeySpace)
	if !recording.released || a.dictation != recording {
		t.Fatal("release did not retain the recording for transcription")
	}
	driver.Frame()
	if !driver.HasText("Transcribing…") || canceled {
		t.Fatal("recording was canceled before transcription")
	}
}

func TestPaneDictationPlacesCaretAfterResult(t *testing.T) {
	id := uuid.New()
	p := &pane{draft: "héllo"}
	a := &app{selectedPane: id, panes: map[uuid.UUID]*pane{id: p}}
	driver := ui.NewTester(func(c *ui.Context) {
		root := ui.Column(c).Fill().Focusable()
		root.Children(func() {
			a.composer(c, id, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send"})
		})
	}, 400, 160)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.Key(0, ui.KeyHome)
	driver.Press(10, 140)
	driver.Release(10, 140)
	a.dictationTextFocused = false
	recording := &dictationRecording{pane: p, base: p.draft, paneOnly: true}
	a.dictation = recording
	a.finishDictation(id, recording, "世界", nil, nil)
	driver.Frame()
	if !driver.Focused("Message") {
		t.Fatal("composer was not focused after pane dictation")
	}
	driver.Type("!")
	if p.draft != "héllo 世界!" {
		t.Fatalf("typing did not continue after dictated text: %q", p.draft)
	}
}

func TestPaneDictationDoesNotStealAnotherTextField(t *testing.T) {
	id := uuid.New()
	p := &pane{draft: "base"}
	a := &app{selectedPane: id, panes: map[uuid.UUID]*pane{id: p}, dictationTextFocused: true}
	recording := &dictationRecording{pane: p, base: p.draft, paneOnly: true}
	a.dictation = recording
	a.finishDictation(id, recording, "spoken", nil, nil)
	if p.focusDictatedDraft || p.draft != "base spoken" {
		t.Fatalf("unexpected focus request or draft: %v %q", p.focusDictatedDraft, p.draft)
	}
}
