package gui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Cyenoch/mygoui"
	"github.com/Cyenoch/mygoui/form"
	"github.com/Cyenoch/mygoui/layout"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func text(c *ui.Context, value string, size float32, color ui.Color) ui.Element {
	return ui.Text(c, value).FontSize(size).TextColor(color).MinWidth(0)
}

func action(c *ui.Context, label string, disabled bool) bool {
	return mygoui.Button(c, mygoui.ButtonOptions{Label: label, Variant: mygoui.ButtonSecondary, Size: uitheme.ButtonExtraSmall, Disabled: disabled}).Clicked()
}

const sidebarWidth = 218
const sidebarIconWidth = 14

func (a *app) view(c *ui.Context) {
	a.viewWithDictation(c, a.startDictation)
}

func (a *app) viewWithDictation(c *ui.Context, beginDictation func(uuid.UUID, *pane)) {
	a.dictationVisible = false
	a.dictationTextFocused = false
	c.SetTheme(theme)
	c.SetMenuPresentation(ui.PopoverMenus)
	if c.Shortcut(ui.Ctrl, ui.KeyQ) && a.window != nil {
		a.window.Close()
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) && !a.busy() {
		a.paletteOpen, a.paletteFilter = true, ""
	}
	if a.dictation == nil && c.Shortcut(0, ui.KeyEscape) && !c.ShortcutRepeated() {
		a.treeEscape(time.Now())
	}
	if a.writeToast != "" {
		c.Toast(a.writeToast)
		a.writeToast = ""
	}
	root := ui.Column(c).Fill().Background(background).Focusable()
	a.observePaneDictation(root)
	root.Children(func() {
		layout.TitleBar(c, layout.TitleBarOptions{
			Label: "meter", MinHeight: 44,
			Leading: func() {
				ui.Image(c, firTitle).Size(23, 23)
				text(c, "meter", 17, ink).Font(monoFont).FontWeight(600)
				if project, ok := a.activeProject(); ok {
					crumb := project.Name
					if tab := a.activeTab(); tab != nil {
						crumb += " › " + tabName(*tab)
					}
					text(c, clean(crumb), 12, muted).NoWrap().Ellipsis("…").MaxWidth(240)
				}
				if a.name != "default" {
					text(c, a.name, 12, muted)
				}
			},
			Trailing: func() {
				ui.Row(c).Padding(0, 16, 0, 0).Gap(8).AlignItems(ui.Center).Children(func() {
					a.daemonPill(c)
					if iconButton(c, refreshIcon, "Refresh", a.fetching || a.creating || a.saving).Clicked() {
						if a.conflict || !a.loaded {
							a.reload()
						} else {
							a.refreshCatalog()
						}
					}
					if iconButton(c, settingsIcon, "Settings", false).Clicked() {
						if a.settingsSection == "tools" {
							a.refreshTools()
						}
						a.backendOpen = true
					}
				})
			},
		}).Background(sidebar)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			project, hasProject := a.activeProject()
			a.sidebarView(c, project)
			ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Radius(12).Clip().Background(contentSurface).Children(func() {
				tab := a.activeTab()
				if hasProject {
					a.projectHeader(c, project, tab)
				}
				if a.notice != "" {
					ui.Row(c).Padding(8, 16).Gap(10).Background(sidebar).AlignItems(ui.Center).Children(func() {
						text(c, a.notice, 13, warning).Grow(1)
						if a.conflict && action(c, "Reload layout", a.fetching) {
							a.reload()
						}
					})
				}
				if hasProject && tab != nil {
					a.surface(c, project, tab)
					return
				}
				ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
					switch {
					case !a.loaded:
						text(c, "Loading workspace", 22, ink).FontWeight(650)
					case hasProject:
						text(c, "Create a tab to organize your panes.", 15, muted)
					default:
						text(c, "Choose or add a project", 22, ink).FontWeight(650)
						text(c, "Projects start from a directory on the daemon host.", 13, muted)
					}
				})
			})
		})
	})
	a.dialogs(c)
	a.backendPanel(c)
	a.conversationPalette(c)
	if a.dictation != nil && (!a.dictationVisible || c.Shortcut(0, ui.KeyEscape)) {
		a.cancelDictation()
	}
	// Begin after checking visibility of the recording rendered in this frame.
	// A newly started recording cannot be visible until the next frame.
	a.tickPaneDictation(c, root.FocusWithin(), beginDictation)
}

