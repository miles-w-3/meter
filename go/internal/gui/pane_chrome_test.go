package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRunningPaneAndTabRequireConfirmation(t *testing.T) {
	tab := protocol.LayoutTab{Id: uuid.New()}
	node := emptyPane()
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{connected: true}
	p.state.Current.Status = protocol.Running
	a := &app{panes: map[uuid.UUID]*pane{node.Id: p}}
	if got := a.tabStatus(tab); got != "running" {
		t.Fatalf("status = %s", got)
	}
	a.requestCloseTab(tab)
	if a.closeTabConfirm == nil || *a.closeTabConfirm != tab.Id {
		t.Fatal("missing tab confirmation")
	}
	a.requestClosePane(tab.Id, node.Id)
	if a.closePaneConfirm == nil || a.closePaneConfirm.pane != node.Id {
		t.Fatal("missing pane confirmation")
	}
	p.state.Current.Status = protocol.Failed
	if got := a.tabStatus(tab); got != "failed" {
		t.Fatalf("status = %s", got)
	}
}

func TestStatusBarFitsNarrowPane(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{}
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 19, Window: 100}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		a.paneStatusLine(c, &tab, node, "/a/very/long/project/directory/meter", false, p)
	}, 320, 80)
	driver.Frame()
	model, mok := driver.Find("Choose model and thinking")
	dir, dok := driver.Find("Change directory")
	meter, uok := driver.Find("Context usage")
	if !mok || !dok || !uok {
		t.Fatalf("missing status controls: %v %v %v", mok, dok, uok)
	}
	if dir.X+dir.W > model.X+0.5 || model.X+model.W > meter.X+0.5 || meter.X+meter.W > 320 {
		t.Fatalf("overlapping controls: model=%+v directory=%+v meter=%+v", model, dir, meter)
	}
	if driver.HasText("idle") || driver.HasText("ctx 19%") {
		t.Fatal("status bar should not show status or numeric usage")
	}
}

func TestContextMeterOpensConversationContext(t *testing.T) {
	id := "42"
	title := "Context details"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{connected: true}
	p.state.Current.Status = protocol.Idle
	estimated := true
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 38000, Window: 200000, Estimated: &estimated, Breakdown: &protocol.ContextBreakdown{
		SystemPrompt: 5000, ProjectInstructions: 3000, Skills: 1000, ToolDefinitions: 4000,
		UserMessages: 7000, AssistantMessages: 10000, ToolResults: 8000,
	}}
	a := &app{
		panes:      map[uuid.UUID]*pane{node.Id: p},
		catalog:    protocol.Catalog{Conversations: []protocol.Conversation{{Id: id, Cwd: "/project", Model: "provider/model", Title: &title}}},
		clientData: "/data/meter",
	}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.paneStatusLine(c, &tab, node, "/project", false, p)
		a.dialogs(c)
	}, 800, 600)
	driver.Frame()
	if err := driver.Click("Context usage"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	for _, value := range []string{"Context", "38,000 / 200,000 tokens", "19%", "Context breakdown", "System prompt", "Project instructions", "Skills", "Tool definitions", "User messages", "Assistant messages", "Tool results", "Free space", "Session", "Conversation ID", id, "/project", "provider/model", "Storage", "/data/meter", "/data/meter/durable.sqlite", "/data/meter/app.sqlite"} {
		if !driver.HasText(value) {
			t.Fatalf("missing %q from context screen: %v", value, driver.Texts())
		}
	}
	if driver.HasText(title) {
		t.Fatal("conversation title should not be rendered as modal copy")
	}
	if driver.HasText("Close") || driver.HasText("Switch conversation…") {
		t.Fatal("context modal should not have footer actions")
	}
}

func TestContextBreakdownIsAbsentWithoutData(t *testing.T) {
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		contextBreakdown(c, &protocol.ContextUsage{Tokens: 10, Window: 100})
	}, 500, 200)
	driver.Frame()
	if driver.HasText("Context breakdown") {
		t.Fatal("empty context breakdown should not render a section")
	}
}

func TestStatusPathUsesAvailableWidthAndContextTooltip(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	p := &pane{}
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 38000, Window: 200000}
	a := &app{}
	path := "/Users/miles/projects/meter"
	driver := ui.NewTester(func(c *ui.Context) {
		a.paneStatusLine(c, &tab, node, path, false, p)
	}, 900, 100)
	driver.Frame()
	if !driver.HasText(path) {
		t.Fatalf("path shortened before layout: %v", driver.Texts())
	}
	meter, ok := driver.Find("Context usage")
	if !ok {
		t.Fatal("context meter missing")
	}
	driver.Move(meter.X+meter.W/2, meter.Y+meter.H/2)
	driver.Advance(time.Second)
	if !driver.HasText("Context: 38,000/200,000 (19%)") {
		t.Fatalf("missing context details: %v", driver.Texts())
	}
}

