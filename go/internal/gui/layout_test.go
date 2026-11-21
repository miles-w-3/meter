package gui

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestDesktopLayoutControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var layout protocol.Layout
		if r.Method != http.MethodPut || r.URL.Path != "/v1/workspaces/default" || json.NewDecoder(r.Body).Decode(&layout) != nil {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		layout.Revision++
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(layout)
	}))
	defer server.Close()
	data := t.TempDir()
	endpoint, err := json.Marshal(map[string]any{
		"port": server.Listener.Addr().(*net.TCPAddr).Port, "token": strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "endpoint.json"), endpoint, 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	project := uuid.New()
	other := uuid.New()
	updates := make(chan func(), 4)
	a := &app{
		ctx: t.Context(), api: api, name: "default", loaded: true,
		update: func(fn func()) { updates <- fn },
		panes:  make(map[uuid.UUID]*pane),
		catalog: protocol.Catalog{Projects: []protocol.Project{
			{Id: project.String(), Name: "City Map", Cwd: "/city-map"},
			{Id: other.String(), Name: "Dotfiles", Cwd: "/dotfiles"},
		}},
	}
	selectProject(&a.layout, project)
	addTab(&a.layout, project, "Reliability")
	active := a.activeTab()
	first := active.Nodes[0].Id
	active.Nodes[0] = protocol.LayoutNode{Id: first, ConversationId: new("1")}
	a.panes[first] = &pane{conversation: "1", cancel: func() {}}
	a.catalog.Conversations = []protocol.Conversation{
		{Id: "1", Cwd: "/city-map", ProjectId: &project},
		{Id: "2", Cwd: "/elsewhere", ProjectId: &project},
	}

	driver := ui.NewTester(a.view, 1190, 780)
	click := func(label string) {
		t.Helper()
		if err := driver.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	saved := func() {
		t.Helper()
		select {
		case fn := <-updates:
			fn()
			driver.Frame()
			if a.notice != "" {
				t.Fatal(a.notice)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("layout was not saved, visible text: %v", driver.Texts())
		}
	}

	if recent := a.recentConversations(); len(recent) != 1 || recent[0].conversation.Id != "1" {
		t.Fatalf("recent conversations should contain only open panes: %+v", recent)
	}
	click("New pane")
	saved()
	panes := orderedPanes(a.activeTab())
	if len(panes) != 2 || panes[0].Id != first || panes[1].Empty == nil || !driver.HasText("Start a conversation") {
		t.Fatalf("new pane was not added on the right: %+v", panes)
	}

	driver.Advance(200 * time.Millisecond)
	header, ok := driver.Find("Drag Conversation #1")
	if !ok {
		t.Fatalf("pane header missing: %v", driver.Texts())
	}
	x, y := header.X+header.W/2, header.Y+header.H/2
	driver.Press(x, y)
	driver.Move(x+20, y)
	driver.Move(1180, y)
	driver.Release(1180, y)
	saved()
	if panes := orderedPanes(a.activeTab()); panes[1].Id != first {
		t.Fatalf("drop did not reorder panes: %+v", panes)
	}
	driver.Advance(time.Second)
	driver.Press(704, 240)
	driver.Move(734, 240)
	driver.Move(764, 240)
	driver.Release(764, 240)
	saved()
	resized := orderedPanes(a.activeTab())
	if resized[0].WidthShare == nil || *resized[0].WidthShare <= .52 || *resized[1].WidthShare >= .48 {
		t.Fatalf("divider did not persist the new pane widths: %+v", resized)
	}
	driver.Key(ui.Cmd, ui.KeyK)
	if !driver.HasText("Jump to conversation") {
		t.Fatal("command palette did not open")
	}
	driver.Type("Conversation #1")
	driver.Key(0, ui.KeyEnter)
	saved()
	if a.paletteOpen {
		t.Fatal("command palette stayed open")
	}
	driver.SetSize(950, 780)
	click("New pane")
	saved()
	if len(orderedPanes(a.activeTab())) != 3 || a.paneScroll.X <= 0 {
		t.Fatal("new pane did not scroll into view in a narrow window")
	}

	if err := driver.RightClick("Reliability"); err != nil {
		t.Fatal(err)
	}
	click("Rename tab")
	click("Tab name")
	driver.Key(ui.Cmd, ui.KeyA)
	driver.Type("Reconnect")
	click("Save tab")
	saved()
	if tabName(*a.activeTab()) != "Reconnect" {
		t.Fatalf("tab was not renamed: %q", tabName(*a.activeTab()))
	}
	click("New tab")
	click("Tab name")
	driver.Type("Scratch")
	click("Save tab")
	saved()
	if tabName(*a.activeTab()) != "Scratch" || !driver.HasText("Reconnect") {
		t.Fatal("new tab should have its own name and preserve the other tab")
	}
	click("Conversation #1")
	saved()
	if tabName(*a.activeTab()) != "Reconnect" {
		t.Fatal("selecting an open conversation should navigate to its tab")
	}
	if err := driver.RightClick("Reconnect"); err != nil {
		t.Fatal(err)
	}
	click("Close tab")
	saved()
	if tabName(*a.activeTab()) != "Scratch" || len(a.recentConversations()) != 0 {
		t.Fatal("closing tab should remove its conversation from Recents")
	}
	click("Dotfiles")
	saved()
	if a.layout.ActiveProject == nil || *a.layout.ActiveProject != other || driver.HasText("Scratch") {
		t.Fatal("switching projects should hide the other project's tabs")
	}
}

func TestRecentConversationsAcrossProjects(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	a := &app{}
	selectProject(&a.layout, first)
	addTab(&a.layout, first, "First")
	a.activeTab().Nodes[0].ConversationId, a.activeTab().Nodes[0].Empty = new("first"), nil
	selectProject(&a.layout, second)
	addTab(&a.layout, second, "Second")
	a.activeTab().Nodes[0].ConversationId, a.activeTab().Nodes[0].Empty = new("second"), nil
	if recent := a.recentConversations(); len(recent) != 2 {
		t.Fatalf("expected open conversations from both projects, got %+v", recent)
	}
}

func TestToolResultExpandsIndependently(t *testing.T) {
	p := &pane{}
	const summary = "read · Reconnect plan"
	const detail = "Resume from the last event ID."
	msg := message{id: "entry-1", label: "read", text: "Reconnect plan\n" + detail, tool: true}
	driver := ui.NewTester(func(c *ui.Context) { toolRow(c, p, msg) }, 440, 200)
	if driver.HasText(msg.text) {
		t.Fatal("tool result starts expanded")
	}
	if err := driver.Click(summary); err != nil {
		t.Fatal(err)
	}
	if !p.expandedTools[msg.id] || !driver.HasText(msg.text) {
		t.Fatalf("tool result did not expand: %v", driver.Texts())
	}
	if err := driver.Click(summary); err != nil {
		t.Fatal(err)
	}
	if p.expandedTools[msg.id] || driver.HasText(msg.text) {
		t.Fatal("tool result did not collapse")
	}
}

func TestPaneSelectionDefaultsAndResetsOnTabChange(t *testing.T) {
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	one, two, three := "1", "2", "3"
	tab := protocol.LayoutTab{Id: uuid.New(), Nodes: []protocol.LayoutNode{
		{Id: first, ConversationId: &one}, {Id: second, ConversationId: &two},
	}}
	other := protocol.LayoutTab{Id: uuid.New(), Nodes: []protocol.LayoutNode{{Id: third, ConversationId: &three}}}
	setPanes(&tab, tab.Nodes)
	setPanes(&other, other.Nodes)
	active := &tab
	a := &app{panes: map[uuid.UUID]*pane{first: {}, second: {}, third: {}}}
	driver := ui.NewTester(func(c *ui.Context) {
		a.surface(c, protocol.Project{}, active)
	}, 1200, 700)
	driver.Frame()
	if a.selectedPane != first {
		t.Fatalf("initial selection = %s, want first pane", a.selectedPane)
	}
	if err := driver.Click("Message 2"); err != nil {
		t.Fatal(err)
	}
	if a.selectedPane != second {
		t.Fatalf("click selection = %s, want second pane", a.selectedPane)
	}
	active = &other
	driver.Frame()
	if a.selectedPane != third {
		t.Fatalf("new tab selection = %s, want its first pane", a.selectedPane)
	}
	active = &tab
	driver.Frame()
	if a.selectedPane != first {
		t.Fatalf("returning tab selection = %s, want its first pane", a.selectedPane)
	}
}
