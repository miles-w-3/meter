package gui

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

type treeFixture struct{}

func (treeFixture) HistoryTree(context.Context, string) (protocol.HistoryTree, error) {
	root, at := "1", "2"
	return protocol.HistoryTree{RootConversationId: root, Branches: []protocol.HistoryBranch{{ConversationId: root}, {ConversationId: "2", ParentConversationId: &root, At: &at}}}, nil
}
func (treeFixture) History(_ context.Context, conversation string, before *string) (protocol.HistoryPage, error) {
	if conversation == "1" {
		if before == nil {
			cursor := "older"
			return protocol.HistoryPage{Before: &cursor, Entries: []protocol.Entry{{Id: "2", Kind: "pi.assistant", Text: "answer"}}}, nil
		}
		return protocol.HistoryPage{Entries: []protocol.Entry{{Id: "1", Kind: "pi.user", Text: "question"}}}, nil
	}
	return protocol.HistoryPage{Entries: []protocol.Entry{{Id: "1", Kind: "pi.user", Text: "question"}, {Id: "2", Kind: "pi.assistant", Text: "answer"}, {Id: "3", Kind: "pi.user", Text: "alternate"}}}, nil
}

func TestTreePreviewLoadsPagesAndAvoidsRepeatedAncestors(t *testing.T) {
	rows, err := loadTreePreview(context.Background(), treeFixture{}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].label != "user · question" || rows[1].label != "assistant · answer" || rows[2].id != "fork:2" || rows[3].label != "user · alternate" || rows[3].depth != 1 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if !rows[1].current || rows[0].current || rows[3].current {
		t.Fatalf("current position should mark root's last message: %+v", rows)
	}
	branch, err := loadTreePreview(context.Background(), treeFixture{}, "2")
	if err != nil || !branch[3].current || branch[1].current {
		t.Fatalf("current position should follow selected branch: %+v, %v", branch, err)
	}
}

func TestTreeDoubleEscapeOpensOnlyForEmptyDraft(t *testing.T) {
	id := uuid.New()
	p := &pane{conversation: "1", connected: true}
	a := &app{loaded: true, selectedPane: id, panes: map[uuid.UUID]*pane{id: p}}
	now := time.Now()
	a.treeEscape(now)
	if a.tree != nil {
		t.Fatal("first escape opened tree")
	}
	a.treeEscape(now.Add(200 * time.Millisecond))
	if a.tree == nil {
		t.Fatal("double escape did not open tree")
	}
	driver := ui.NewTester(func(c *ui.Context) {
		if a.tree != nil {
			a.treePane(c, a.tree)
		}
	}, 800, 600)
	driver.Frame()
	if !driver.HasText("Conversation tree") {
		t.Fatalf("tree dialog missing: %v", driver.Texts())
	}
	if err := driver.Click("Close tree"); err != nil {
		t.Fatal(err)
	}
	if a.tree != nil {
		t.Fatal("close did not dismiss tree")
	}
	p.draft = "unsent draft"
	a.treeEscape(now)
	a.treeEscape(now.Add(100 * time.Millisecond))
	if a.tree != nil {
		t.Fatal("tree opened over a draft")
	}
}

func TestTreePreviewFoldAndFilter(t *testing.T) {
	rows, err := loadTreePreview(context.Background(), treeFixture{}, "1")
	if err != nil {
		t.Fatal(err)
	}
	preview := &treePreview{rows: rows, collapsed: map[string]bool{"fork:2": true}}
	visible := visibleTreeRows(preview)
	if len(visible) != 2 || visible[1].id != "entry:2" || !visible[1].fork {
		t.Fatalf("fold did not retain the junction and hide its descendants: %+v", visible)
	}
	preview.collapsed["fork:2"] = false
	preview.query = "ALTERNATE"
	visible = visibleTreeRows(preview)
	if len(visible) != 3 || visible[0].label != "user · question" || visible[1].label != "assistant · answer" || visible[2].label != "user · alternate" {
		t.Fatalf("filter did not preserve the path to the match: %+v", visible)
	}
}

type splitTreeFixture struct{}

func (splitTreeFixture) HistoryTree(context.Context, string) (protocol.HistoryTree, error) {
	root, at := "1", "2"
	return protocol.HistoryTree{RootConversationId: root, Branches: []protocol.HistoryBranch{{ConversationId: root}, {ConversationId: "2", ParentConversationId: &root, At: &at}, {ConversationId: "3", ParentConversationId: &root, At: &at}}}, nil
}
func (splitTreeFixture) History(_ context.Context, id string, _ *string) (protocol.HistoryPage, error) {
	entry := func(id string) protocol.Entry { return protocol.Entry{Id: id, Kind: "pi.user", Text: id} }
	switch id {
	case "1":
		return protocol.HistoryPage{Entries: []protocol.Entry{entry("1"), entry("2"), entry("3"), entry("4")}}, nil
	case "2":
		return protocol.HistoryPage{Entries: []protocol.Entry{entry("1"), entry("2"), entry("5"), entry("6")}}, nil
	default:
		return protocol.HistoryPage{Entries: []protocol.Entry{entry("1"), entry("2"), entry("7")}}, nil
	}
}
func TestTreeGutterTracksSiblingBranches(t *testing.T) {
	rows, err := loadTreePreview(context.Background(), splitTreeFixture{}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 {
		t.Fatalf("unexpected tree: %+v", rows)
	}
	if rows[2].id != "fork:2" || rows[3].id != "entry:3" || !rows[3].branchStart || rows[3].lastBranch || !rows[3].rails[0] {
		t.Fatalf("first branch: %+v", rows)
	}
	if rows[4].id != "entry:4" || rows[4].branchStart || !rows[4].rails[0] {
		t.Fatalf("first continuation: %+v", rows)
	}
	if rows[5].id != "entry:5" || !rows[5].branchStart || rows[5].lastBranch || !rows[5].rails[0] {
		t.Fatalf("middle branch: %+v", rows)
	}
	if rows[7].id != "entry:7" || !rows[7].branchStart || !rows[7].lastBranch || rows[7].rails[0] {
		t.Fatalf("last branch: %+v", rows)
	}
}

func TestTreeJunctionDisclosure(t *testing.T) {
	rows, err := loadTreePreview(context.Background(), treeFixture{}, "1")
	if err != nil {
		t.Fatal(err)
	}
	preview := &treePreview{rows: rows, collapsed: make(map[string]bool)}
	a := &app{tree: preview}
	driver := ui.NewTester(func(c *ui.Context) { a.treePane(c, preview) }, 800, 600)
	driver.Frame()
	if !driver.HasText("user · alternate") {
		t.Fatalf("branch missing: %v", driver.Texts())
	}
	if err := driver.Click("Toggle branches fork:2"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	if !preview.collapsed["fork:2"] || driver.HasText("user · alternate") {
		t.Fatal("junction did not collapse descendants")
	}
	if err := driver.Click("Toggle branches fork:2"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	if preview.collapsed["fork:2"] || !driver.HasText("user · alternate") {
		t.Fatal("junction did not expand descendants")
	}
}
