package gui

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

const minPaneWidth = 300

type paneResize struct {
	tab   uuid.UUID
	slot  int
	delta float32
}

// Allocate the space above the width floor in proportion to saved widths.
// Keeping the floor fixed allows the containing row to scroll on narrow windows.
func paneWidths(panes []protocol.LayoutNode, available float32) []float32 {
	widths := make([]float32, len(panes))
	var remainingShare float32
	for i, pane := range panes {
		widths[i] = 1 / float32(len(panes))
		if pane.WidthShare != nil {
			widths[i] = *pane.WidthShare
		}
		remainingShare += widths[i]
	}
	remaining := available
	pending := make([]bool, len(widths))
	for changed := true; changed; {
		changed = false
		for i, share := range widths {
			if !pending[i] && remaining*share < minPaneWidth*remainingShare {
				widths[i] = minPaneWidth
				pending[i], changed = true, true
				remaining -= minPaneWidth
				remainingShare -= share
			}
		}
	}
	for i, share := range widths {
		if !pending[i] {
			widths[i] = max(minPaneWidth, remaining*share/remainingShare)
		}
	}
	return widths
}

func (a *app) resizeDivider(c *ui.Context, target ui.Element, tab *protocol.LayoutTab, slot int, panes []protocol.LayoutNode, widths []float32) bool {
	target.Cursor(ui.CursorResizeColumn).Tooltip("Drag to resize panes")
	movement, _, moving := target.Dragged()
	_, _, ended := target.DragEnded()
	if moving && movement != 0 {
		if a.widthDrag == nil {
			a.widthDrag = &paneResize{tab: tab.Id, slot: slot}
		}
		drag := a.widthDrag
		if drag.tab == tab.Id && drag.slot == slot {
			drag.delta = min(widths[slot]-minPaneWidth, max(minPaneWidth-widths[slot-1], drag.delta+movement))
			c.AnimationFrame()
		}
	}
	drag := a.widthDrag
	if drag == nil || drag.tab != tab.Id || drag.slot != slot {
		return target.Hovered()
	}
	if ended || target.PressCanceled() {
		a.widthDrag = nil
		c.AnimationFrame()
	}
	if ended && drag.delta != 0 {
		resized := slices.Clone(widths)
		resized[slot-1] += drag.delta
		resized[slot] -= drag.delta
		var total float32
		for _, width := range resized {
			total += width
		}
		a.save(func(layout *protocol.Layout) {
			tab := &layout.Tabs[tabIndex(layout, tab.Id)]
			for i, pane := range panes {
				tab.Nodes[nodeIndex(tab, pane.Id)].WidthShare = new(resized[i] / total)
			}
		})
	}
	return true
}
