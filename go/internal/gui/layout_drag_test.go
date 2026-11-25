package gui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestDesktopDragMovesTabsAndPanes(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	updates := make(chan func(), 4)
	a := &app{
		ctx: t.Context(), api: layoutTestClient(t), name: "default", loaded: true,
		update: func(fn func()) { updates <- fn }, panes: make(map[uuid.UUID]*pane),
		terminals: make(map[uuid.UUID]terminalSession),
		catalog: protocol.Catalog{
			Projects:      []protocol.Project{{Id: project.String(), Name: "Project", Cwd: "/project"}},
			Conversations: []protocol.Conversation{{Id: "1", Cwd: "/project", ProjectId: &project}},
		},
	}
	selectProject(&a.layout, project)
	addTab(&a.layout, project, "Source")
	addTab(&a.layout, other, "Unrelated")
	addTab(&a.layout, project, "Destination")
	addTab(&a.layout, project, "Third tab")
	source, unrelated, destination, third := a.layout.Tabs[0].Id, a.layout.Tabs[1].Id, a.layout.Tabs[2].Id, a.layout.Tabs[3].Id
	a.layout.ActiveTab = &source
	conversation := protocol.LayoutNode{Id: a.layout.Tabs[0].Root, ConversationId: new("1")}
	setPanes(&a.layout.Tabs[0], []protocol.LayoutNode{conversation})
	running := &pane{conversation: "1", connected: true, draft: "Unsent draft",
		state: client.State{Current: protocol.ConversationView{Status: protocol.Running}},
	}
	a.panes[conversation.Id] = running
	terminal := terminalPane("/project")
	setPanes(&a.layout.Tabs[2], []protocol.LayoutNode{terminal})
	term := &fakeTerminal{}
	a.terminals[terminal.Id] = term

	driver := ui.NewTester(a.view, 1190, 780)
	drag := func(from, to string, position float32) {
		t.Helper()
		driver.Advance(200 * time.Millisecond)
		src, srcOK := driver.Find(from)
		dst, dstOK := driver.Find(to)
		if !srcOK || !dstOK {
			t.Fatalf("missing drag source %q (%t) or destination %q (%t)", from, srcOK, to, dstOK)
		}
		x, y := src.X+src.W/2, src.Y+src.H/2
		targetX, targetY := dst.X+dst.W*position, dst.Y+dst.H/2
		driver.Press(x, y)
		driver.Move(x+15, y)
		driver.Move(targetX, targetY)
		driver.Release(targetX, targetY)
		select {
		case fn := <-updates:
			fn()
			driver.Frame()
			if a.notice != "" {
				t.Fatal(a.notice)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("drag from %q to %q did not save", from, to)
		}
	}

	drag("Source", "Third tab", .15)
	if tabIndex(&a.layout, destination) != 0 || tabIndex(&a.layout, unrelated) != 1 || tabIndex(&a.layout, source) != 2 || *a.layout.ActiveTab != source {
		t.Fatalf("wrong tab order or active tab: %+v", a.layout)
	}
	drag("Drag Conversation #1", "Destination", .5)
	if *a.layout.ActiveTab != destination || a.selectedPane != conversation.Id || a.panes[conversation.Id] != running || running.draft != "Unsent draft" || running.state.Current.Status != protocol.Running {
		t.Fatal("move did not focus and preserve the running conversation and draft")
	}
	if panes := orderedPanes(&a.layout.Tabs[tabIndex(&a.layout, source)]); len(panes) != 1 || panes[0].Empty == nil {
		t.Fatal("moving the only pane should leave a new empty pane")
	}
	if panes := orderedPanes(a.activeTab()); len(panes) != 2 || panes[0].Id != terminal.Id || panes[1].Id != conversation.Id {
		t.Fatal("moved pane was not appended to the destination")
	}
	drag("Drag Terminal", "Third tab", .5)
	if a.terminals[terminal.Id] != term || term.closed != 0 || *a.layout.ActiveTab != third || a.selectedPane != terminal.Id {
		t.Fatal("move did not focus and retain the existing terminal session")
	}
	reloaded, err := a.api.Layout(t.Context(), a.name)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Revision != 3 || *reloaded.ActiveTab != third || tabIndex(&reloaded, destination) != 0 || tabIndex(&reloaded, source) != 2 || nodeIndex(&reloaded.Tabs[tabIndex(&reloaded, third)], terminal.Id) < 0 {
		t.Fatalf("moves did not persist atomically: %+v", reloaded)
	}
}

func TestDesktopDragScrollsOverflowingStrips(t *testing.T) {
	project := uuid.New()
	a := &app{
		loaded: true, panes: make(map[uuid.UUID]*pane),
		catalog: protocol.Catalog{Projects: []protocol.Project{{Id: project.String(), Name: "Project", Cwd: "/project"}}},
	}
	selectProject(&a.layout, project)
	for i := range 10 {
		addTab(&a.layout, project, fmt.Sprintf("Project workspace tab %d", i))
	}
	a.layout.ActiveTab = &a.layout.Tabs[0].Id
	var panes []protocol.LayoutNode
	for i := range 5 {
		node := emptyPane()
		node.Name = new(fmt.Sprintf("Pane %d", i))
		panes = append(panes, node)
	}
	setPanes(a.activeTab(), panes)
	a.syncPanes()
	driver := ui.NewTester(a.view, 940, 780)
	driver.Advance(250 * time.Millisecond)
	terminal, ok := driver.Find("New terminal pane")
	if !ok {
		t.Fatal("could not locate the right edge of the tab strip")
	}
	for _, tc := range []struct {
		label string
		edge  float32
		state *ui.ScrollState
	}{
		{"Project workspace tab 0", terminal.X - 11, &a.tabScroll},
		{"Drag Pane 0", 935, &a.paneScroll},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if tc.state.MaxX <= 0 {
				t.Fatal("expected horizontal overflow")
			}
			source, ok := driver.Find(tc.label)
			if !ok {
				t.Fatal("drag source is missing")
			}
			x, y := source.X+source.W/2, source.Y+source.H/2
			driver.Press(x, y)
			driver.Move(x+15, y)
			driver.Move(tc.edge, y)
			first := tc.state.X
			driver.Advance(250 * time.Millisecond)
			if tc.state.X <= first+20 {
				t.Fatalf("expected continued edge scrolling, offset went from %g to %g", first, tc.state.X)
			}
			driver.Key(0, ui.KeyEscape)
			driver.Release(tc.edge, y)
			driver.Advance(250 * time.Millisecond)
			if a.saving || a.layout.Revision != 0 {
				t.Fatal("canceling an edge drag should not save a move")
			}
		})
	}
}

