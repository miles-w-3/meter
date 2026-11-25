package gui

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

type layoutDrag struct {
	tab, pane uuid.UUID // A nil pane identifies the whole tab.
}

func sameProject(a, b protocol.LayoutTab) bool {
	return a.ProjectId != nil && b.ProjectId != nil && *a.ProjectId == *b.ProjectId
}

func (a *app) hasOtherProjectTab(tab protocol.LayoutTab) bool {
	return slices.ContainsFunc(a.layout.Tabs, func(other protocol.LayoutTab) bool {
		return other.Id != tab.Id && sameProject(tab, other)
	})
}

func (a *app) canMovePaneToAnotherTab(tab *protocol.LayoutTab, pane protocol.LayoutNode) bool {
	if pane.PendingFork != nil {
		return false
	}
	return slices.ContainsFunc(a.layout.Tabs, func(other protocol.LayoutTab) bool {
		return other.Id != tab.Id && sameProject(*tab, other) && len(orderedPanes(&other)) < maxPanes
	})
}

func reorderedTabs(layout *protocol.Layout, source, target uuid.UUID, after bool) []protocol.LayoutTab {
	from, to := tabIndex(layout, source), tabIndex(layout, target)
	if from < 0 || to < 0 || from == to || !sameProject(layout.Tabs[from], layout.Tabs[to]) {
		return nil
	}
	var indexes []int
	var tabs []protocol.LayoutTab
	for i, tab := range layout.Tabs {
		if sameProject(tab, layout.Tabs[from]) {
			indexes = append(indexes, i)
			tabs = append(tabs, tab)
		}
	}
	from, to = slices.Index(indexes, from), slices.Index(indexes, to)
	if after {
		to++
	}
	if to > from {
		to--
	}
	if to == from {
		return nil
	}
	moved := tabs[from]
	tabs = slices.Insert(slices.Delete(tabs, from, from+1), to, moved)
	next := slices.Clone(layout.Tabs)
	for i, index := range indexes {
		next[index] = tabs[i]
	}
	return next
}

func (a *app) tabDropTarget(target ui.Element, tab protocol.LayoutTab) bool {
	if a.busy() {
		return false
	}
	drag, over := ui.DragOver[layoutDrag](target)
	if dropped, ok := ui.Drop[layoutDrag](target); ok {
		drag = dropped
	} else if !over {
		return false
	}
	if drag.pane != uuid.Nil {
		if node, ok := a.transferablePane(drag, tab); ok {
			if over {
				target.DrawOver(func(p *ui.Painter, b ui.Rect) {
					p.Stroke(b, accent, controlRadius, 2)
				})
			} else {
				a.transferPane(drag, tab.Id, node)
			}
		}
		return over
	}
	after := rightHalf(target)
	if reorderedTabs(&a.layout, drag.tab, tab.Id, after) != nil {
		if over {
			drawInsertion(target, after)
		} else {
			a.save(func(layout *protocol.Layout) {
				layout.Tabs = reorderedTabs(layout, drag.tab, tab.Id, after)
			})
		}
	}
	return over
}

func (a *app) transferablePane(drag layoutDrag, target protocol.LayoutTab) (protocol.LayoutNode, bool) {
	i := tabIndex(&a.layout, drag.tab)
	if i < 0 || drag.tab == target.Id || !sameProject(a.layout.Tabs[i], target) || len(orderedPanes(&target)) >= maxPanes {
		return protocol.LayoutNode{}, false
	}
	for _, pane := range orderedPanes(&a.layout.Tabs[i]) {
		if pane.Id == drag.pane && pane.PendingFork == nil {
			return pane, true
		}
	}
	return protocol.LayoutNode{}, false
}

func (a *app) transferPane(drag layoutDrag, target uuid.UUID, pane protocol.LayoutNode) {
	a.saveThen(func(layout *protocol.Layout) {
		from := &layout.Tabs[tabIndex(layout, drag.tab)]
		to := &layout.Tabs[tabIndex(layout, target)]
		closePane(from, pane.Id)
		setPanes(to, append(orderedPanes(to), pane))
		layout.ActiveTab = &target
	}, func() {
		a.tabEditor, a.cwdEditor = nil, nil
		a.paneScroll.X = 0
		a.revealPane = pane.Id
	})
}

func (a *app) paneDropTarget(target ui.Element, tab *protocol.LayoutTab, panes []protocol.LayoutNode, slot int) bool {
	if a.busy() || len(panes) < 2 {
		return false
	}
	drag, over := ui.DragOver[layoutDrag](target)
	if dropped, ok := ui.Drop[layoutDrag](target); ok {
		drag = dropped
	} else if !over {
		return false
	}
	if drag.pane == uuid.Nil || drag.tab != tab.Id {
		return false
	}
	after := rightHalf(target)
	if after {
		slot++
	}
	if over {
		if !sameOrder(panes, reorder(panes, drag.pane, slot)) {
			drawInsertion(target, after)
		}
	} else {
		a.reorderPane(tab.Id, panes, drag.pane, slot)
	}
	return over
}

func (a *app) reorderPane(tab uuid.UUID, panes []protocol.LayoutNode, pane uuid.UUID, slot int) {
	if moved := reorder(panes, pane, slot); !sameOrder(panes, moved) {
		a.save(func(layout *protocol.Layout) { setPanes(&layout.Tabs[tabIndex(layout, tab)], moved) })
	}
}

func rightHalf(target ui.Element) bool {
	x, _, _ := target.PointerPosition()
	return x >= target.Bounds().W/2
}

func drawInsertion(target ui.Element, after bool) {
	target.DrawOver(func(p *ui.Painter, b ui.Rect) {
		x := b.X
		if after {
			x += b.W - 2
		}
		p.Fill(ui.Rect{X: x, Y: b.Y + 2, W: 2, H: max(0, b.H-4)}, accent, 1)
	})
}

func scrollDragEdge(c *ui.Context, viewport ui.Element, scroll *ui.ScrollState) {
	x, _, inside := viewport.PointerPosition()
	if !inside || scroll.MaxX <= 0 {
		return
	}
	const edge = 32
	width := viewport.Bounds().W
	var delta float32
	if x < edge {
		delta = -(edge - x) / 3
	} else if width-x < edge {
		delta = (edge - (width - x)) / 3
	}
	next := min(scroll.MaxX, max(0, scroll.X+delta))
	if next != scroll.X {
		scroll.X = next
		c.AnimationFrame()
	}
}
