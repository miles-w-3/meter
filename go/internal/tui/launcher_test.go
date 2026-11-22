package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestLauncherShowsAllProjectConversationsWithoutLayout(t *testing.T) {
	id, other := uuid.New(), uuid.New()
	p := protocol.Project{Id: id.String(), Name: "test", Cwd: "/tmp/test"}
	recent, old := 200, 100
	catalog := protocol.Catalog{Projects: []protocol.Project{p}, Conversations: []protocol.Conversation{
		{Id: "1", ProjectId: &id, Title: new("Older"), LastActivity: &old},
		{Id: "2", ProjectId: &id, Title: new("Newer"), LastActivity: &recent},
		{Id: "3", ProjectId: &other, Title: new("Other")},
	}}
	l := newLauncher(context.Background(), nil, Options{}, Start{Cwd: p.Cwd})
	l.receive(launchDataMsg{catalog: catalog, cwd: p.Cwd})
	l.showConversations(p)
	items := l.list.Items()
	if len(items) != 3 || items[1].(launchItem).conversation.Id != "2" || items[2].(launchItem).conversation.Id != "1" {
		t.Fatalf("incorrect project conversations: %#v", items)
	}
	if !strings.Contains(l.View().Content, "Newer") {
		t.Fatal("missing conversation in picker")
	}
}

func TestLauncherReturnKeepsDraft(t *testing.T) {
	conv := protocol.Conversation{Id: "1", Cwd: "/tmp"}
	a := NewApp(context.Background(), nil, protocol.ServerInfo{}, Options{}, Start{Conversation: &conv})
	a.model.draft.SetValue("unsent")
	oldEpoch := a.model.streamEpoch
	a.Update(leaveMsg{})
	if a.model != nil || a.launcher == nil {
		t.Fatal("did not return to launcher")
	}
	a.Update(openMsg{conversation: conv})
	if a.model.draft.Value() != "unsent" || a.model.streamEpoch == oldEpoch {
		t.Fatal("draft lost or old stream reused")
	}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
}