func (a *app) sidebarView(c *ui.Context, project protocol.Project) {
	ui.Column(c).Width(sidebarWidth).MinWidth(sidebarWidth).MinHeight(0).Background(sidebar).Padding(14, 16).Gap(9).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			sectionLabel(c, "PROJECTS").Grow(1)
			if ghostPlus(c, "Add project", a.busy()).Clicked() {
				a.pathError = ""
				a.projectForm = !a.projectForm
			}
		})
		ui.Scroll(c).MaxHeight(155).MinHeight(0).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				for _, item := range a.catalog.Projects {
					id, err := uuid.Parse(item.Id)
					if err == nil && a.layout.ClosedProjects != nil && slices.Contains(*a.layout.ClosedProjects, id) {
						continue
					}
					selected := item.Id == project.Id
					row := sidebarRow(c.Key(item.Id), item.Name, a.busy())
					if selected {
						row.Background(selectedFill)
					}
					count := a.runningCount(item)
					row.Children(func() {
						ui.Icon(c, folderIcon).Size(sidebarIconWidth, sidebarIconWidth).Shrink(0).TextColor(muted).Tooltip(item.Cwd)
						color := muted
						if selected {
							color = ink
						}
						text(c, item.Name, 13, color).Grow(1).NoWrap().Ellipsis("…")
						text(c, fmt.Sprint(count), 11, muted).Font(monoFont).FontFeatures("tnum").
							Tooltip(fmt.Sprintf("%d running panes", count))
					})
					row.ContextMenu(func(m *ui.Menu) { a.projectMenu(m, item, selected) })
					if row.Clicked() && !selected {
						if id, err := uuid.Parse(item.Id); err == nil {
							a.selectProject(id)
						}
					}
				}
			})
		})
		if a.layout.ClosedProjects != nil && len(*a.layout.ClosedProjects) > 0 {
			ui.MenuButton(c, "Reopen project", func(m *ui.Menu) {
				for _, project := range a.catalog.Projects {
					id, err := uuid.Parse(project.Id)
					if err == nil && slices.Contains(*a.layout.ClosedProjects, id) && m.Item(project.Name).Chosen() {
						a.selectProject(id)
					}
				}
			})
		}
		ui.Box(c).Height(1).Background(theme.Border)
		sectionLabel(c, "RECENT")
		filter := ui.TextInputBase(c, &a.filter).Placeholder("Filter conversations").Label("Filter conversations").Padding(7, 9).Radius(6).Background(panel)
		if filter.Focused() {
			a.dictationTextFocused = true
			filter.Border(1, theme.Focus)
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(5).Children(func() {
				conversations := a.recentConversations()
				if len(conversations) == 0 {
					text(c, "No open conversations", 12, muted)
				}
				for _, entry := range conversations {
					conversation := entry.conversation
					title := a.title(conversation)
					row := sidebarRow(c.Key(entry.node.Id.String()), title, a.busy()).Tooltip(title)
					row.Children(func() {
						ui.Row(c).Size(sidebarIconWidth, sidebarIconWidth).Shrink(0).Center().Children(func() {
							if color, ok := conversationLight(a.panes[entry.node.Id]); ok {
								ui.Box(c).Size(7, 7).Radius(4).Background(color)
							}
						})
						ui.Column(c).Gap(3).Grow(1).MinWidth(0).Padding(4, 0).Children(func() {
							text(c, title, 13, ink).NoWrap().Ellipsis("…")
							subtitle := tabName(entry.tab)
							for _, p := range a.catalog.Projects {
								if entry.tab.ProjectId != nil && p.Id == entry.tab.ProjectId.String() {
									subtitle = p.Name + " · " + subtitle
									break
								}
							}
							if conversation.LastActivity != nil {
								subtitle += " · " + activityAge(c.Now(), *conversation.LastActivity)
							}
							text(c, subtitle, 11, muted).NoWrap().Ellipsis("…")
						})
					})
					if row.Clicked() {
						a.revealPane = entry.node.Id
						if a.layout.ActiveTab == nil || *a.layout.ActiveTab != entry.tab.Id || a.layout.ActiveProject == nil || *a.layout.ActiveProject != *entry.tab.ProjectId {
							a.save(func(layout *protocol.Layout) {
								layout.ActiveProject, layout.ActiveTab = entry.tab.ProjectId, &entry.tab.Id
							})
						}
					}
				}
			})
		})
	})
}

