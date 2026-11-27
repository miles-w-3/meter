package gui

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

type modelPickerState struct {
	open, pending bool
	query, error  string
	lastQuery     string
	index         int
	scroll        ui.ScrollState
	draft         protocol.SelectionInput
	expected      protocol.Selection
}

func (a *app) paneSelection(p *pane) protocol.Selection {
	if p.conversation != "" && p.state.Current.Selection != nil {
		return *p.state.Current.Selection
	}
	if p.modelDraft != nil && p.conversation == "" {
		return protocol.Selection{Model: &p.modelDraft.Model, ThinkingLevel: protocol.ThinkingLevel(p.modelDraft.ThinkingLevel)}
	}
	if p.conversation != "" {
		if info, ok := a.metadata(p.conversation); ok && info.Selection != nil {
			return *info.Selection
		}
		return protocol.Selection{ThinkingLevel: "off"}
	}
	level := protocol.ThinkingLevel("off")
	if a.modelDefaults.ThinkingLevel != nil {
		level = protocol.ThinkingLevel(*a.modelDefaults.ThinkingLevel)
	}
	return protocol.Selection{Model: a.modelDefaults.Model, ThinkingLevel: level}
}

func (a *app) modelInfo(ref protocol.ModelRef) *protocol.ModelInfo {
	for i := range a.modelsInventory.Models {
		m := &a.modelsInventory.Models[i]
		if m.Provider == ref.Provider && m.ModelId == ref.ModelId {
			return m
		}
	}
	return nil
}

func (a *app) providerStatusFor(id string) string {
	for _, p := range a.modelsInventory.Providers {
		if p.Id == id {
			return string(p.Status)
		}
	}
	return "unknown"
}

func (a *app) modelChip(c *ui.Context, node protocol.LayoutNode, p *pane, locked bool) {
	current := a.paneSelection(p)
	label, tooltip := "Choose model", "Choose a model and thinking level"
	if current.Model != nil {
		name := current.Model.ModelId
		if model := a.modelInfo(*current.Model); model != nil {
			name = model.Name
		}
		label = name + " · " + string(current.ThinkingLevel)
		tooltip = current.Model.Provider + " / " + current.Model.ModelId + " · " + string(current.ThinkingLevel)
		if a.modelsLoaded && a.modelInfo(*current.Model) == nil {
			label += " · unavailable"
			tooltip += " (not in the current catalog)"
		} else if a.modelsLoaded && a.providerStatusFor(current.Model.Provider) != "configured" {
			label += " · unavailable"
			tooltip += " (provider authentication is not available)"
		}
	}
	if locked {
		tooltip += " · Available when the conversation is idle and connected"
	}
	button := ui.ButtonBase(c.Key("model-chip")).Label("Choose model and thinking").Tooltip(tooltip).MaxWidth(220).MinWidth(0).Shrink(1).Clip().
		Padding(2, 8).Radius(controlRadius).Border(1, theme.Border).Disabled(locked || p.modelPicker.pending)
	if button.Hovered() && !locked {
		button.Background(panel)
	}
	button.Children(func() { text(c, label+" ⌄", 12, muted).Font(monoFont).MinWidth(0).Shrink(1).SingleLine() })
	if button.Clicked() {
		picker := &p.modelPicker
		picker.open, picker.query, picker.error, picker.index = true, "", "", 0
		picker.scroll.Y, picker.lastQuery = 0, ""
		picker.expected = current
		picker.draft = protocol.SelectionInput{ThinkingLevel: protocol.ThinkingLevel(current.ThinkingLevel)}
		if current.Model != nil {
			picker.draft.Model = *current.Model
		}
		if !a.modelsLoaded && !a.modelsLoading {
			a.refreshModels()
		}
	}
	ui.Popover(c.Key("model-picker"), button, &p.modelPicker.open, func() { a.modelPickerPanel(c, node, p) })
}

func (a *app) matchingModels(query string) []protocol.ModelInfo {
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]protocol.ModelInfo, 0)
	for _, model := range a.modelsInventory.Models {
		if query == "" || strings.Contains(strings.ToLower(model.Name+" "+model.ModelId+" "+model.Provider), query) {
			items = append(items, model)
		}
	}
	// Recently used and configured providers come first, without hiding other models.
	slices.SortStableFunc(items, func(x, y protocol.ModelInfo) int {
		rank := func(m protocol.ModelInfo) int {
			for i, r := range a.recentModels {
				if r.Provider == m.Provider && r.ModelId == m.ModelId {
					return i
				}
			}
			if a.providerStatusFor(m.Provider) == "configured" {
				return 100
			}
			return 200
		}
		return rank(x) - rank(y)
	})
	if len(items) > 100 {
		items = items[:100]
	}
	return items
}

func (a *app) choosePickerModel(picker *modelPickerState, model protocol.ModelInfo) {
	picker.draft.Model = protocol.ModelRef{Provider: model.Provider, ModelId: model.ModelId}
	if !slices.Contains(model.ThinkingLevels, protocol.ThinkingLevel(picker.draft.ThinkingLevel)) {
		picker.draft.ThinkingLevel = "off"
		if len(model.ThinkingLevels) > 0 {
			picker.draft.ThinkingLevel = protocol.ThinkingLevel(model.ThinkingLevels[0])
		}
	}
}

