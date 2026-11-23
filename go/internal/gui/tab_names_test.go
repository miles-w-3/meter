package gui

import (
	"testing"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestTabNamesAreUniquePerProject(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	layout := protocol.Layout{}
	for range 24 {
		name := suggestTabName(layout, project)
		if !tabNameAvailable(layout, project, uuid.Nil, name) {
			t.Fatalf("duplicate suggestion %q", name)
		}
		addTab(&layout, project, name)
	}
	name := tabName(layout.Tabs[0])
	if tabNameAvailable(layout, project, uuid.Nil, "  "+name+"  ") {
		t.Fatal("duplicate accepted")
	}
	if !tabNameAvailable(layout, other, uuid.Nil, name) {
		t.Fatal("name in another project rejected")
	}
	if !tabNameAvailable(layout, project, layout.Tabs[0].Id, name) {
		t.Fatal("existing tab cannot keep its name")
	}
}

func TestClosedProjectDoesNotReopenOnReload(t *testing.T) {
	project := uuid.New()
	a := &app{layout: protocol.Layout{Revision: 1}, catalog: protocol.Catalog{Projects: []protocol.Project{{Id: project.String()}}}}
	a.scopePrototypeTabs()
	if a.layout.ActiveProject != nil {
		t.Fatal("closed project reopened")
	}
}

func TestRecentPaneTitleAndCanonicalSearch(t *testing.T) {
	project := uuid.New()
	id, branch := "original", "branch"
	canonical, custom := "Canonical title", "My working pane"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{node})
	a := &app{layout: protocol.Layout{Tabs: []protocol.LayoutTab{tab}}, catalog: protocol.Catalog{
		Conversations: []protocol.Conversation{{Id: id}},
	}}
	if a.seedPaneTitles(&a.layout) {
		t.Fatal("should not persist a fallback title")
	}
	a.catalog.Conversations[0].Title = &canonical
	if !a.seedPaneTitles(&a.layout) {
		t.Fatal("should copy the first known conversation title to the pane")
	}
	pane := &a.layout.Tabs[0].Nodes[nodeIndex(&tab, node.Id)]
	if pane.Name == nil || *pane.Name != canonical {
		t.Fatalf("pane was not seeded: %+v", pane)
	}
	pane.Name = &custom
	for _, query := range []string{canonical, custom} {
		a.filter = query
		rows := a.recentConversations()
		if len(rows) != 1 || a.conversationPaneTitle(rows[0].node, rows[0].conversation) != custom {
			t.Fatalf("search %q did not find the renamed pane: %+v", query, rows)
		}
	}
	pane.ConversationId = &branch
	branchTitle := "Branch title"
	a.catalog.Conversations = append(a.catalog.Conversations, protocol.Conversation{Id: branch, Title: &branchTitle})
	if a.seedPaneTitles(&a.layout) || *pane.Name != custom || *a.catalog.Conversations[0].Title != canonical {
		t.Fatal("pane remap overwrote the pane name or the canonical title")
	}
}

func TestProjectRestoresLastTabAndFallsBackAfterClose(t *testing.T) {
	project, other := uuid.New(), uuid.New()
	a := &app{}
	addTab(&a.layout, project, "First")
	first := *a.layout.ActiveTab
	addTab(&a.layout, project, "Second")
	last := *a.layout.ActiveTab
	a.layout.ActiveProject = &project
	a.rememberActiveTab()
	a.restoreProjectTab(&a.layout, other)
	a.restoreProjectTab(&a.layout, project)
	if a.layout.ActiveTab == nil || *a.layout.ActiveTab != last {
		t.Fatal("last tab was not restored")
	}
	closeTab(&a.layout, last)
	a.restoreProjectTab(&a.layout, project)
	if a.layout.ActiveTab == nil || *a.layout.ActiveTab != first {
		t.Fatal("missing fallback after closing the remembered tab")
	}
}

func TestRecentUnreadDoesNotReappearForSameHead(t *testing.T) {
	a := &app{}
	entries := []protocol.Entry{{Id: "first"}}
	a.noteRecentHead("session", entries)
	if !a.unreadConversations["session"] {
		t.Fatal("initial history should be unread")
	}
	delete(a.unreadConversations, "session")
	a.noteRecentHead("session", entries)
	if a.unreadConversations["session"] {
		t.Fatal("the same snapshot should not mark the session unread again")
	}
	a.noteRecentHead("session", append(entries, protocol.Entry{Id: "second"}))
	if !a.unreadConversations["session"] {
		t.Fatal("new activity should mark the session unread")
	}
}
