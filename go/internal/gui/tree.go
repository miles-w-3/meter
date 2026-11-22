package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Cyenoch/mygoicons/lucide"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

// treePreview is deliberately read-only: moving a pane to a historical
// position needs branch-aware, retry-safe submission and is not implemented yet.
type treePreview struct {
	pane      uuid.UUID
	loading   bool
	err       string
	notice    string
	query     string
	selected  string
	collapsed map[string]bool
	rows      []treePreviewRow
}

type treePreviewRow struct {
	id, label string
	// conversation identifies a path containing entryID. entryParent preserves
	// the physical parent, including bookkeeping entries hidden in the preview.
	conversation, entryID, entryParent string
	kind                               string
	depth                              int
	header                             bool
	current                            bool
	parent                             string
	rails                              []bool
	branchStart                        bool
	lastBranch                         bool
	hasNext                            bool
	fork                               bool
}

type treeHistoryClient interface {
	HistoryTree(context.Context, string) (protocol.HistoryTree, error)
	History(context.Context, string, *string) (protocol.HistoryPage, error)
}

func (a *app) treeEscape(now time.Time) {
	if a.tree != nil && a.tree.pane == a.selectedPane {
		a.tree = nil
		a.lastTreeEscape = time.Time{}
		return
	}
	p := a.panes[a.selectedPane]
	if a.busy() || a.tree != nil || a.paletteOpen || a.backendOpen || a.projectForm || a.tabEditor != nil || a.cwdEditor != nil || a.nameEditor != nil || a.closePaneConfirm != nil || a.closeTabConfirm != nil || a.dictation != nil || p == nil || !p.connected || p.conversation == "" {
		a.lastTreeEscape = time.Time{}
		return
	}
	if a.lastTreeEscape.IsZero() || now.Sub(a.lastTreeEscape) < 0 || now.Sub(a.lastTreeEscape) >= 500*time.Millisecond {
		a.lastTreeEscape = now
		return
	}
	a.lastTreeEscape = time.Time{}
	a.openTree(a.selectedPane, p)
}

func (a *app) openTree(id uuid.UUID, p *pane) {
	preview := &treePreview{pane: id, loading: true, collapsed: make(map[string]bool)}
	a.tree = preview
	if a.api == nil || a.ctx == nil || a.update == nil {
		preview.loading, preview.err = false, "Conversation tree is unavailable."
		return
	}
	conversation := p.conversation
	go func() {
		rows, err := loadTreePreview(a.ctx, a.api, conversation)
		a.update(func() {
			if a.tree != preview {
				return
			}
			preview.loading = false
			if err != nil {
				preview.err = err.Error()
				return
			}
			preview.rows = rows
			for _, row := range rows {
				if row.current {
					preview.selected = row.id
					break
				}
			}
			for _, tab := range a.layout.Tabs {
				if i := nodeIndex(&tab, id); i >= 0 && tab.Nodes[i].TreePosition != nil {
					entryID := tab.Nodes[i].TreePosition.EntryId
					for _, row := range rows {
						if row.entryID == entryID {
							preview.selected = row.id
							break
						}
					}
					break
				}
			}
		})
	}()
}

// loadPinnedHistory keeps the fixed transcript separate from the attached live stream.
func (a *app) loadPinnedHistory(p *pane, position protocol.TreePosition) {
	key := position.ConversationId + ":" + position.EntryId
	if p.historyKey == key {
		return
	}
	p.historyKey, p.historyError, p.historyLoading = key, "", true
	p.historical = client.State{}
	p.transcriptImages = nil
	if a.api == nil || a.ctx == nil || a.update == nil {
		p.historyLoading, p.historyError = false, "Historical transcript is unavailable."
		return
	}
	go func() {
		page, err := a.api.HistoricalPath(a.ctx, position.ConversationId, position.EntryId, nil)
		a.update(func() {
			if p.historyKey != key {
				return
			}
			p.historyLoading = false
			if err != nil {
				p.historyError = err.Error()
				return
			}
			view := protocol.ConversationView{ConversationId: position.ConversationId, Status: protocol.Idle, Entries: page.Entries, Before: page.Before}
			p.historical.Apply(client.Event{View: &view, Reset: true})
			p.list.ScrollToEnd()
		})
	}()
}

