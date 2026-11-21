package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRunningPaneAndTabRequireConfirmation(t *testing.T) {
	tab := protocol.LayoutTab{Id: uuid.New()}
	node := emptyPane()
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{connected: true}
	p.state.Current.Status = protocol.Running
	a := &app{panes: map[uuid.UUID]*pane{node.Id: p}}
	if got := a.tabStatus(tab); got != "running" {
		t.Fatalf("status = %s", got)
	}
	a.requestCloseTab(tab)
	if a.closeTabConfirm == nil || *a.closeTabConfirm != tab.Id {
		t.Fatal("missing tab confirmation")
	}
	a.requestClosePane(tab.Id, node.Id)
	if a.closePaneConfirm == nil || a.closePaneConfirm.pane != node.Id {
		t.Fatal("missing pane confirmation")
	}
	p.state.Current.Status = protocol.Failed
	if got := a.tabStatus(tab); got != "failed" {
		t.Fatalf("status = %s", got)
	}
}

func TestStatusBarFitsNarrowPane(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{}
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 19, Window: 100}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		a.paneStatusLine(c, &tab, node, "/a/very/long/project/directory/meter", false, p)
	}, 320, 80)
	driver.Frame()
	model, mok := driver.Find("Choose model and thinking")
	dir, dok := driver.Find("Change directory")
	meter, uok := driver.Find("Context usage")
	if !mok || !dok || !uok {
		t.Fatalf("missing status controls: %v %v %v", mok, dok, uok)
	}
	if dir.X+dir.W > model.X+0.5 || model.X+model.W > meter.X+0.5 || meter.X+meter.W > 320 {
		t.Fatalf("overlapping controls: model=%+v directory=%+v meter=%+v", model, dir, meter)
	}
	if driver.HasText("idle") || driver.HasText("ctx 19%") {
		t.Fatal("status bar should not show status or numeric usage")
	}
}

func TestStatusPathUsesAvailableWidthAndContextTooltip(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{}
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 38000, Window: 200000}
	a := &app{}
	path := "/Users/miles/projects/meter"
	driver := ui.NewTester(func(c *ui.Context) {
		a.paneStatusLine(c, &tab, node, path, false, p)
	}, 900, 100)
	driver.Frame()
	if !driver.HasText(path) {
		t.Fatalf("path shortened before layout: %v", driver.Texts())
	}
	meter, ok := driver.Find("Context usage")
	if !ok {
		t.Fatal("context meter missing")
	}
	driver.Move(meter.X+meter.W/2, meter.Y+meter.H/2)
	driver.Advance(time.Second)
	if !driver.HasText("Context: 38,000/200,000 (19%)") {
		t.Fatalf("missing context details: %v", driver.Texts())
	}
}
