package gui

import (
	"testing"

	"github.com/Cyenoch/mygoui/form"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRunningComposerQueuesOnEnterAndStopsWithButton(t *testing.T) {
	p := &pane{draft: "next message"}
	queued, stopped := 0, 0
	driver := ui.NewTester(func(c *ui.Context) {
		(&app{}).composerWithStop(c, uuid.Nil, p, form.ComposerOptions{
			Label: "Message", SubmitLabel: "Send",
			OnSubmit: func(string) { queued++ },
		}, true, func() { stopped++ }, false)
	}, 500, 200)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.Key(0, ui.KeyEnter)
	if queued != 1 {
		t.Fatalf("enter queued %d messages", queued)
	}
	if err := driver.Click("Stop"); err != nil || stopped != 1 {
		t.Fatalf("stop not invoked: %v, count %d", err, stopped)
	}
}

func TestStopButtonKeepsComposerBottomInset(t *testing.T) {
	p := &pane{draft: "first line\nsecond line\nthird line"}
	driver := ui.NewTester(func(c *ui.Context) {
		(&app{}).composerWithStop(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send", OnSubmit: func(string) {}}, true, func() {}, false)
	}, 500, 200)
	driver.Frame()
	input, inputOK := driver.Find("Message")
	button, buttonOK := driver.Find("Stop")
	if !inputOK || !buttonOK {
		t.Fatal("composer controls missing")
	}
	if gap := input.Y + input.H - (button.Y + button.H); gap < 7 || gap > 9 {
		t.Fatalf("stop button should match send's bottom inset: input=%+v button=%+v gap=%v", input, button, gap)
	}
}

func TestEscapeRecallsLatestQueuedMessage(t *testing.T) {
	items := []protocol.QueuedMessage{{Id: "1", Mode: "followUp", Text: "first"}, {Id: "2", Mode: "followUp", Text: "last"}}
	p := &pane{connected: true, draft: "existing draft"}
	p.state.Current = protocol.ConversationView{Status: "running", Queued: &items}
	var recalled string
	driver := ui.NewTester(func(c *ui.Context) {
		(&app{}).composerWithStop(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send", OnSubmit: func(string) {}}, true, func() {}, false, func() bool {
			recalled = items[len(items)-1].Id
			return true
		})
	}, 500, 200)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.Key(0, ui.KeyEscape)
	if recalled != "2" || p.draft != "existing draft" {
		t.Fatalf("recall %q, draft %q", recalled, p.draft)
	}
}

func TestGroupBackgroundQueuePreservesOrderAndUserControls(t *testing.T) {
	mode := protocol.QueuedBackgroundMode("monitor")
	bg := func(id, job string) protocol.QueuedMessage {
		return protocol.QueuedMessage{Id: id, Mode: "followUp", Text: "header\nOutput events:\nline " + id, Background: &protocol.QueuedBackground{JobId: job, Label: "CI", Notice: "output", Mode: &mode}}
	}
	items := []protocol.QueuedMessage{bg("1", "job"), bg("2", "job"), {Id: "3", Mode: "followUp", Text: "my question"}, bg("4", "job")}
	groups := groupQueued(items)
	if len(groups) != 3 || len(groups[0].items) != 2 || groups[1].items[0].Id != "3" || groups[2].items[0].Id != "4" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	last, ok := latestUserQueued(items)
	if !ok || last.Id != "3" {
		t.Fatalf("latest user: %+v, %v", last, ok)
	}
	if _, ok := latestUserQueued(items[:2]); ok {
		t.Fatal("background event must not be recalled")
	}
	if got := queuedBackgroundText(items[0]); got != "line 1" {
		t.Fatalf("background text %q", got)
	}
	p := &pane{connected: true}
	p.state.Current = protocol.ConversationView{Status: "running", Queued: &items}
	driver := ui.NewTester(func(c *ui.Context) { queuedMessages(c, &app{}, p, "1") }, 600, 500)
	driver.Frame()
	if _, found := driver.Find("Toggle Monitor event · CI · 2 batches"); !found {
		t.Fatal("grouped monitor row missing")
	}
	if _, found := driver.Find("Steer queued message"); !found {
		t.Fatal("user follow-up controls missing")
	}
	// The event-only view must not expose controls for editing user messages.
	eventsOnly := items[:2]
	p.state.Current.Queued = &eventsOnly
	driver.Frame()
	if _, found := driver.Find("Steer queued message"); found {
		t.Fatal("monitor exposed steer control")
	}
	if _, found := driver.Find("Return queued message to draft"); found {
		t.Fatal("monitor exposed recall control")
	}
}