func (a *app) projectMenu(m *ui.Menu, project protocol.Project, selected bool) {
	if m.Item("Rename project").Disabled(a.busy()).Chosen() {
		a.nameEditor = &nameEditor{label: "Rename project", value: project.Name, save: func(name string) { a.renameProject(project, name) }}
	}
	if m.Item("Close project").Disabled(a.busy()).Chosen() {
		a.tabEditor, a.cwdEditor = nil, nil
		a.save(func(layout *protocol.Layout) {
			id, err := uuid.Parse(project.Id)
			if err != nil {
				return
			}
			closed := []uuid.UUID{}
			if layout.ClosedProjects != nil {
				closed = slices.Clone(*layout.ClosedProjects)
			}
			if !slices.Contains(closed, id) {
				closed = append(closed, id)
			}
			layout.ClosedProjects = &closed
			if layout.ActiveProject != nil && *layout.ActiveProject == id {
				layout.ActiveProject, layout.ActiveTab = nil, nil
			}
		})
	}
}

func sidebarRow(c *ui.Context, label string, disabled bool) ui.Element {
	row := ui.ButtonBase(c).Label(label).AlignSelf(ui.Stretch).Justify(ui.Start).AlignItems(ui.Center).
		MinWidth(0).MinHeight(32).Gap(7).Padding(0, 7).Radius(6).Disabled(disabled)
	if row.Hovered() && !disabled {
		row.Background(panel)
	}
	return row
}

func activityAge(now time.Time, milliseconds int) string {
	age := now.Sub(time.UnixMilli(int64(milliseconds)))
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

func (a *app) runningCount(project protocol.Project) int {
	count := 0
	for _, tab := range a.layout.Tabs {
		if tab.ProjectId == nil || tab.ProjectId.String() != project.Id {
			continue
		}
		for _, node := range tab.Nodes {
			p := a.panes[node.Id]
			if p != nil && p.connected && p.state.Current.Status == "running" {
				count++
			}
		}
	}
	return count
}

func (a *app) daemonPill(c *ui.Context) {
	online := a.loaded && len(a.panes) == 0
	for _, p := range a.panes {
		online = online || p.connected
	}
	label, color := "Connecting", warning
	if online {
		label, color = "Local daemon", theme.Success
	} else if a.loaded {
		label = "Reconnecting"
	}
	if a.provider != "" {
		label += " · " + a.provider
	}
	if mygoui.Button(c, mygoui.ButtonOptions{
		Label: label, Tooltip: "Backend info", Variant: mygoui.ButtonSecondary, Size: uitheme.ButtonExtraSmall,
		Content: func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(color)
			text(c, label, 11, muted)
		},
	}).Clicked() {
		a.backendConfirm = ""
		a.settingsSection = "backend"
		a.backendOpen = true
		a.runBackendAction("Refresh")
	}
}

type recentEntry struct {
	conversation protocol.Conversation
	tab          protocol.LayoutTab
	node         protocol.LayoutNode
}

func conversationLight(p *pane) (ui.Color, bool) {
	if p == nil || !p.connected {
		return ui.Transparent, false
	}
	switch p.state.Current.Status {
	case "idle":
		return idleStatus, true
	case "running":
		return runningColor, true
	case "failed":
		return theme.Danger, true
	default:
		return ui.Transparent, false
	}
}

