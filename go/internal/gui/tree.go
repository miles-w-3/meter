package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Cyenoch/mygoicons/lucide"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

// treePreview is deliberately read-only: moving a pane to a historical
// position needs branch-aware, retry-safe submission and is not implemented yet.
type treePreview struct {
	pane      uuid.UUID
	loading   bool
	err       string
	query     string
	collapsed map[string]bool
	rows      []treePreviewRow
}

type treePreviewRow struct {
	id, label   string
	kind        string
	depth       int
	header      bool
	current     bool
	parent      string
	rails       []bool
	branchStart bool
	lastBranch  bool
	hasNext     bool
	fork        bool
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
	if a.busy() || a.tree != nil || a.paletteOpen || a.backendOpen || a.projectForm || a.tabEditor != nil || a.cwdEditor != nil || a.nameEditor != nil || a.closePaneConfirm != nil || a.closeTabConfirm != nil || a.dictation != nil || p == nil || !p.connected || p.conversation == "" || strings.TrimSpace(p.draft) != "" || len(p.images) != 0 {
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
		})
	}()
}

// A conversation stores a linear segment. Link each segment to its fork
// entry, then suppress bookkeeping entries only after the links are built.
type treeMessage struct {
	entry  protocol.Entry
	parent string
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
			messages[entry.Id] = treeMessage{entry: entry, parent: parent}
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
		rows = append(rows, treePreviewRow{id: "entry:" + entry.Id, label: kind + " · " + firstLine(body), kind: kind, depth: next.depth, current: entry.Id == current, parent: parent, rails: next.rails, branchStart: next.branchStart, lastBranch: next.lastBranch, hasNext: len(kids) > 0 || visibleForks[next.id], fork: visibleForks[next.id]})
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

func (a *app) treePane(c *ui.Context, preview *treePreview) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Padding(16, 16, 12).Gap(10).AlignItems(ui.Center).Children(func() {
			text(c, "◈", 19, accent)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				text(c, "Conversation tree", 16, ink).FontWeight(650)
				text(c, "Read-only preview · navigation coming soon", 12, muted).NoWrap().Ellipsis("…")
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
		ui.Column(c).FillWidth().Padding(12, 16, 8).Children(func() {
			ui.TextInput(c, &preview.query).Label("Search tree").Placeholder("Filter messages…").FillWidth()
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
					if row.current {
						line.Background(selectedFill)
					}
					line.Children(func() {
						treeGutter(c, row, preview)
						text(c, marker, 10, color)
						text(c, row.label, 13, ink).NoWrap().Ellipsis("…").Grow(1).MinWidth(0)
						if row.current {
							text(c, "CURRENT", 10, accent).Font(monoFont).Padding(0, 8, 0, 0)
						}
					})
				}
			})
		})
	})
}