func (a *app) olderPinnedHistory(p *pane, position protocol.TreePosition) {
	cursor := p.historical.Before
	if p.historyLoading || cursor == nil || a.api == nil {
		return
	}
	key := p.historyKey
	p.historyLoading = true
	go func() {
		page, err := a.api.HistoricalPath(a.ctx, position.ConversationId, position.EntryId, cursor)
		a.update(func() {
			if p.historyKey != key {
				return
			}
			p.historyLoading = false
			if err != nil {
				p.historyError = err.Error()
				return
			}
			p.historical.Prepend(cursor, protocol.HistoryPage{Entries: page.Entries, Before: page.Before})
		})
	}()
}

// beginFork persists the request before transmitting it so a restart can replay it.
func (a *app) beginFork(tabID uuid.UUID, node protocol.LayoutNode, p *pane) {
	if a.busy() || !p.connected || node.TreePosition == nil || node.PendingFork != nil || p.submission.Pending() || (strings.TrimSpace(p.draft) == "" && len(p.images) == 0) {
		return
	}
	pending := protocol.PendingFork{RequestId: uuid.NewString(), Content: p.draft}
	if len(p.images) > 0 {
		images := make([]protocol.ImageAttachment, len(p.images))
		for i, image := range p.images {
			images[i] = image.attachment
		}
		pending.Images = &images
	}
	p.notice = "Saving branch request…"
	a.saveThen(func(layout *protocol.Layout) {
		tab := &layout.Tabs[tabIndex(layout, tabID)]
		tab.Nodes[nodeIndex(tab, node.Id)].PendingFork = &pending
	}, func() {
		p.draft, p.images = "", nil
	})
}

func (a *app) resumeFork(tabID uuid.UUID, node protocol.LayoutNode, p *pane) {
	if node.PendingFork == nil || node.TreePosition == nil || node.ConversationId == nil || p.forkInFlight || p.forkReceipt != nil || a.api == nil || a.ctx == nil || a.update == nil {
		return
	}
	pending, position, attached := *node.PendingFork, *node.TreePosition, *node.ConversationId
	p.forkInFlight, p.notice = true, "Sending branch…"
	go func() {
		receipt, err := a.api.ForkAndSubmit(a.ctx, attached, protocol.ForkInput{ParentConversationId: position.ConversationId, At: position.EntryId, RequestId: pending.RequestId, Content: pending.Content, Images: pending.Images})
		a.update(func() {
			if a.panes[node.Id] != p {
				return
			}
			p.forkInFlight = false
			if err != nil {
				p.notice = "Branch send not confirmed: " + err.Error()
				return
			}
			p.forkReceipt = &receipt
			a.finishFork(tabID, node.Id, pending.RequestId, p)
		})
	}()
}

func (a *app) finishFork(tabID, paneID uuid.UUID, requestID string, p *pane) {
	if p.forkReceipt == nil || a.busy() {
		return
	}
	idx := tabIndex(&a.layout, tabID)
	if idx < 0 {
		return
	}
	ni := nodeIndex(&a.layout.Tabs[idx], paneID)
	if ni < 0 || a.layout.Tabs[idx].Nodes[ni].PendingFork == nil || a.layout.Tabs[idx].Nodes[ni].PendingFork.RequestId != requestID {
		return
	}
	id := p.forkReceipt.ConversationId
	p.notice = "Switching to branch…"
	a.saveThen(func(layout *protocol.Layout) {
		tab := &layout.Tabs[tabIndex(layout, tabID)]
		node := &tab.Nodes[nodeIndex(tab, paneID)]
		node.ConversationId, node.TreePosition, node.PendingFork = &id, nil, nil
	}, func() {
		p.forkReceipt, p.notice = nil, ""
	})
}