func (a *app) recentConversations() []recentEntry {
	var recent []recentEntry
	needle := strings.ToLower(strings.TrimSpace(a.filter))
	for _, tab := range a.layout.Tabs {
		if projectClosed(a.layout, tab.ProjectId) {
			continue
		}
		if tab.ProjectId == nil {
			continue
		}
		for _, node := range orderedPanes(&tab) {
			if node.ConversationId == nil {
				continue
			}
			conversation, _ := a.metadata(*node.ConversationId)
			if strings.Contains(strings.ToLower(a.title(conversation)+" "+conversation.Cwd), needle) {
				recent = append(recent, recentEntry{conversation, tab, node})
			}
		}
	}
	slices.SortStableFunc(recent, func(a, b recentEntry) int {
		return cmp.Compare(activityTime(b.conversation), activityTime(a.conversation))
	})
	return recent
}

func activityTime(conversation protocol.Conversation) int {
	if conversation.LastActivity != nil {
		return *conversation.LastActivity
	}
	return 0
}

func (a *app) title(conversation protocol.Conversation) string {
	if conversation.Title != nil && *conversation.Title != "" {
		return clean(*conversation.Title)
	}
	return "Conversation #" + conversation.Id
}

func (a *app) metadata(id string) (protocol.Conversation, bool) {
	for _, conversation := range a.catalog.Conversations {
		if conversation.Id == id {
			return conversation, true
		}
	}
	return protocol.Conversation{Id: id}, false
}

func tabName(tab protocol.LayoutTab) string {
	if tab.Name != nil {
		return *tab.Name
	}
	return "Main"
}

func (a *app) projectHeader(c *ui.Context, project protocol.Project, active *protocol.LayoutTab) {
	closeSize := uitheme.Scale(c, uitheme.ButtonHeight(uitheme.ButtonExtraSmall))
	ui.Row(c).Height(40).Background(sidebar).Padding(0, 2*paneGutter, 0, paneGutter).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Height(40).Gap(4).AlignItems(ui.Center).Children(func() {
				for _, tab := range a.layout.Tabs {
					if tab.ProjectId == nil || tab.ProjectId.String() != project.Id {
						continue
					}
					selected := active != nil && active.Id == tab.Id
					row := ui.Row(c.Key(tab.Id.String())).Height(32).Padding(0, 4, 0, 10).Gap(4).AlignItems(ui.Center).Radius(8)
					if selected {
						row.Background(contentSurface)
					}
					row.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, tab) })
					row.Children(func() {
						chip := ui.ButtonBase(c).Label(tabName(tab)).Height(32).MaxWidth(200).Center().Disabled(a.busy())
						chip.Children(func() {
							color := muted
							if selected {
								color = ink
							}
							text(c, tabName(tab), 13, color).NoWrap().Ellipsis("…").MaxWidth(200)
						})
						if chip.Clicked() && !selected {
							a.tabEditor, a.cwdEditor = nil, nil
							a.paneScroll.X = 0
							a.save(func(layout *protocol.Layout) { layout.ActiveTab = &tab.Id })
						}
						ui.Row(c.Key("close")).Size(closeSize, closeSize).Shrink(0).Center().Children(func() {
							if row.Hovered() && iconButton(c, closeIcon, "Close tab "+tabName(tab), a.busy()).AlignSelf(ui.Center).Clicked() {
								a.requestCloseTab(tab)
							}
						})
					})
				}
				if ghostPlus(c.Key("new-tab"), "New tab", a.busy() || len(a.layout.Tabs) >= 24).Clicked() {
					a.tabEditor = &editor{id: uuid.Nil, suggestion: suggestTabName(a.layout, *a.layout.ActiveProject)}
				}
			})
		})
		if active != nil {
			if iconButton(c, terminalIcon, "New terminal pane", a.busy() || len(orderedPanes(active)) >= maxPanes).AlignSelf(ui.Center).Clicked() {
				a.appendPane(active.Id, terminalPane(project.Cwd))
			}
			if iconButton(c, newPaneIcon, "New pane", a.busy() || len(orderedPanes(active)) >= maxPanes).AlignSelf(ui.Center).Clicked() {
				a.appendPane(active.Id, emptyPane())
			}
		}
	})
}

