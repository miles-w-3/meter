package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/microphone"
)

func dictationModel(t *testing.T) *Model {
	t.Helper()
	if !microphone.Available {
		t.Skip("microphone capture unavailable in build")
	}
	m := testModel()
	m.api = &client.Client{}
	m.connected = true
	m.mayDictate = true
	m.keyReleases = true
	return m
}
func TestSpaceTapAndStaleHold(t *testing.T) {
	m := dictationModel(t)
	m.draft.SetValue("hello")
	m.draft.CursorEnd()
	handled, _ := m.holdSpace(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !handled || m.spaceHold == nil {
		t.Fatal("space not held")
	}
	id := m.spaceHold.id
	m.releaseSpace()
	if m.draft.Value() != "hello " {
		t.Fatalf("quick space lost: %q", m.draft.Value())
	}
	if m.startDictation(id) != nil || m.recording != nil {
		t.Fatal("released space started microphone")
	}
}
func TestSpaceWorksWithoutTerminalReleaseSupport(t *testing.T) {
	m := dictationModel(t)
	m.keyReleases = false
	m.key(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.draft.Value() != " " || m.spaceHold != nil {
		t.Fatal("ordinary typing must not wait for unsupported releases")
	}
}
func TestRecordingReleasePreviewAndCancellation(t *testing.T) {
	m := dictationModel(t)
	m.draft.SetValue("existing")
	ctx, cancel := context.WithCancel(context.Background())
	r := &tuiRecording{session: m.session, ctx: ctx, base: "existing", cancel: cancel, release: make(chan struct{}), events: make(chan dictationEvent, 2)}
	m.spaceHold = &spaceHold{session: m.session}
	m.recording = r
	m.receiveDictation(dictationEvent{recording: r, ready: true})
	if !r.ready {
		t.Fatal("recording should become ready")
	}
	m.receiveDictation(dictationEvent{recording: r, preview: "spoken"})
	if m.draft.Value() != "existing spoken" {
		t.Fatal(m.draft.Value())
	}
	m.releaseSpace()
	select {
	case <-r.release:
	default:
		t.Fatal("release did not stop recording")
	}
	m.receiveDictation(dictationEvent{recording: r, preview: "final words", final: true})
	if m.draft.Value() != "existing final words" || m.recording != nil {
		t.Fatal("final transcript not inserted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	r = &tuiRecording{session: m.session, ctx: ctx, base: m.draft.Value(), cancel: cancel, release: make(chan struct{}), events: make(chan dictationEvent, 2)}
	m.recording = r
	m.receiveDictation(dictationEvent{recording: r, preview: "partial"})
	m.cancelDictation()
	if m.draft.Value() != "existing final words" || ctx.Err() == nil {
		t.Fatal("cancel failed to restore draft and stop capture")
	}
	m.receiveDictation(dictationEvent{recording: r, preview: "late", final: true, err: errors.New("late")})
	if m.draft.Value() != "existing final words" {
		t.Fatal("stale result changed draft")
	}
}

func TestDictationIndicatorBlinksThenStaysRed(t *testing.T) {
	m := dictationModel(t)
	m.colors.set(false, false, colorprofile.TrueColor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &tuiRecording{session: m.session, ctx: ctx, cancel: cancel, release: make(chan struct{}), events: make(chan dictationEvent, 2), blink: true}
	m.recording = r
	if !strings.Contains(m.status(80), m.colors.warning.Render("■")) {
		t.Fatal("setup should show orange square")
	}
	m.Update(dictationBlink{recording: r})
	if strings.Contains(m.status(80), "■") {
		t.Fatal("setup square should blink off")
	}
	m.receiveDictation(dictationEvent{recording: r, ready: true})
	if !strings.Contains(m.status(80), m.colors.errorStyle.Render("■")) {
		t.Fatal("ready should show red square")
	}
	m.Update(dictationBlink{recording: r})
	if !strings.Contains(m.status(80), m.colors.errorStyle.Render("■")) {
		t.Fatal("ready square should stay red")
	}
	r.released = true
	if strings.Contains(m.status(80), "■") {
		t.Fatal("indicator should disappear on release")
	}
}
