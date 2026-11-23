package gui

import (
	"testing"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRecentReadHeadSurvivesReopen(t *testing.T) {
	data, server := t.TempDir(), uuid.New()
	open := func(server uuid.UUID, name string) *app {
		a := &app{clientData: data, name: name}
		a.loadReadHeads(server)
		return a
	}
	entries := []protocol.Entry{{Id: "first"}}
	a := open(server, "main")
	a.noteRecentHead("seen", entries)
	a.noteRecentHead("unseen", entries)
	a.markRecentRead("seen")
	b := open(server, "main")
	b.noteRecentHead("seen", entries)
	b.noteRecentHead("unseen", entries)
	if b.unreadConversations["seen"] || !b.unreadConversations["unseen"] {
		t.Fatalf("wrong unread state after reopen: %v", b.unreadConversations)
	}
	b.noteRecentHead("seen", append(entries, protocol.Entry{Id: "second"}))
	if !b.unreadConversations["seen"] {
		t.Fatal("new activity should mark the conversation unread")
	}
	if open(uuid.New(), "main").readHeads["seen"] != "" || open(server, "other").readHeads["seen"] != "" {
		t.Fatal("read receipt leaked across servers or workspaces")
	}
}