func (a *app) tabStatus(tab protocol.LayoutTab) string {
	status := "idle"
	for _, node := range orderedPanes(&tab) {
		if p := a.panes[node.Id]; p != nil && p.connected {
			if p.state.Current.Status == "running" {
				return "running"
			}
			if p.state.Current.Status == "failed" {
				status = "failed"
			}
		}
	}
	return status
}

func (a *app) requestCloseTab(tab protocol.LayoutTab) {
	for _, node := range orderedPanes(&tab) {
		if node.PendingFork != nil {
			a.notice = "Resolve the pending branch send before closing this tab."
			return
		}
	}
	if a.tabStatus(tab) == "running" {
		id := tab.Id
		a.closeTabConfirm = &id
		return
	}
	a.closeTab(tab.Id)
}

func (a *app) closeTab(id uuid.UUID) {
	a.tabEditor, a.cwdEditor = nil, nil
	a.paneScroll.X = 0
	a.save(func(layout *protocol.Layout) { closeTab(layout, id) })
}

func (a *app) tabMenu(m *ui.Menu, tab protocol.LayoutTab) {
	if m.Item("Rename tab").Disabled(a.busy()).Chosen() {
		a.tabEditor = &editor{id: tab.Id, value: tabName(tab)}
	}
	if m.Item("Close tab").Disabled(a.busy()).Chosen() {
		a.requestCloseTab(tab)
	}
}

func (a *app) surface(c *ui.Context, project protocol.Project, tab *protocol.LayoutTab) {
	panes := orderedPanes(tab)
	focusFirst := a.selectedPaneTab != tab.Id || !containsPane(panes, a.selectedPane)
	if focusFirst {
		a.cancelDictation()
		a.selectedPaneTab, a.selectedPane = tab.Id, uuid.Nil
		if len(panes) > 0 {
			a.selectedPane = panes[0].Id
		}
	}
	width, _ := c.Size()
	const gap = paneGutter
	available := max(minPaneWidth*float32(len(panes)), width-sidebarWidth-1-float32((len(panes)+1)*gap))
	widths := paneWidths(panes, available)
	ui.ScrollHorizontal(c.Key(tab.Id.String())).Grow(1).MinHeight(0).MinWidth(0).
		Scrollbars(true).TrackScroll(&a.paneScroll).Children(func() {
		ui.Row(c).FillHeight().Padding(12, 0).AlignItems(ui.Stretch).Children(func() {
			for i, node := range panes {
				a.dropGap(c, tab, panes, i, gap, widths)
				paneWidth := widths[i]
				if drag := a.widthDrag; drag != nil && drag.tab == tab.Id {
					switch i {
					case drag.slot - 1:
						paneWidth += drag.delta
					case drag.slot:
						paneWidth -= drag.delta
					}
				}
				column := ui.Column(c.Key(node.Id.String())).Width(paneWidth).MinWidth(paneWidth).MinHeight(0).Clip().
					Radius(12).Background(contentSurface).Focusable().Transition(ui.ElementTransition{Enter: &ui.Motion{X: 6}})
				border := theme.Border
				if a.selectedPane == node.Id {
					border = muted
				}
				column.Border(1, border)
				if p := a.panes[node.Id]; p != nil && p.connected && p.state.Current.Status == "running" {
					reduceMotion := c.Preferences().ReduceMotion
					column.DrawOver(func(painter *ui.Painter, bounds ui.Rect) {
						drawActivityTrace(painter, bounds, reduceMotion)
					})
				}
				if focusFirst && a.selectedPane == node.Id {
					column.Focus(ui.FocusOptions{PreventScroll: true})
				}
				column.ObserveInput(func(ev ui.InputEvent) {
					if ev.Kind == ui.InputPointerDown && ev.Button == 0 && a.selectedPane != node.Id {
						a.cancelDictation()
						a.selectedPane = node.Id
					}
				})
				column.Children(func() {
					if len(panes) > 1 {
						ui.Box(c).FillWidth().Height(1).Background(highlight)
					}
					if a.tree != nil && a.tree.pane == node.Id {
						a.treePane(c, a.tree)
					} else {
						a.renderPane(c, project, tab, node)
					}
				})
				if a.revealPane == node.Id {
					column.ScrollIntoView()
					a.revealPane = uuid.Nil
				}
			}
			a.dropGap(c, tab, panes, len(panes), gap, widths)
		})
	})
}

