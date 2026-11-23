package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestTranscriptScrollGuardRoutesNestedScroll(t *testing.T) {
	var guard transcriptScrollGuard
	var outer, inner ui.ScrollState
	driver := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().TrackScroll(&outer).HandleInput(func(e ui.InputEvent) bool { return guard.outer(e, &outer) }).Children(func() {
			ui.Column(c).Children(func() {
				ui.Box(c).Height(100).Shrink(0)
				ui.Column(c).Height(100).Shrink(0).HandleInput(func(e ui.InputEvent) bool { return guard.embedded(e, &outer) }).Children(func() {
					ui.Scroll(c).Fill().TrackScroll(&inner).Children(func() { ui.Box(c).Height(500).Shrink(0) })
				})
				ui.Box(c).Height(1000).Shrink(0)
			})
		})
	}, 200, 200)
	driver.Frame()
	driver.Scroll(50, 50, 0, 20)
	if outer.Y != 20 {
		t.Fatalf("outer start: %v", outer.Y)
	}
	driver.Scroll(50, 120, 0, 30)
	if outer.Y != 50 || inner.Y != 0 {
		t.Fatalf("active outer: outer=%v inner=%v", outer.Y, inner.Y)
	}
	driver.Advance(transcriptScrollIdle + time.Millisecond)
	driver.Scroll(50, 100, 0, 30)
	if inner.Y != 30 || outer.Y != 50 {
		t.Fatalf("fresh inner: outer=%v inner=%v", outer.Y, inner.Y)
	}
}

func TestTranscriptScrollGuardBoundsAndIsolation(t *testing.T) {
	now := time.Unix(100, 0)
	scroll := ui.ScrollState{Y: 50, MaxY: 100}
	var first, second transcriptScrollGuard
	e := ui.InputEvent{Kind: ui.InputScroll, DY: 20, Time: now}
	first.outer(e, &scroll)
	e.Time = now.Add(time.Millisecond)
	if second.embedded(e, &scroll) {
		t.Fatal("other pane should not be active")
	}
	if !first.embedded(e, &scroll) || scroll.Y != 70 {
		t.Fatalf("expected forwarded scroll: %v", scroll.Y)
	}
	scroll.Y = scroll.MaxY
	if !first.embedded(e, &scroll) || scroll.Y != scroll.MaxY {
		t.Fatal("active gesture should not hand off at edge")
	}
	e.Time = now.Add(transcriptScrollIdle + time.Second)
	if first.embedded(e, &scroll) {
		t.Fatal("idle gesture should expire")
	}
}

func TestTranscriptScrollGuardDoesNotArmAtEdgeOrForModifiers(t *testing.T) {
	now := time.Unix(200, 0)
	var guard transcriptScrollGuard
	scroll := ui.ScrollState{Y: 0, MaxY: 100}
	event := ui.InputEvent{Kind: ui.InputScroll, DY: -10, Time: now}
	guard.outer(event, &scroll)
	if !guard.last.IsZero() {
		t.Fatal("outer cannot scroll past its edge")
	}
	event.DY, event.Mods = 10, ui.Shift
	guard.outer(event, &scroll)
	if !guard.last.IsZero() {
		t.Fatal("shift scroll should not arm guard")
	}
	event.Mods = 0
	guard.outer(event, &scroll)
	event.Time = now.Add(100 * time.Millisecond)
	event.Momentum = true
	if !guard.embedded(event, &scroll) {
		t.Fatal("momentum should continue the outer scroll")
	}
	event.Mods = ui.Ctrl
	if guard.embedded(event, &scroll) {
		t.Fatal("modified scroll must remain native")
	}
}
