package tui

import (
	"errors"
	"testing"

	"pi-workspace.local/workspace/internal/protocol"
)

func TestReconnectKeepsDraft(t *testing.T) {
	model := &Model{}
	model.insert("unsent draft")
	model.Update(streamMsg{Err: errors.New("connection dropped")})
	model.Update(streamMsg{Reset: true, View: &protocol.ConversationView{}})
	if !model.connected || string(model.draft) != "unsent draft" {
		t.Fatal("reconnect changed draft")
	}
}