// A conversation stores a linear segment. Link each segment to its fork
// entry, then suppress bookkeeping entries only after the links are built.
type treeMessage struct {
	entry        protocol.Entry
	parent       string
	conversation string
}

func loadTreePreview(ctx context.Context, api treeHistoryClient, conversation string) ([]treePreviewRow, error) {
	tree, err := api.HistoryTree(ctx, conversation)
	if err != nil {
		return nil, err
	}
	messages := make(map[string]treeMessage)
	order := make([]string, 0)
	forks := make(map[string]bool)
	current := ""
	for _, branch := range tree.Branches {
		parent := ""
		if branch.At != nil {
			parent = *branch.At
			forks[parent] = true
		}
		var entries []protocol.Entry
		var before *string
		cursors := make(map[string]bool)
		for {
			page, err := api.History(ctx, branch.ConversationId, before)
			if err != nil {
				return nil, err
			}
			entries = append(page.Entries, entries...)
			if len(entries) > 10000 {
				return nil, fmt.Errorf("conversation is too large for the tree preview")
			}
			if page.Before == nil {
				break
			}
			if cursors[*page.Before] {
				return nil, fmt.Errorf("history cursor did not advance")
			}
			cursors[*page.Before] = true
			before = page.Before
		}
		for _, entry := range entries {
			if branch.ConversationId == conversation && treeEntryVisible(entry) {
				current = entry.Id
			}
			if _, exists := messages[entry.Id]; exists {
				continue
			}
			messages[entry.Id] = treeMessage{entry: entry, parent: parent, conversation: branch.ConversationId}
			order = append(order, entry.Id)
			parent = entry.Id
		}
	}
	// Skip non-display entries without losing their descendants or fork points.
	visibleParent := func(id string) string {
		for steps := 0; id != "" && steps <= len(messages); steps++ {
			node, ok := messages[id]
			if !ok {
				return ""
			}
			if treeEntryVisible(node.entry) {
				return id
			}
			id = node.parent
		}
		return ""
	}
	children := make(map[string][]string)
	for _, id := range order {
		node := messages[id]
		if treeEntryVisible(node.entry) {
			parent := visibleParent(node.parent)
			children[parent] = append(children[parent], id)
		}
	}
	visibleForks := make(map[string]bool)
	for id := range forks {
		visibleForks[visibleParent(id)] = true
	}
	type item struct {
		id                      string
		depth                   int
		marker                  bool
		rails                   []bool
		branchStart, lastBranch bool
	}
	var pending []item
	roots := children[""]
	for i := len(roots) - 1; i >= 0; i-- {
		pending = append(pending, item{id: roots[i]})
	}
	var rows []treePreviewRow
	for len(pending) > 0 {
		last := len(pending) - 1
		next := pending[last]
		pending = pending[:last]
		if next.marker {
			rows = append(rows, treePreviewRow{id: "fork:" + next.id, label: "Branches", depth: next.depth, header: true, rails: next.rails, hasNext: len(children[next.id]) > 0})
			continue
		}
		entry := messages[next.id].entry
		kind := strings.TrimPrefix(entry.Kind, "pi.")
		if entry.ToolName != nil {
			kind = *entry.ToolName
		}
		body := strings.TrimSpace(clean(entry.Text))
		if body == "" && entry.ImageCount != nil && *entry.ImageCount > 0 {
			body = "[image]"
		}
		kids := children[next.id]
		parent := visibleParent(messages[next.id].parent)
		rows = append(rows, treePreviewRow{id: "entry:" + entry.Id, entryID: entry.Id, entryParent: messages[next.id].parent, conversation: messages[next.id].conversation, label: kind + " · " + firstLine(body), kind: kind, depth: next.depth, current: entry.Id == current, parent: parent, rails: next.rails, branchStart: next.branchStart, lastBranch: next.lastBranch, hasNext: len(kids) > 0 || visibleForks[next.id], fork: visibleForks[next.id]})
		for i := len(kids) - 1; i >= 0; i-- {
			child := item{id: kids[i], depth: next.depth, rails: next.rails}
			if visibleForks[next.id] {
				child.depth++
				child.branchStart, child.lastBranch = true, i == len(kids)-1
				child.rails = append(append([]bool(nil), next.rails...), !child.lastBranch)
			}
			pending = append(pending, child)
		}
		if visibleForks[next.id] {
			pending = append(pending, item{id: next.id, depth: next.depth, marker: true, rails: next.rails})
		}
	}
	return rows, nil
}