func TestSinglePaneShowsTitle(t *testing.T) {
	node := emptyPane()
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	a := &app{selectedPane: node.Id}
	driver := ui.NewTester(func(c *ui.Context) {
		a.paneHeader(c, &tab, node, "Conversation title")
	}, 500, 80)
	driver.Frame()
	if !driver.HasText("Conversation title") || driver.HasText("#1") {
		t.Fatalf("expected an unnumbered single-pane title: %v", driver.Texts())
	}
}

func TestSingleConversationPaneTitleVisibleInSurface(t *testing.T) {
	id, title := "conversation", "Visible conversation pane title"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id, Name: &title}
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	a := &app{loaded: true, panes: map[uuid.UUID]*pane{node.Id: {}}}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Column(c).Fill().Children(func() { a.surface(c, protocol.Project{}, &tab) })
	}, 850, 550)
	driver.Frame()
	if bounds, ok := driver.Find(title); !ok || bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("pane title is missing or clipped: %+v, %v", bounds, ok)
	}
}

func TestThinkingToggleIsGlobalAndImmediate(t *testing.T) {
	id := "1"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.paneHeader(c, &tab, node, "Chat") }, 500, 80)
	driver.Frame()
	bulb, ok := driver.Find("Hide thinking traces")
	if !ok {
		t.Fatal("thinking toggle missing")
	}
	closeBefore, ok := driver.Find("Close pane")
	if !ok {
		t.Fatal("close control must be visible without hover")
	}
	driver.Move(bulb.X+bulb.W/2, bulb.Y+bulb.H/2)
	driver.Frame()
	bulbAfter, ok := driver.Find("Hide thinking traces")
	if !ok || bulbAfter.X != bulb.X || bulbAfter.W != bulb.W {
		t.Fatalf("thinking toggle moved on hover: %v -> %v", bulb, bulbAfter)
	}
	closeAfter, ok := driver.Find("Close pane")
	if !ok || closeAfter.X != closeBefore.X || closeAfter.X < bulbAfter.X+bulbAfter.W {
		t.Fatalf("close control shifted or overlaps toggle: %v -> %v", closeBefore, closeAfter)
	}
	if err := driver.Click("Hide thinking traces"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	if !a.hideThinking {
		t.Fatal("thinking must be hidden immediately")
	}
	driver.Frame()
	if err := driver.Click("Show thinking traces"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	if a.hideThinking {
		t.Fatal("thinking must be shown again")
	}
}

func TestSingleConversationTitleDoesNotMoveOnHover(t *testing.T) {
	id := "conversation"
	title := "A long conversation title that needs to fit a narrow pane"
	node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{node})
	a := &app{selectedPane: node.Id}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.paneHeader(c, &tab, node, title)
	}, 240, 80)
	driver.Frame()
	before, ok := driver.Find(title)
	if !ok {
		t.Fatal("title missing")
	}
	driver.Move(before.X+4, before.Y+4)
	driver.Frame()
	closeButton, ok := driver.Find("Close pane")
	if !ok {
		t.Fatal("close button missing")
	}
	after, ok := driver.Find(title)
	if !ok || before != after || after.X+after.W > closeButton.X {
		t.Fatalf("title before hover: %+v, after hover: %+v, close button: %+v", before, after, closeButton)
	}
}

func TestPaneHeaderWidthAndCloseVisibility(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "multiple"}[multiple], func(t *testing.T) {
			id := "conversation"
			node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &id}
			tab := protocol.LayoutTab{Id: uuid.New()}
			panes := []protocol.LayoutNode{node}
			if multiple {
				panes = append(panes, emptyPane())
			}
			setPanes(&tab, panes)
			a := &app{selectedPane: node.Id}
			driver := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				a.paneHeader(c, &tab, node, "Title")
			}, 1200, 80)
			driver.Frame()
			title, ok := driver.Find("Title")
			if !ok {
				t.Fatal("title missing")
			}
			closeButton, ok := driver.Find("Close pane")
			if !ok {
				t.Fatal("close button must be visible without hover")
			}
			bulb, ok := driver.Find("Hide thinking traces")
			if !ok || bulb.X+bulb.W > closeButton.X {
				t.Fatalf("thinking and close controls overlap or are missing: %+v %+v", bulb, closeButton)
			}
			if !multiple && (title.X > 100 || closeButton.X < 1100) {
				t.Fatalf("single-pane header does not use available width: title=%+v close=%+v", title, closeButton)
			}
			if multiple && (title.X < 200 || closeButton.X > 1000) {
				t.Fatalf("multi-pane header should retain centered content width: title=%+v close=%+v", title, closeButton)
			}
		})
	}
}