func TestPaneTransferRejectsUnavailableDestinations(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	a := &app{}
	addTab(&a.layout, project, "Source")
	addTab(&a.layout, project, "Destination")
	addTab(&a.layout, other, "Other project")
	source, destination, unrelated := &a.layout.Tabs[0], &a.layout.Tabs[1], a.layout.Tabs[2]
	drag := layoutDrag{tab: source.Id, pane: source.Root}
	if _, ok := a.transferablePane(drag, unrelated); ok || reorderedTabs(&a.layout, source.Id, unrelated.Id, true) != nil {
		t.Fatal("a drag cannot move items across projects")
	}
	source.Nodes[0].PendingFork = &protocol.PendingFork{RequestId: "pending"}
	if _, ok := a.transferablePane(drag, *destination); ok {
		t.Fatal("a pane with a pending branch send cannot transfer")
	}
	source.Nodes[0].PendingFork = nil
	full := make([]protocol.LayoutNode, maxPanes)
	for i := range full {
		full[i] = emptyPane()
	}
	setPanes(destination, full)
	if _, ok := a.transferablePane(drag, *destination); ok {
		t.Fatal("a full tab cannot accept another pane")
	}
}

func TestReorderedTabsPositions(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	var original protocol.Layout
	addTab(&original, project, "A")
	addTab(&original, other, "Unrelated")
	addTab(&original, project, "B")
	addTab(&original, project, "C")
	a, unrelated, b, c := original.Tabs[0].Id, original.Tabs[1].Id, original.Tabs[2].Id, original.Tabs[3].Id
	for _, tc := range []struct {
		name           string
		source, target uuid.UUID
		after          bool
		want           []uuid.UUID
	}{
		{"first to end", a, c, true, []uuid.UUID{b, unrelated, c, a}},
		{"last to beginning", c, a, false, []uuid.UUID{c, unrelated, a, b}},
		{"middle before first", b, a, false, []uuid.UUID{b, unrelated, a, c}},
		{"middle after last", b, c, true, []uuid.UUID{a, unrelated, c, b}},
		{"already before", a, b, false, nil},
		{"already after", b, a, true, nil},
		{"different project", a, unrelated, true, nil},
		{"self", a, a, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reorderedTabs(&original, tc.source, tc.target, tc.after)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected no move, got %v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tabs", len(got))
			}
			for i, id := range tc.want {
				if got[i].Id != id {
					t.Fatalf("tab %d got %v want %v", i, got[i].Id, id)
				}
			}
			if original.Tabs[0].Id != a || original.Tabs[2].Id != b {
				t.Fatal("mutated original layout")
			}
		})
	}
}
