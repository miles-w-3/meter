package gui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

type fakeTerminal struct{ closed int }

func (t *fakeTerminal) Close() error { t.closed++; return nil }

func TestTerminalLazyLifecycle(t *testing.T) {
	project := uuid.New()
	one, two := terminalPane("/one"), terminalPane("/two")
	tab1 := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project, Root: one.Id, Nodes: []protocol.LayoutNode{one}}
	tab2 := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project, Root: two.Id, Nodes: []protocol.LayoutNode{two}}
	created := map[string]*fakeTerminal{}
	a := &app{panes: map[uuid.UUID]*pane{}, terminalErrors: map[uuid.UUID]string{},
		layout: protocol.Layout{ActiveProject: &project, ActiveTab: &tab1.Id, Tabs: []protocol.LayoutTab{tab1, tab2}},
		newTerminal: func(opts terminal.Options) (terminalSession, error) {
			if opts.Font.Family != terminalFont {
				t.Fatalf("terminal font fallback: %q", opts.Font.Family)
			}
			term := &fakeTerminal{}
			created[opts.Dir] = term
			return term, nil
		},
	}
	a.syncPanes()
	if len(created) != 1 || created["/one"] == nil {
		t.Fatalf("initial terminals: %v", created)
	}
	a.layout.ActiveTab = &tab2.Id
	a.syncPanes()
	if len(created) != 2 || created["/one"].closed != 0 {
		t.Fatalf("switching tabs should retain sessions: %v", created)
	}
	a.layout.Tabs = []protocol.LayoutTab{tab2}
	a.syncPanes()
	if created["/one"].closed != 1 {
		t.Fatal("closing a tab must close its terminal")
	}
	a.layout.ClosedProjects = &[]uuid.UUID{project}
	a.syncPanes()
	if created["/two"].closed != 1 {
		t.Fatal("closing a project must close its terminal")
	}
	// Do not make the unit test wait for real plugin cleanup.
	a.terminalCleanupUntil = time.Time{}
	a.shutdownTerminals()
}

func TestTerminalShutdownAndDescriptor(t *testing.T) {
	node := terminalPane("/project")
	bytes, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatal(err)
	}
	managed, ok := got["clientManaged"].(map[string]any)
	if !ok || managed["type"] != "terminal" {
		t.Fatalf("descriptor: %s", bytes)
	}
	term := &fakeTerminal{}
	a := &app{terminals: map[uuid.UUID]terminalSession{node.Id: term}}
	a.shutdownTerminals()
	if term.closed != 1 || len(a.terminals) != 0 {
		t.Fatal("shutdown did not close session")
	}
}
