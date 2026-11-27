package client

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"pi-workspace.local/workspace/internal/protocol"
)

func frameJSON(t *testing.T, kind string, rev int, boot string, drop, keep int, ids ...string) []byte {
	t.Helper()
	view := protocol.ConversationView{ConversationId: "2", Entries: []protocol.Entry{}}
	for _, id := range ids {
		view.Entries = append(view.Entries, protocol.Entry{Id: id})
	}
	data, err := json.Marshal(map[string]any{
		"type": kind, "revision": rev, "generation": boot, "drop": drop, "keep": keep, "view": view,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func entryIDs(view *protocol.ConversationView) []string {
	if view == nil {
		return nil
	}
	ids := []string{}
	for _, entry := range view.Entries {
		ids = append(ids, entry.Id)
	}
	return ids
}

func TestFrameOrder(t *testing.T) {
	order := frameOrder{id: "2"}
	check := func(kind string, rev int, boot string, updates, resets, reject bool) {
		t.Helper()
		event, err := order.accept(frameJSON(t, kind, rev, boot, 0, 0))
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
	check("update", 2, second, false, false, true)
	check("snapshot", 2, first, true, false, false)
	order = frameOrder{id: "2"}
	check("snapshot", 0, second, true, true, false)
}

func TestFrameOrderAppliesUpdates(t *testing.T) {
	boot := "d6f2c699-f2dc-4884-86af-ccc78f16a35d"
	order := frameOrder{id: "2"}
	steps := []struct {
		frame []byte
		want  []string
	}{
		{frameJSON(t, "snapshot", 0, boot, 0, 0, "1", "2", "3"), []string{"1", "2", "3"}},
		{frameJSON(t, "update", 1, boot, 0, 3), []string{"1", "2", "3"}},                    // Only other fields changed.
		{frameJSON(t, "update", 2, boot, 0, 3, "4"), []string{"1", "2", "3", "4"}},          // Appended.
		{frameJSON(t, "update", 3, boot, 1, 2, "4b"), []string{"2", "3", "4b"}},             // Slid, and the tail replaced.
		{frameJSON(t, "update", 4, boot, 3, 0, "9"), []string{"9"}},                         // Jumped past the window.
		{frameJSON(t, "snapshot", 5, boot, 0, 0, "8", "9"), []string{"8", "9"}},             // Trimmed resend.
		{frameJSON(t, "update", 6, boot, 0, 2, "10", "11"), []string{"8", "9", "10", "11"}}, // Continues from it.
	}
	for i, step := range steps {
		event, err := order.accept(step.frame)
		if err != nil || !slices.Equal(entryIDs(event.View), step.want) {
			t.Fatalf("step %d: got %v %v, want %v", i, entryIDs(event.View), err, step.want)
		}
		if event.EntriesUnchanged != (i == 1) {
			t.Fatalf("step %d: incorrect unchanged flag: %v", i, event.EntriesUnchanged)
		}
	}
	if _, err := order.accept(frameJSON(t, "update", 7, boot, 3, 2)); err == nil {
		t.Fatal("accepted an update that keeps entries the client does not have")
	}
}

// The fixtures come from contracts/generate.ts, so this checks that the Go
// client reads the frames the TypeScript schemas produce.
func TestFrameOrderReadsGeneratedFrames(t *testing.T) {
	data, err := os.ReadFile("../protocol/testdata/frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames []json.RawMessage
	if err := json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	order := frameOrder{id: "conversation-1"}
	var event Event
	for _, frame := range frames {
		if event, err = order.accept(frame); err != nil {
			t.Fatal(err)
		}
	}
	if got := entryIDs(event.View); !slices.Equal(got, []string{"entry-1", "entry-2"}) {
		t.Fatalf("entries %v", got)
	}
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
