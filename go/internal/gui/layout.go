package gui

import (
	"slices"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

const maxPanes = 16 // 31 nodes in the stored binary layout.

func addTab(layout *protocol.Layout, project uuid.UUID, name string) {
	tab := protocol.LayoutTab{Id: uuid.New(), Name: &name, ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{emptyPane()})
	layout.Tabs = append(layout.Tabs, tab)
	layout.ActiveTab = &tab.Id
}

func emptyPane() protocol.LayoutNode {
	return protocol.LayoutNode{Id: uuid.New(), Empty: new(protocol.LayoutNodeEmpty(true))}
}

func terminalPane(cwd string) protocol.LayoutNode {
	node := protocol.LayoutNode{Id: uuid.New(), ClientManaged: &struct {
		Type protocol.LayoutNodeClientManagedType `json:"type"`
	}{Type: "terminal"}}
	if cwd != "" {
		node.Cwd = &cwd
	}
	return node
}

func tabIndex(layout *protocol.Layout, id uuid.UUID) int {
	return slices.IndexFunc(layout.Tabs, func(tab protocol.LayoutTab) bool { return tab.Id == id })
}

func nodeIndex(tab *protocol.LayoutTab, id uuid.UUID) int {
	return slices.IndexFunc(tab.Nodes, func(node protocol.LayoutNode) bool { return node.Id == id })
}

func copyLayout(layout protocol.Layout) protocol.Layout {
	if layout.ClosedProjects != nil {
		closed := slices.Clone(*layout.ClosedProjects)
		layout.ClosedProjects = &closed
	}
	layout.Tabs = append(make([]protocol.LayoutTab, 0, len(layout.Tabs)), layout.Tabs...)
	for i := range layout.Tabs {
		layout.Tabs[i].Nodes = slices.Clone(layout.Tabs[i].Nodes)
	}
	return layout
}

// orderedPanes reads the stored split tree in visual order. It also reads
// layouts written by the prototype, which supported vertical splits.
func orderedPanes(tab *protocol.LayoutTab) []protocol.LayoutNode {
	var panes []protocol.LayoutNode
	var walk func(uuid.UUID)
	walk = func(id uuid.UUID) {
		i := nodeIndex(tab, id)
		if i < 0 {
			return
		}
		node := tab.Nodes[i]
		if node.First == nil || node.Second == nil {
			panes = append(panes, node)
			return
		}
		walk(*node.First)
		walk(*node.Second)
	}
	walk(tab.Root)
	return panes
}

func setPanes(tab *protocol.LayoutTab, panes []protocol.LayoutNode) {
	tab.Nodes = slices.Clone(panes)
	var build func([]protocol.LayoutNode) uuid.UUID
	build = func(nodes []protocol.LayoutNode) uuid.UUID {
		if len(nodes) == 1 {
			return nodes[0].Id
		}
		middle := len(nodes) / 2
		first, second := build(nodes[:middle]), build(nodes[middle:])
		split := protocol.LayoutNode{
			Id: uuid.New(), Axis: new(protocol.LayoutNodeAxis("horizontal")),
			Ratio: new(float32(middle) / float32(len(nodes))), First: &first, Second: &second,
		}
		tab.Nodes = append(tab.Nodes, split)
		return split.Id
	}
	tab.Root = build(panes)
}

func closePane(tab *protocol.LayoutTab, id uuid.UUID) {
	panes := slices.DeleteFunc(orderedPanes(tab), func(node protocol.LayoutNode) bool { return node.Id == id })
	if len(panes) == 0 {
		panes = append(panes, emptyPane())
	}
	setPanes(tab, panes)
}

func reorder(panes []protocol.LayoutNode, id uuid.UUID, gap int) []protocol.LayoutNode {
	from := slices.IndexFunc(panes, func(node protocol.LayoutNode) bool { return node.Id == id })
	if from < 0 || gap < 0 || gap > len(panes) || gap == from || gap == from+1 {
		return panes
	}
	next := slices.Clone(panes)
	next = slices.Delete(next, from, from+1)
	if gap > from {
		gap--
	}
	return slices.Insert(next, gap, panes[from])
}

func projectClosed(layout protocol.Layout, project *uuid.UUID) bool {
	return project != nil && layout.ClosedProjects != nil && slices.Contains(*layout.ClosedProjects, *project)
}

func selectProject(layout *protocol.Layout, project uuid.UUID) {
	if layout.ClosedProjects != nil {
		closed := slices.DeleteFunc(*layout.ClosedProjects, func(id uuid.UUID) bool { return id == project })
		layout.ClosedProjects = &closed
	}
	layout.ActiveProject = &project
	layout.ActiveTab = nil
	for _, tab := range layout.Tabs {
		if tab.ProjectId != nil && *tab.ProjectId == project {
			layout.ActiveTab = &tab.Id
			return
		}
	}
}

func closeTab(layout *protocol.Layout, id uuid.UUID) {
	i := tabIndex(layout, id)
	if i >= 0 {
		layout.Tabs = slices.Delete(layout.Tabs, i, i+1)
		if layout.ActiveProject != nil && layout.ActiveTab != nil && *layout.ActiveTab == id {
			selectProject(layout, *layout.ActiveProject)
		}
	}
}

func sameOrder(a, b []protocol.LayoutNode) bool {
	return slices.EqualFunc(a, b, func(x, y protocol.LayoutNode) bool { return x.Id == y.Id })
}
