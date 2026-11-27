package gui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

type nameEditor struct {
	label, value string
	save         func(string)
}

func (a *app) dialogs(c *ui.Context) {
	a.contextDialog(c)
	if target := a.closePaneConfirm; target != nil {
		open := true
		ui.Modal(c.Key("close-running-pane"), &open, func() {
			ui.Column(c).Width(360).Gap(12).Children(func() {
				text(c, "Close running pane?", 16, ink).FontWeight(650)
				text(c, "The conversation will continue in the background.", 13, muted)
				ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
					if action(c, "Cancel", false) {
						open = false
					}
					if action(c, "Close pane", a.busy()) {
						open = false
						a.closePane(target.tab, target.pane)
					}
				})
			})
		})
		if !open {
			a.closePaneConfirm = nil
		}
	}
	if id := a.closeTabConfirm; id != nil {
		open := true
		ui.Modal(c.Key("close-running-tab"), &open, func() {
			ui.Column(c).Width(360).Gap(12).Children(func() {
				text(c, "Close running tab?", 16, ink).FontWeight(650)
				text(c, "The conversation will continue in the background.", 13, muted)
				ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
					if action(c, "Cancel", false) {
						open = false
					}
					if action(c, "Close tab", a.busy()) {
						open = false
						a.closeTab(*id)
					}
				})
			})
		})
		if !open {
			a.closeTabConfirm = nil
		}
	}
	if a.projectForm {
		ui.Modal(c.Key("new-project"), &a.projectForm, func() {
			ui.Column(c).Width(480).Gap(12).Children(func() {
				text(c, "New project", 16, ink).FontWeight(650)
				ui.TextInput(c, &a.projectName).Label("Project name").Placeholder("Project name")
				submitted := a.pathPicker(c, &a.projectPath)
				valid := !a.busy() && len(a.layout.Tabs) < 24 && strings.TrimSpace(a.projectName) != "" && strings.TrimSpace(a.projectPath) != ""
				ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
					if action(c, "Cancel", false) {
						a.projectForm = false
					}
					if action(c, "Add project", !valid) || (valid && submitted) {
						a.register()
					}
				})
			})
		})
	}
	if edit := a.tabEditor; edit != nil {
		label := "Rename tab"
		if edit.id == uuid.Nil {
			label = "New tab"
		}
		open := true
		ui.Modal(c.Key("tab-editor"), &open, func() {
			ui.Column(c).Width(340).Gap(12).Children(func() {
				text(c, label, 16, ink).FontWeight(650)
				input := ui.TextInput(c, &edit.value).Label("Tab name").Placeholder(edit.suggestion).AutoFocus()
				name := strings.TrimSpace(edit.value)
				if name == "" && edit.id == uuid.Nil {
					name = edit.suggestion
				}
				valid := a.layout.ActiveProject != nil && len([]rune(name)) <= 80 && !a.busy() && tabNameAvailable(a.layout, *a.layout.ActiveProject, edit.id, name)
				if name != "" && a.layout.ActiveProject != nil && !tabNameAvailable(a.layout, *a.layout.ActiveProject, edit.id, name) {
					text(c, "A tab with this name already exists in this project.", 12, warning)
				}
				ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
					if action(c, "Cancel", false) {
						open = false
					}
					if action(c, "Save tab", !valid) || (valid && input.Submitted()) {
						open = false
						a.save(func(layout *protocol.Layout) {
							if edit.id == uuid.Nil {
								a.paneScroll.X = 0
								addTab(layout, *layout.ActiveProject, name)
							} else {
								layout.Tabs[tabIndex(layout, edit.id)].Name = &name
							}
						})
					}
				})
			})
		})
		if !open {
			a.tabEditor = nil
		}
	}
	if edit := a.nameEditor; edit != nil {
		if !a.nameDialog(c, edit.label, "Name", "Save", &edit.value, edit.save) {
			a.nameEditor = nil
		}
	}
}