func containsPane(panes []protocol.LayoutNode, id uuid.UUID) bool {
	for _, node := range panes {
		if node.Id == id {
			return true
		}
	}
	return false
}

func (a *app) dropGap(c *ui.Context, tab *protocol.LayoutTab, panes []protocol.LayoutNode, slot, width int, widths []float32) {
	target := ui.Row(c.Key(slot)).Width(float32(width)).MinWidth(float32(width)).Justify(ui.Center)
	if a.busy() || len(panes) <= 1 {
		return
	}
	tint := ui.Transparent
	if slot > 0 && slot < len(panes) {
		if a.resizeDivider(c, target, tab, slot, panes, widths) {
			tint = muted
		} else {
			tint = theme.Border
		}
	}
	if drag, ok := ui.DragOver[paneDrag](target); ok && drag.tab == tab.Id {
		if moved := reorder(panes, drag.pane, slot); !sameOrder(panes, moved) {
			tint = accent
		}
	}
	target.Children(func() { ui.Box(c).Width(2).Radius(1).Background(tint) })
	if drag, ok := ui.Drop[paneDrag](target); ok && drag.tab == tab.Id {
		if moved := reorder(panes, drag.pane, slot); !sameOrder(panes, moved) {
			a.save(func(layout *protocol.Layout) { setPanes(&layout.Tabs[tabIndex(layout, tab.Id)], moved) })
		}
	}
}

