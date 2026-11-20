package gui

import (
	"testing"

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
