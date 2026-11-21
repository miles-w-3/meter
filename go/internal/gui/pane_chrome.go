package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
	palette "pi-workspace.local/workspace/internal/theme"
)

func (a *app) paneHeader(c *ui.Context, tab *protocol.LayoutTab, node protocol.LayoutNode, title string) {
	panes := orderedPanes(tab)
	target := float32(0)
	if len(panes) > 1 {
		target = 28
	}
	shell := ui.Column(c.Key("pane-header"))
	height := target
	if !c.Preferences().ReduceMotion {
		height = shell.Animate("height", target, 150*time.Millisecond)
	}
	shell.Height(height).Clip().Children(func() {
		if target == 0 {
			return
		}
		paneContent(c, func() {
			row := ui.Row(c).FillWidth().Height(28).AlignItems(ui.Center)
			row.Children(func() {
				index := 1
				for i, pane := range panes {
					if pane.Id == node.Id {
						index = i + 1
						break
					}
				}
				handle := ui.Row(c.Key("handle")).Grow(1).MinWidth(0).AlignItems(ui.Center)
				if !a.busy() {
					handle.Label("Drag " + title).Cursor(ui.CursorGrab).Drag(paneDrag{tab: tab.Id, pane: node.Id})
				}
				handle.Children(func() {
					text(c, fmt.Sprintf("#%d", index), 11, muted).Font(monoFont).Width(paneTextInset).Shrink(0).Padding(0, 0, 0, 3)
					text(c, title, 13, ink).Grow(1).NoWrap().Ellipsis("…")
				})
				if row.Hovered() && iconButton(c, closeIcon, "Close pane", a.busy()).Clicked() {
					a.requestClosePane(tab.Id, node.Id)
				}
			})
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item("Rename pane").Disabled(a.busy()).Chosen() {
					a.nameEditor = &nameEditor{label: "Rename pane", value: title, save: func(name string) {
						a.save(func(layout *protocol.Layout) {
							t := &layout.Tabs[tabIndex(layout, tab.Id)]
							t.Nodes[nodeIndex(t, node.Id)].Name = &name
						})
					}}
				}
				if m.Item("Close pane").Disabled(a.busy()).Chosen() {
					a.requestClosePane(tab.Id, node.Id)
				}
			})
		})
	})
}

func (a *app) requestClosePane(tab, node uuid.UUID) {
	if p := a.panes[node]; p != nil && p.connected && p.state.Current.Status == "running" {
		a.closePaneConfirm = &paneDrag{tab: tab, pane: node}
		return
	}
	a.closePane(tab, node)
}

func (a *app) closePane(tab, node uuid.UUID) {
	if a.cwdEditor != nil && a.cwdEditor.id == node {
		a.cwdEditor = nil
	}
	a.save(func(layout *protocol.Layout) { closePane(&layout.Tabs[tabIndex(layout, tab)], node) })
}

func (a *app) paneStatusLine(c *ui.Context, tab *protocol.LayoutTab, node protocol.LayoutNode, cwd string, locked bool, p *pane) {
	status := "idle"
	if p.starting {
		status = "connecting"
	} else if node.ConversationId != nil {
		if !p.connected {
			status = "connecting"
		} else {
			status = string(p.state.Current.Status)
		}
	}
	paneContent(c, func() {
		ui.Row(c).Height(26).FillWidth().Padding(0, paneGutter, 0, paneTextInset).Gap(10).AlignItems(ui.Center).Children(func() {
			a.modelChip(c, node, p, locked || a.busy())
			button := ui.ButtonBase(c).Label("Change directory").Tooltip(cwd).Grow(1).MinWidth(0).Padding(2, 0).Radius(4).Disabled(a.busy() || locked)
			if button.Hovered() && !a.busy() && !locked {
				button.Background(panel)
			}
			button.Children(func() { pathLabel(c, cwd) })
			if button.Clicked() {
				a.pathError = ""
				a.cwdEditor = &editor{id: node.Id, value: cwd}
			}
			text(c, status, 12, ink).Font(monoFont).NoWrap().Shrink(0)
			if usage := p.state.Current.ContextUsage; usage != nil && usage.Window > 0 {
				pct := palette.Percent(usage.Tokens, usage.Window)
				label := "ctx " + palette.PercentLabel(pct)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Tooltip("Context usage").Children(func() {
					text(c, label, 12, muted).Font(monoFont).FontFeatures("tnum").NoWrap()
					ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
						for i := range palette.MeterCells {
							color := theme.SurfacePressed
							if float64(i)/palette.MeterCells < pct/100 {
								color = ui.Hex(palette.MeterColor(i))
							}
							ui.Box(c.Key(i)).Size(3, 4).Radius(1).Background(color)
						}
					})
				})
			}
		})
	})
}

func (a *app) directoryDialog(c *ui.Context, tab *protocol.LayoutTab, node protocol.LayoutNode, locked bool) {
	edit := a.cwdEditor
	if edit == nil || edit.id != node.Id {
		return
	}
	open := true
	ui.Modal(c.Key("change-directory"), &open, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			text(c, "Change directory", 16, ink).FontWeight(650)
			submitted := a.pathPicker(c, &edit.value)
			valid := strings.TrimSpace(edit.value) != "" && !a.busy() && !locked
			ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
				if action(c, "Cancel", false) {
					open = false
				}
				if action(c, "Apply", !valid) || (valid && submitted) {
					a.changeCwd(tab.Id, node, expandHome(edit.value))
				}
			})
		})
	})
	if !open {
		a.cwdEditor = nil
	}
}
