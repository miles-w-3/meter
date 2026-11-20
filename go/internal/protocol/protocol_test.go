package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGeneratedFrames(t *testing.T) {
	data, err := os.ReadFile("testdata/frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames []ConversationFrame
	if err := json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	value, err := frames[0].ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := value.(ConversationSnapshot)
	if !ok || snapshot.Revision != 0 || snapshot.View.Partial != nil || len(snapshot.View.Entries) != 1 || snapshot.View.Entries[0].ToolName != nil {
		t.Fatalf("snapshot lost fields: %+v", value)
	}
	value, err = frames[1].ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	update, ok := value.(ConversationUpdate)
	if !ok || update.Revision != 1 || update.View.Partial == nil || *update.View.Partial != "streaming" || len(update.View.Entries) != 1 ||
		update.View.Entries[0].ToolName == nil || *update.View.Entries[0].ToolName != "read" {
		t.Fatalf("update lost fields: %+v", value)
	}
}