func (a *app) renderPane(c *ui.Context, project protocol.Project, tab *protocol.LayoutTab, node protocol.LayoutNode) {
	if node.ClientManaged != nil && node.ClientManaged.Type == "terminal" {
		a.renderTerminalPane(c, tab, node)
		return
	}
	if node.ConversationId == nil {
		cwd := project.Cwd
		if node.Cwd != nil {
			cwd = *node.Cwd
		}
		a.paneHeader(c, tab, node, paneTitle(node, "New pane"))
		p := a.panes[node.Id]
		if p == nil {
			return
		}
		ui.Column(c).Grow(1).MinHeight(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			text(c, "Start a conversation", 14, muted)
		})
		composerArea(c, func() {
			a.composer(c, node.Id, p, form.ComposerOptions{
				Label: "New conversation", Placeholder: "Message…", SubmitLabel: "Send", MinLines: 1, MaxLines: 8,
				ReadOnly:       p.starting,
				SubmitDisabled: a.busy() || p.starting || p.modelPicker.pending || (a.cwdEditor != nil && a.cwdEditor.id == node.Id),
				OnSubmit:       func(string) { a.create(tab.Id, node, cwd, p) },
			})
			a.dictationPreview(c, p)
			if p.notice != "" {
				text(c, clean(p.notice), 12, warning)
			}
		})
		a.paneStatusLine(c, tab, node, cwd, false, p)
		a.directoryDialog(c, tab, node, false)
		return
	}
	p := a.panes[node.Id]
	if p == nil {
		return
	}
	if node.TreePosition != nil {
		position := *node.TreePosition
		a.loadPinnedHistory(p, position)
		a.paneHeader(c, tab, node, paneTitle(node, "Tree preview"))
		ui.Column(c).Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().Padding(8, 16).Children(func() {
				text(c, "Tree preview · send a message to branch conversation", 12, warning)
			})
			if p.historyError != "" {
				text(c, clean(p.historyError), 12, warning)
			} else if p.historyLoading && len(p.historical.Entries()) == 0 {
				text(c, "Loading historical transcript…", 12, muted)
			} else {
				a.transcriptState(c, p, position.ConversationId, &p.historical, func() { a.olderPinnedHistory(p, position) }, p.historyLoading)
			}
			composerArea(c, func() {
				if node.PendingFork == nil {
					a.composer(c, node.Id, p, form.ComposerOptions{
						Label: "Message tree preview", Placeholder: "Continue on a new branch…", SubmitLabel: "Send", MinLines: 1, MaxLines: 8,
						ReadOnly: a.busy(), SubmitDisabled: a.busy() || !p.connected || p.historyLoading || p.historyError != "",
						OnSubmit: func(string) { a.beginFork(tab.Id, node, p) },
					})
					a.dictationPreview(c, p)
				} else if !p.forkInFlight && action(c, "Retry branch", a.busy()) {
					if p.forkReceipt != nil {
						a.finishFork(tab.Id, node.Id, node.PendingFork.RequestId, p)
					} else {
						a.resumeFork(tab.Id, node, p)
					}
				}
				if p.notice != "" {
					text(c, clean(p.notice), 12, warning)
				}
			})
		})
		return
	}
	id := *node.ConversationId
	conversation, _ := a.metadata(id)
	title := paneTitle(node, a.title(conversation))
	running := p.connected && p.state.Current.Status == "running"
	a.paneHeader(c, tab, node, title)
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Background(contentSurface).Children(func() {
		a.transcript(c, p, id)
	})
	composerArea(c, func() {
		queuedMessages(c, a, p, id)
		mode := protocol.SubmitInputMode("prompt")
		if running {
			mode = "followup"
		}
		a.composerWithStop(c, node.Id, p, form.ComposerOptions{
			Label: "Message " + id, Placeholder: "Message…", SubmitLabel: "Send", MinLines: 1, MaxLines: 8,
			ReadOnly: p.pendingFirst || p.queuePending != "", SubmitDisabled: !p.connected || p.modelPicker.pending,
			OnSubmit: func(string) { a.send(p, id, mode, false) },
		}, running, func() {
			if a.mayAbort && p.state.Current.AbortTarget != nil {
				a.abort(p, id, *p.state.Current.AbortTarget)
			}
		}, !a.mayAbort || p.state.Current.AbortTarget == nil, func() bool {
			if a.dictation != nil || p.queuePending != "" || p.state.Current.Queued == nil || len(*p.state.Current.Queued) == 0 {
				return false
			}
			items := *p.state.Current.Queued
			a.recallQueued(p, id, items[len(items)-1], false)
			return true
		})

		a.dictationPreview(c, p)
		if p.rejected && action(c, "Dismiss rejection", false) {
			p.restoreSubmission()
			p.rejected, p.notice = false, ""
		}
		if a.mayRetry && p.submission.Uncertain() && action(c, "Retry", !p.connected) {
			a.send(p, id, "prompt", true)
		}
		if p.notice != "" {
			text(c, clean(p.notice), 12, muted)
		}
	})
	a.paneStatusLine(c, tab, node, conversation.Cwd, !p.connected || running, p)
	a.directoryDialog(c, tab, node, !p.connected || running)
}

func (a *app) renderTerminalPane(c *ui.Context, tab *protocol.LayoutTab, node protocol.LayoutNode) {
	a.paneHeader(c, tab, node, paneTitle(node, "Terminal"))
	if err := a.terminalErrors[node.Id]; err != "" {
		ui.Column(c).Grow(1).MinHeight(0).Justify(ui.Center).AlignItems(ui.Center).Padding(16).Children(func() {
			text(c, err, 12, warning).MaxWidth(400)
		})
		return
	}
	t, ok := a.terminals[node.Id].(*terminal.Terminal)
	if !ok || t == nil {
		return
	}
	terminal.View(c, t).Grow(1).FillWidth()
}