func (a *app) modelPickerPanel(c *ui.Context, node protocol.LayoutNode, p *pane) {
	picker := &p.modelPicker
	if picker.query != picker.lastQuery {
		picker.index, picker.scroll.Y, picker.lastQuery = 0, 0, picker.query
	}
	items := a.matchingModels(picker.query)
	if picker.index >= len(items) {
		picker.index = 0
	}
	ui.Column(c).Width(410).Padding(14).Gap(10).Children(func() {
		text(c, "Model & thinking", 15, ink).FontWeight(650)
		ui.TextInputBase(c, &picker.query).Label("Search models").Placeholder("Search models or providers…").AutoFocus().Padding(8, 10).Radius(6).Background(panel).OnTextKey(func(k ui.TextKey) bool {
			if k.Mods != 0 || len(items) == 0 {
				return false
			}
			switch k.Key {
			case ui.KeyDown:
				picker.index = (picker.index + 1) % len(items)
				picker.scroll.Y = max(0, float32(picker.index*34)-180)
				return true
			case ui.KeyUp:
				picker.index = (picker.index + len(items) - 1) % len(items)
				picker.scroll.Y = max(0, float32(picker.index*34)-180)
				return true
			case ui.KeyEnter:
				if a.providerStatusFor(items[picker.index].Provider) == "configured" {
					a.choosePickerModel(picker, items[picker.index])
				}
				return true
			}
			return false
		})
		if a.modelsLoading {
			text(c, "Loading models…", 12, muted)
		}
		if a.modelsError != "" {
			text(c, clean(a.modelsError), 12, warning)
		}
		ui.Scroll(c).TrackScroll(&picker.scroll).Height(230).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for i, model := range items {
					status := a.providerStatusFor(model.Provider)
					available := status == "configured"
					chosen := picker.draft.Model.Provider == model.Provider && picker.draft.Model.ModelId == model.ModelId
					row := ui.ButtonBase(c.Key(model.Provider+"/"+model.ModelId)).Label(model.Name+" · "+model.Provider).FillWidth().MinHeight(32).Padding(6, 8).Radius(5).Disabled(!available)
					if chosen || i == picker.index {
						row.Background(selectedFill)
					}
					row.Children(func() {
						mark := "  "
						if chosen {
							mark = "✓ "
						}
						text(c, mark+model.Name, 12, ink).Grow(1).SingleLine()
						label := model.Provider
						if !available {
							label += " · " + status
						}
						text(c, label, 11, muted).SingleLine().MaxWidth(150)
					})
					if row.Clicked() {
						picker.index = i
						a.choosePickerModel(picker, model)
					}
				}
				if a.modelsLoaded && len(items) == 0 {
					text(c, "No matching models", 12, muted)
				}
			})
		})
		if selected := a.modelInfo(picker.draft.Model); selected != nil {
			text(c, clean(selected.Name), 13, ink).FontWeight(600).SingleLine()
			if len(selected.ThinkingLevels) <= 1 {
				text(c, "Thinking not configurable", 12, muted)
			} else {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					text(c, "Thinking", 11, muted)
					for _, level := range selected.ThinkingLevels {
						button := ui.ButtonBase(c.Key(string(level))).Label("Thinking "+string(level)).Padding(5, 6).Radius(5)
						if string(level) == string(picker.draft.ThinkingLevel) {
							button.Background(selectedFill)
						}
						button.Children(func() { text(c, string(level), 11, ink) })
						if button.Clicked() {
							picker.draft.ThinkingLevel = protocol.ThinkingLevel(level)
						}
					}
				})
			}
		}
		if picker.error != "" {
			text(c, clean(picker.error), 12, warning)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if action(c, "Provider settings", picker.pending) {
				picker.open = false
				a.settingsSection = "models"
				a.backendOpen = true
			}
			ui.Box(c).Grow(1)
			if action(c, "Cancel", picker.pending) {
				picker.open = false
			}
			valid := a.modelInfo(picker.draft.Model) != nil && a.providerStatusFor(picker.draft.Model.Provider) == "configured"
			if action(c, "Apply", !valid || picker.pending) {
				a.applyModelSelection(node, p)
			}
		})
	})
}

func (a *app) applyModelSelection(node protocol.LayoutNode, p *pane) {
	picker := &p.modelPicker
	selected := picker.draft
	if p == &a.defaultModelPane {
		picker.pending, picker.error = true, ""
		level := protocol.ThinkingLevel(selected.ThinkingLevel)
		input := protocol.DefaultSettings{Model: &selected.Model, ThinkingLevel: &level}
		go func() {
			updated, err := a.api.SetDefaults(a.ctx, input)
			a.update(func() {
				picker.pending = false
				if err != nil {
					picker.error = err.Error()
					return
				}
				a.modelDefaults = updated
				picker.open = false
				a.rememberModel(selected.Model)
			})
		}()
		return
	}
	if node.ConversationId == nil {
		p.modelDraft = &selected
		picker.open = false
		a.rememberModel(selected.Model)
		return
	}
	id := *node.ConversationId
	picker.pending, picker.error = true, ""
	input := protocol.SelectionChange{Model: selected.Model, ThinkingLevel: protocol.ThinkingLevel(selected.ThinkingLevel), Expected: picker.expected}
	go func() {
		updated, err := a.api.SetConversationModel(a.ctx, id, input)
		a.update(func() {
			picker.pending = false
			if err != nil {
				picker.error = err.Error()
				return
			}
			for i := range a.catalog.Conversations {
				if a.catalog.Conversations[i].Id == id {
					a.catalog.Conversations[i] = updated
					break
				}
			}
			picker.open = false
			a.rememberModel(selected.Model)
		})
	}()
}

func (a *app) rememberModel(model protocol.ModelRef) {
	a.recentModels = slices.DeleteFunc(a.recentModels, func(item protocol.ModelRef) bool { return item == model })
	a.recentModels = append([]protocol.ModelRef{model}, a.recentModels...)
	if len(a.recentModels) > 5 {
		a.recentModels = a.recentModels[:5]
	}
}