func treeEntryVisible(entry protocol.Entry) bool {
	return entry.Text != "" || entry.ToolName != nil || (entry.ImageCount != nil && *entry.ImageCount > 0)
}

// visibleTreeRows keeps fork markers while filtering message text. Folding a
// fork hides its descendants without changing history.
func visibleTreeRows(preview *treePreview) []treePreviewRow {
	query := strings.ToLower(strings.TrimSpace(preview.query))
	keep := make(map[string]bool)
	if query != "" {
		byID := make(map[string]treePreviewRow)
		for _, row := range preview.rows {
			byID[row.id] = row
		}
		for _, row := range preview.rows {
			if row.header || !strings.Contains(strings.ToLower(row.label), query) {
				continue
			}
			for id := row.id; id != ""; {
				if keep[id] {
					break
				}
				keep[id] = true
				node, ok := byID[id]
				if !ok || node.parent == "" {
					break
				}
				keep["fork:"+node.parent] = true
				id = "entry:" + node.parent
			}
		}
	}
	var rows []treePreviewRow
	foldedDepth := -1
	for _, row := range preview.rows {
		if query != "" {
			if !row.header && keep[row.id] {
				rows = append(rows, row)
			}
			continue
		}
		if foldedDepth >= 0 && row.depth > foldedDepth {
			continue
		}
		foldedDepth = -1
		if row.header {
			if preview.collapsed[row.id] {
				foldedDepth = row.depth
			}
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

var treeForkColor = ui.Hex("#b79ad6")
var treeExpandedIcon = lucide.ChevronDown()
var treeCollapsedIcon = lucide.ChevronRight()
var treeLineColor = ui.RGBA(142, 159, 146, .38)

// Each row paints its piece of the gutter so the lines stay continuous while
// scrolling; ancestor rails remain visible until their last sibling branches.
func treeGutter(c *ui.Context, row treePreviewRow, preview *treePreview) {
	const step = float32(20)
	depth := min(row.depth, 12)
	gutter := ui.Box(c).Width(float32(depth+1) * step).Height(32).Shrink(0)
	gutter.Draw(func(p *ui.Painter, r ui.Rect) {
		mid := r.Y + r.H/2
		for i := 0; i < depth && i < len(row.rails); i++ {
			if row.rails[i] {
				x := r.X + step/2 + float32(i)*step
				p.Line(x, r.Y, x, r.Y+r.H, 1, treeLineColor)
			}
		}
		x := r.X + step/2 + float32(depth)*step
		if row.branchStart && depth > 0 {
			parent := x - step
			if row.lastBranch {
				p.Line(parent, r.Y, parent, mid, 1, treeLineColor)
			}
			p.Line(parent, mid, x, mid, 1, treeForkColor)
		} else {
			p.Line(x, r.Y, x, mid, 1, treeLineColor)
		}
		if !row.header {
			p.Line(x, mid, r.X+r.W, mid, 1, treeLineColor)
		}
		if row.hasNext {
			color := treeLineColor
			if row.fork {
				color = treeForkColor
			}
			p.Line(x, mid, x, r.Y+r.H, 1, color)
		}
	})
	if row.fork {
		forkID := "fork:" + strings.TrimPrefix(row.id, "entry:")
		collapsed := preview.collapsed[forkID] && strings.TrimSpace(preview.query) == ""
		gutter.Children(func() {
			button := ui.ButtonBase(c.Key(forkID)).Absolute().Left(float32(depth)*step+2).Top(8).Size(16, 16).
				Radius(4).Background(contentSurface).Center().Label("Toggle branches " + forkID).Tooltip("Expand or collapse branches")
			button.Children(func() {
				icon := treeExpandedIcon
				if collapsed {
					icon = treeCollapsedIcon
				}
				ui.Icon(c, icon).Size(14, 14).TextColor(treeForkColor)
			})
			if button.Clicked() {
				preview.collapsed[forkID] = !preview.collapsed[forkID]
			}
		})
	}
}

func (a *app) treeNavigate(preview *treePreview, newTab bool) {
	p := a.panes[preview.pane]
	if p == nil || !p.connected || p.submission.Pending() || p.queuePending != "" || a.busy() {
		preview.notice = "Wait for the current submission to finish before navigating."
		return
	}
	var selected treePreviewRow
	for _, row := range preview.rows {
		if row.id == preview.selected && !row.header {
			selected = row
			break
		}
	}
	if selected.entryID == "" {
		return
	}
	resumeCurrent := selected.current && !newTab
	at := selected.entryID
	if selected.kind == "user" && !resumeCurrent {
		if selected.entryParent == "" {
			preview.notice = "Cannot navigate before the first entry."
			return
		}
		if !newTab && (strings.TrimSpace(p.draft) != "" || len(p.images) > 0) {
			preview.notice = "Keep or clear the existing draft before editing this message."
			return
		}
		at = selected.entryParent
	}
	var sourceTab *protocol.LayoutTab
	for i := range a.layout.Tabs {
		if nodeIndex(&a.layout.Tabs[i], preview.pane) >= 0 {
			sourceTab = &a.layout.Tabs[i]
			break
		}
	}
	if sourceTab == nil || (newTab && (sourceTab.ProjectId == nil || len(a.layout.Tabs) >= 24)) {
		preview.notice = "Cannot create a tab here."
		return
	}
	current := sourceTab.Nodes[nodeIndex(sourceTab, preview.pane)]
	if current.PendingFork != nil {
		preview.notice = "Resolve the pending branch send before navigating."
		return
	}
	tabID, paneID, source := sourceTab.Id, preview.pane, p.conversation
	if resumeCurrent {
		if current.TreePosition == nil {
			a.tree = nil
			return
		}
		a.saveThen(func(layout *protocol.Layout) {
			tab := &layout.Tabs[tabIndex(layout, tabID)]
			node := &tab.Nodes[nodeIndex(tab, paneID)]
			node.TreePosition = nil
			node.ResumeEntryId = &selected.entryID
		}, func() { a.tree = nil })
		return
	}
	if newTab {
		paneID = uuid.New()
	}
	apply := func(message *protocol.UserMessage) {
		if a.tree != preview || a.panes[preview.pane] != p || a.busy() {
			return
		}
		if message != nil && !newTab && (p.draft != "" || len(p.images) != 0) {
			preview.notice = "Keep or clear the existing draft before editing this message."
			return
		}
		position := protocol.TreePosition{ConversationId: selected.conversation, EntryId: at}
		a.saveThen(func(layout *protocol.Layout) {
			if newTab {
				project := *sourceTab.ProjectId
				addTab(layout, project, suggestTabName(*layout, project))
				tab := &layout.Tabs[len(layout.Tabs)-1]
				node := protocol.LayoutNode{Id: paneID, ConversationId: &source, TreePosition: &position}
				setPanes(tab, []protocol.LayoutNode{node})
				layout.ActiveTab = &tab.Id
			} else {
				tab := &layout.Tabs[tabIndex(layout, tabID)]
				tab.Nodes[nodeIndex(tab, paneID)].TreePosition = &position
			}
		}, func() {
			a.tree = nil
			a.selectedPane = paneID
			if message != nil {
				if target := a.panes[paneID]; target != nil {
					target.restoreDraft(message.Content, &message.Images)
				}
			}
		})
	}
	if selected.kind != "user" {
		apply(nil)
		return
	}
	if a.api == nil || a.ctx == nil || a.update == nil {
		preview.notice = "Message is unavailable."
		return
	}
	preview.loading = true
	go func() {
		message, err := a.api.UserMessage(a.ctx, selected.conversation, selected.entryID)
		a.update(func() {
			if a.tree != preview {
				return
			}
			preview.loading = false
			if err != nil {
				preview.notice = err.Error()
				return
			}
			apply(&message)
		})
	}()
}

func (a *app) treePane(c *ui.Context, preview *treePreview) {
	if !preview.loading && preview.err == "" {
		rows := visibleTreeRows(preview)
		selectable := make([]string, 0, len(rows))
		for _, row := range rows {
			if !row.header {
				selectable = append(selectable, row.id)
			}
		}
		index := -1
		for i, id := range selectable {
			if id == preview.selected {
				index = i
				break
			}
		}
		if len(selectable) > 0 && c.Shortcut(0, ui.KeyDown) {
			preview.selected = selectable[min(index+1, len(selectable)-1)]
		} else if len(selectable) > 0 && c.Shortcut(0, ui.KeyUp) {
			preview.selected = selectable[max(index-1, 0)]
		} else if preview.selected != "" && c.Shortcut(0, ui.KeyEnter) {
			a.treeNavigate(preview, false)
		}
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Padding(16, 16, 12).Gap(10).AlignItems(ui.Center).Children(func() {
			text(c, "◈", 19, accent)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				text(c, "Conversation tree", 16, ink).FontWeight(650)
				text(c, "Select a point", 12, muted).NoWrap().Ellipsis("…")
			})
			if action(c, "Close tree", false) {
				a.tree = nil
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(theme.Border)
		if preview.loading {
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				text(c, "◌", 24, accent)
				text(c, "Loading conversation history…", 13, muted)
			})
			return
		}
		if preview.err != "" {
			ui.Column(c).Grow(1).Padding(16).Gap(8).Children(func() {
				text(c, "Could not load the tree", 14, warning).FontWeight(600)
				text(c, clean(preview.err), 13, muted)
			})
			return
		}
		ui.Column(c).FillWidth().Padding(12, 16, 8).Gap(6).Children(func() {
			ui.TextInput(c, &preview.query).Label("Search tree").Placeholder("Filter messages…").FillWidth()
			ui.Row(c).Gap(8).Children(func() {
				if action(c, "Jump to selection", preview.selected == "" || a.busy()) {
					a.treeNavigate(preview, false)
				}
				if action(c, "Jump in new tab", preview.selected == "" || a.busy()) {
					a.treeNavigate(preview, true)
				}
			})
			if preview.notice != "" {
				text(c, clean(preview.notice), 12, warning)
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
			ui.Column(c).FillWidth().Padding(4, 12, 18).Children(func() {
				for _, row := range visibleTreeRows(preview) {
					if row.fork && preview.collapsed["fork:"+strings.TrimPrefix(row.id, "entry:")] && strings.TrimSpace(preview.query) == "" {
						row.hasNext = false
					}
					color, marker := rain, "●"
					if row.kind == "user" {
						color, marker = accent, "●"
					} else if row.kind != "assistant" {
						color, marker = runningColor, "◆"
					}
					line := ui.Row(c.Key(row.id)).FillWidth().Height(32).Gap(7).AlignItems(ui.Center).Radius(6)
					if row.id == preview.selected {
						line.Background(selectedFill)
					}
					line.Children(func() {
						treeGutter(c, row, preview)
						if row.header {
							text(c, row.label, 13, muted)
							return
						}
						button := ui.ButtonBase(c.Key("select:" + row.id)).Label("Select " + row.id).Grow(1).MinWidth(0).Height(32)
						button.Children(func() {
							ui.Row(c).Fill().Gap(7).AlignItems(ui.Center).Children(func() {
								text(c, marker, 10, color)
								text(c, row.label, 13, ink).NoWrap().Ellipsis("…").Grow(1).MinWidth(0)
								if row.current {
									text(c, "CURRENT", 10, accent).Font(monoFont).Padding(0, 8, 0, 0)
								}
							})
						})
						if button.Clicked() {
							preview.selected, preview.notice = row.id, ""
						}
					})
				}
			})
		})
	})
}
