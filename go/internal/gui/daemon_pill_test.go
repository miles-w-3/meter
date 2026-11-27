package gui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestDaemonPillIgnoresEmptyPanes(t *testing.T) {
	project := uuid.New()
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{emptyPane()})
	a := &app{
		loaded: true, backendOnline: true, panes: make(map[uuid.UUID]*pane),
		layout: protocol.Layout{Tabs: []protocol.LayoutTab{tab}, ActiveProject: &project, ActiveTab: &tab.Id},
	}
	a.syncPanes()
	if len(a.panes) != 1 {
		t.Fatalf("expected one empty pane, got %d", len(a.panes))
	}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.daemonPill(c)
	}, 300, 60)
	driver.Frame()
	if driver.HasText("Reconnecting") || !driver.HasText("Local daemon") {
		t.Fatalf("online backend with only an empty pane: %v", driver.Texts())
	}

	a.backendOnline = false
	driver.Frame()
	if driver.HasText("Local daemon") || !driver.HasText("Backend offline") {
		t.Fatalf("offline backend should not look healthy: %v", driver.Texts())
	}
	a.backendPending = "Restart backend"
	driver.Frame()
	if !driver.HasText("Reconnecting") {
		t.Fatalf("pending backend action should show reconnecting: %v", driver.Texts())
	}
}

func TestDaemonStatusCountsOnlyConversationStreams(t *testing.T) {
	id := "conversation"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
	watching := &pane{conversation: id}
	a := &app{loaded: true, backendOnline: true, panes: map[uuid.UUID]*pane{node.Id: watching, uuid.New(): {}}}
	// A backend that stops between health checks shows first in its streams.
	if label, _ := a.daemonStatus(); label != "Reconnecting" {
		t.Fatalf("every conversation stream down = %q", label)
	}
	watching.connected = true
	if label, _ := a.daemonStatus(); label != "Local daemon" {
		t.Fatalf("a connected stream beside an empty pane = %q", label)
	}
	a.loaded = false
	if label, _ := a.daemonStatus(); label != "Connecting" {
		t.Fatalf("before the first layout load = %q", label)
	}
}
