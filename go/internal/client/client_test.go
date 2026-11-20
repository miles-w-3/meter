package client

import (
	"encoding/json"
	"errors"
	"testing"

	"pi-workspace.local/workspace/internal/protocol"
)

func TestFrameOrder(t *testing.T) {
	var order frameOrder
	check := func(kind string, rev int, boot string, updates, resets, reject bool) {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"type": kind, "revision": rev, "generation": boot,
			"view": protocol.ConversationView{ConversationId: "2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		var frame protocol.ConversationFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		event, err := order.accept(frame, "2")
		if (event.View != nil) != updates || event.Reset != resets || (err != nil) != reject {
			t.Fatalf("%s rev %d: got %+v %v", kind, rev, event, err)
		}
	}
	first := "d6f2c699-f2dc-4884-86af-ccc78f16a35d"
	second := "fa20900b-2bb5-4d37-b54a-6374ec1f91df"
	check("update", 1, first, false, false, true)
	check("snapshot", 0, first, true, true, false)
	check("update", 1, first, true, false, false)
	check("update", 1, first, false, false, false)
	check("update", 3, first, false, false, true)
	order = frameOrder{}
	check("snapshot", 0, second, true, true, false)
}

func TestTranscriptOverlap(t *testing.T) {
	entry := func(id string) protocol.Entry { return protocol.Entry{Id: id} }
	cursor, older := "page-before-3", "page-before-1"
	var state State
	state.Apply(Event{Reset: true, View: &protocol.ConversationView{Before: &cursor, Entries: []protocol.Entry{entry("3"), entry("4")}}})
	state.Prepend(&cursor, protocol.HistoryPage{Before: &older, Entries: []protocol.Entry{entry("1"), entry("2"), entry("3")}})
	state.Apply(Event{Reset: true, View: &protocol.ConversationView{Entries: []protocol.Entry{entry("4"), entry("5")}}})
	for i, e := range state.Entries() {
		if e.Id != string(rune('1'+i)) {
			t.Fatalf("bad transcript: %+v", state.Entries())
		}
	}
	if len(state.Entries()) != 5 || state.Before == nil || *state.Before != older {
		t.Fatalf("lost ordered history: %+v", state)
	}
}

func TestSubmissionRetainsIdentity(t *testing.T) {
	var action Submission
	if !action.Begin("prompt", "input") {
		t.Fatal("could not start input")
	}
	first, ok := action.Take()
	if !ok {
		t.Fatal("could not take input")
	}
	action.Resolve(errors.New("lost receipt"))
	if action.Restore() != "" || action.Begin("prompt", "different") {
		t.Fatal("ambiguous input lost")
	}
	retry, ok := action.Take()
	if !ok || retry != first {
		t.Fatalf("retry changed request: %+v", retry)
	}
	action.Resolve(nil)
	if action.Uncertain() || !action.Begin("steer", "next") {
		t.Fatal("success failed to clear input")
	}
}
