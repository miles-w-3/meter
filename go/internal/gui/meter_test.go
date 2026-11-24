package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRunningPaneShowsMeterWithoutUsage(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{connected: true}
	p.state.Current.Status = protocol.Running
	p.updateMeter(time.Now())
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.paneStatusLine(c, &tab, node, "/project", false, p)
	}, 500, 80)
	driver.Frame()
	if _, ok := driver.Find("Context usage"); !ok {
		t.Fatal("running pane should show a meter before usage arrives")
	}
	p.state.Current.Status = protocol.Idle
	p.updateMeter(time.Now())
	driver.Frame()
	if _, ok := driver.Find("Context usage"); ok {
		t.Fatal("idle pane without usage should not show a meter")
	}
}

func TestPaneMeterWaveFollowsTheTurn(t *testing.T) {
	p := &pane{connected: true}
	now := time.Now()
	p.updateMeter(now)
	if _, ok := p.activity.Wave(now); ok {
		t.Fatal("an idle pane has no wave")
	}
	p.state.Current.Status = protocol.Running
	p.updateMeter(now)
	if w, ok := p.activity.Wave(now.Add(500 * time.Millisecond)); !ok || w.Position <= 0 {
		t.Fatalf("the wave should move without streaming text: %+v %v", w, ok)
	}
	if w, ok := p.activity.Wave(now.Add(1500 * time.Millisecond)); !ok || w.Position <= 0 {
		t.Fatalf("the wave should keep moving while waiting: %+v %v", w, ok)
	}
	p.connected = false
	p.updateMeter(now.Add(time.Second))
	if _, ok := p.activity.Wave(now.Add(time.Second)); ok {
		t.Fatal("a disconnected pane has no wave")
	}
}
