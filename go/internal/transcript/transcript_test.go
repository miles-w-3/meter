package transcript

import (
	"encoding/json"
	"testing"

	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestToolExpansionFollowsCommittedEntry(t *testing.T) {
	var state client.State
	decode := func(payload string) {
		t.Helper()
		var view protocol.ConversationView
		if err := json.Unmarshal([]byte(payload), &view); err != nil {
			t.Fatal(err)
		}
		state.Apply(client.Event{View: &view})
	}
	decode(`{"conversationId":"1","abortTarget":"run","tools":[{"name":"read","status":"running","callId":"one"},{"name":"read","status":"running","callId":"two"}]}`)
	expanded := make(map[string]bool)
	first := Build(&state, false, expanded)
	expanded[first[0].ID] = true

	decode(`{"conversationId":"1","abortTarget":"run","tools":[{"name":"read","status":"running","callId":"two"},{"name":"read","status":"running","callId":"one","taskId":"t"}]}`)
	next := Build(&state, false, expanded)
	if next[1].ID != first[0].ID || expanded[next[0].ID] {
		t.Fatalf("expansion followed the wrong tool: %+v", next)
	}

	decode(`{"conversationId":"1","entries":[{"id":"e1","kind":"pi.tool","toolName":"read","toolCallId":"one"},{"id":"e2","kind":"pi.tool","toolName":"read","toolCallId":"two"}],"tools":[{"name":"read","status":"done","entryId":"e2","callId":"two"}]}`)
	final := Build(&state, false, expanded)
	if len(final) != 2 || !expanded["e1"] || expanded["e2"] || expanded[first[0].ID] {
		t.Fatalf("unexpected final tools or expansion: %+v, %v", final, expanded)
	}
}