func (a *app) nameDialog(c *ui.Context, title, field, confirm string, value *string, commit func(string)) bool {
	open := true
	ui.Modal(c.Key(title), &open, func() {
		ui.Column(c).Width(340).Gap(12).Children(func() {
			text(c, title, 16, ink).FontWeight(650)
			input := ui.TextInput(c, value).Label(field).AutoFocus()
			name := strings.TrimSpace(*value)
			valid := name != "" && len([]rune(name)) <= 80 && !a.busy()
			ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
				if action(c, "Cancel", false) {
					open = false
				}
				if action(c, confirm, !valid) || (valid && input.Submitted()) {
					open = false
					commit(name)
				}
			})
		})
	})
	return open
}

func (a *app) renameProject(project protocol.Project, name string) {
	a.creating = true
	go func() {
		updated, err := a.api.RenameProject(a.ctx, project.Id, name)
		a.update(func() {
			a.creating = false
			if err != nil {
				a.notice = err.Error()
				return
			}
			for i, item := range a.catalog.Projects {
				if item.Id == updated.Id {
					a.catalog.Projects[i] = updated
					break
				}
			}
		})
	}()
}

func (a *app) conversationPalette(c *ui.Context) {
	ui.Modal(c.Key("conversations"), &a.paletteOpen, func() {
		ui.Column(c).Width(450).Gap(10).Children(func() {
			text(c, "Jump to conversation", 16, ink).FontWeight(650)
			input := ui.TextInput(c, &a.paletteFilter).Label("Search conversations").
				Placeholder("Search title, path, or ID").AutoFocus()
			needle := strings.ToLower(strings.TrimSpace(a.paletteFilter))
			submitted := input.Submitted()
			ui.Scroll(c).MaxHeight(320).Children(func() {
				ui.Column(c).Gap(3).Children(func() {
					found := false
					for _, conversation := range a.catalog.Conversations {
						title := a.title(conversation)
						if !strings.Contains(strings.ToLower(title+" "+conversation.Id+" "+conversation.Cwd), needle) {
							continue
						}
						row := sidebarRow(c.Key(conversation.Id), title, a.busy())
						row.Children(func() {
							ui.Column(c).Grow(1).Padding(5, 8).Gap(4).Children(func() {
								text(c, title, 13, ink).SingleLine()
								pathLabel(c, conversation.Cwd)
							})
						})
						if row.Clicked() || (!found && submitted && !a.busy()) {
							a.paletteOpen = false
							a.jumpTo(conversation)
						}
						found = true
					}
					if !found {
						text(c, "No matching conversations", 12, muted)
					}
				})
			})
		})
	})
}

func (a *app) jumpTo(conversation protocol.Conversation) {
	for _, tab := range a.layout.Tabs {
		if tab.ProjectId == nil {
			continue
		}
		for _, node := range tab.Nodes {
			if node.ConversationId == nil || *node.ConversationId != conversation.Id {
				continue
			}
			a.revealPane = node.Id
			a.save(func(layout *protocol.Layout) {
				if tab.ProjectId != nil {
					selectProject(layout, *tab.ProjectId)
				}
				layout.ActiveProject, layout.ActiveTab = tab.ProjectId, &tab.Id
			})
			return
		}
	}

	projectID := conversation.ProjectId
	if projectID == nil {
		for _, project := range a.catalog.Projects {
			if project.Cwd == conversation.Cwd {
				if id, err := uuid.Parse(project.Id); err == nil {
					projectID = &id
					break
				}
			}
		}
	}
	if projectID == nil {
		a.notice = "Add this conversation’s directory as a project first."
		return
	}

	a.save(func(layout *protocol.Layout) {
		if layout.ActiveProject == nil || *layout.ActiveProject != *projectID {
			selectProject(layout, *projectID)
		}
		if layout.ActiveTab == nil {
			if len(layout.Tabs) >= 24 {
				return
			}
			addTab(layout, *projectID, suggestTabName(*layout, *projectID))
		}
		tab := &layout.Tabs[tabIndex(layout, *layout.ActiveTab)]
		panes := orderedPanes(tab)
		for _, pane := range panes {
			if pane.Empty != nil {
				node := &tab.Nodes[nodeIndex(tab, pane.Id)]
				node.ConversationId, node.Empty, node.Cwd = &conversation.Id, nil, nil
				a.revealPane = node.Id
				return
			}
		}
		if len(panes) < maxPanes {
			node := protocol.LayoutNode{Id: uuid.New(), ConversationId: &conversation.Id}
			setPanes(tab, append(panes, node))
			a.revealPane = node.Id
		}
	})
}