func (a *app) composer(c *ui.Context, id uuid.UUID, p *pane, options form.ComposerOptions) {
	a.composerWithStop(c, id, p, options, false, nil, false)
}

func (a *app) composerWithStop(c *ui.Context, id uuid.UUID, p *pane, options form.ComposerOptions, running bool, stop func(), stopDisabled bool, recall ...func() bool) {
	draft := &p.draft
	services := c.Services()
	recording := a.dictation != nil && a.dictation.pane == p
	canSend := func(value string) bool {
		return !recording && !options.ReadOnly && !options.Disabled && !options.SubmitDisabled && (strings.TrimSpace(value) != "" || len(p.images) > 0)
	}
	border := ui.Transparent
	status := ""
	if recording {
		status, border = dictationStatus(a.dictation)
	}
	ui.Column(c).FillWidth().MinWidth(0).Radius(8).Clip().Background(panel).Border(1, border).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(highlight)
		imagePreviews(c, p)
		ui.Row(c).Padding(8, paneGutter, 8, paneTextInset).Gap(8).AlignItems(ui.Start).Children(func() {
			input := ui.TextAreaBase(c, draft).Grow(1).MinWidth(0).Lines(1, 8).Label(options.Label).
				Placeholder(options.Placeholder).PlaceholderColor(ui.Hex("#abbcb0")).Padding(0).
				ReadOnly(options.ReadOnly || recording).FontSize(14).TextColor(ink).
				OnPaste(func(text string) bool { return !recording && p.pasteImage(services, text) }).
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyEscape && k.Mods == 0 && len(recall) > 0 && recall[0]() {
						return true
					}
					if k.Key != ui.KeyEnter || k.Mods != 0 {
						return false
					}
					value := k.Text()
					if canSend(value) {
						*draft = value
						options.OnSubmit(value)
						if *draft != value {
							k.SetText(*draft)
						}
					}
					return true
				})
			if p.focusDictatedDraft {
				p.focusDictatedDraft = false
				input.Focus(ui.FocusOptions{PreventScroll: true})
				end := len([]rune(p.draft))
				input.SetTextSelection(end, end)
			}
			if input.Focused() {
				a.dictationTextFocused = true
			}
			if recording && strings.TrimSpace(a.dictation.preview) != "" {
				start := len([]rune(dictationDraft(a.dictation.base, "x"))) - 1
				input.TextRanges(ui.TextRange{Start: start, End: len([]rune(p.draft)), Color: muted})
			}
			a.dictationInput(c, input, p, options.ReadOnly || options.Disabled, func() { a.startDictation(id, p) })
			if options.Actions != nil {
				options.Actions()
			}
			if recording {
				text(c, status, 11, muted).NoWrap()
			}
			if running {
				button := ui.ButtonBase(c).Label("Stop").Size(32, 32).AlignSelf(ui.End).Radius(16).Background(accent).Center().Disabled(stopDisabled).KeepFocus()
				button.Children(func() { ui.Box(c).Size(10, 10).Radius(1).Background(background) })
				if button.Clicked() && !stopDisabled {
					stop()
				}
				return
			}
			button := ui.ButtonBase(c).Label(options.SubmitLabel).Size(32, 32).AlignSelf(ui.End).Radius(16).Background(accent).Center().Disabled(!canSend(*draft)).KeepFocus()
			button.Children(func() { text(c, "↑", 18, background).FontWeight(650) })
			if button.Clicked() && canSend(*draft) {
				options.OnSubmit(*draft)
			}
		})
	})
}

func composerArea(c *ui.Context, children func()) {
	paneContent(c, func() {
		ui.Column(c).FillWidth().MinWidth(0).Padding(8, 0, 4).Gap(5).Children(children)
	})
}

func paneTitle(node protocol.LayoutNode, fallback string) string {
	if node.Name != nil {
		return *node.Name
	}
	return fallback
}

func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, value)
}
