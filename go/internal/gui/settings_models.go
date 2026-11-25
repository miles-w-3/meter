package gui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

func (a *app) refreshModels() {
	if a.modelsLoading {
		return
	}
	a.modelsLoading = true
	a.modelsError = ""
	go func() {
		inventory, err := a.api.Models(a.ctx)
		var defaults protocol.Settings
		if err == nil {
			defaults, err = a.api.Defaults(a.ctx)
		}
		a.update(func() {
			a.modelsLoading = false
			if err != nil {
				a.modelsError = err.Error()
				return
			}
			a.modelsInventory, a.modelDefaults = inventory, defaults
			a.modelsLoaded = true
		})
	}()
}

func (a *app) refreshProvider(id string) {
	if a.modelsLoading {
		return
	}
	a.modelsLoading = true
	a.modelsError = ""
	go func() {
		inventory, err := a.api.RefreshProvider(a.ctx, id)
		a.update(func() {
			a.modelsLoading = false
			if err != nil {
				a.modelsError = err.Error()
				return
			}
			a.modelsInventory = inventory
			a.modelsLoaded = true
		})
	}()
}

func (a *app) setProvider(id string, enabled bool) {
	if a.modelsLoading {
		return
	}
	a.claudeStatusEpoch++
	a.modelsLoading = true
	a.modelsError = ""
	go func() {
		inventory, err := a.api.SetProvider(a.ctx, id, enabled)
		a.update(func() {
			a.modelsLoading = false
			if err != nil {
				a.modelsError = err.Error()
				return
			}
			a.modelsInventory = inventory
			a.modelsLoaded = true
		})
	}()
}

// setProviderSessions applies an optimistic choice, restoring the previous
// value and retaining the error when the backend rejects the update.
func (a *app) setProviderSessions(id string, sessions protocol.ProviderSessions) {
	if a.modelsLoading {
		return
	}
	var previous *protocol.ProviderSessions
	for i := range a.modelsInventory.Providers {
		if a.modelsInventory.Providers[i].Id == id {
			previous = a.modelsInventory.Providers[i].Sessions
			value := sessions
			a.modelsInventory.Providers[i].Sessions = &value
		}
	}
	a.claudeStatusEpoch++
	a.modelsLoading = true
	a.modelsError = ""
	go func() {
		inventory, err := a.api.SetProviderSessions(a.ctx, id, sessions)
		a.update(func() {
			a.modelsLoading = false
			if err != nil {
				for i := range a.modelsInventory.Providers {
					if a.modelsInventory.Providers[i].Id == id {
						a.modelsInventory.Providers[i].Sessions = previous
					}
				}
				a.modelsError = err.Error()
				a.claudeStatusAt = time.Time{}
				return
			}
			a.modelsInventory = inventory
			a.modelsLoaded = true
		})
	}()
}

func (a *app) modelsPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			text(c, "Models", 16, ink).FontWeight(650).Grow(1)
			if action(c, "Refresh", a.modelsLoading) {
				a.refreshModels()
			}
		})
		if a.modelsLoading {
			text(c, "Loading provider and model inventory…", 12, muted)
		}
		if a.modelsError != "" {
			text(c, clean(a.modelsError), 12, warning)
		}
		if !a.modelsLoaded {
			return
		}
		defaultModel := "Not configured"
		if m := a.modelDefaults.Model; m != nil {
			defaultModel = m.Provider + " / " + m.ModelId
		}
		thinking := "Model default"
		if a.modelDefaults.ThinkingLevel != nil {
			thinking = string(*a.modelDefaults.ThinkingLevel)
		}
		backendField(c, "Default for new conversations", defaultModel+" · "+thinking)
		a.modelChip(c, protocol.LayoutNode{}, &a.defaultModelPane, false)
		text(c, "Existing Pi credentials are detected on the backend. Choose a default here or override it in each conversation.", 12, muted)
		ui.TextInputBase(c, &a.modelsFilter).Placeholder("Filter providers and models").Label("Filter providers and models").Padding(7, 9).Radius(6).Background(panel)
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(9).Children(func() {
				filter := strings.ToLower(strings.TrimSpace(a.modelsFilter))
				sectionLabel(c, "PROVIDERS")
				providerCount := 0
				for _, p := range a.modelsInventory.Providers {
					if filter != "" && !strings.Contains(strings.ToLower(p.Id+" "+p.Name), filter) {
						continue
					}
					providerCount++
					a.providerStatus(c, p)
				}
				if providerCount == 0 {
					text(c, "No matching providers", 12, muted)
				}
				sectionLabel(c, "MODELS")
				shown, total := 0, 0
				for _, m := range a.modelsInventory.Models {
					if filter != "" && !strings.Contains(strings.ToLower(m.Provider+" "+m.ModelId+" "+m.Name), filter) {
						continue
					}
					total++
					if shown >= 100 {
						continue
					}
					shown++
					ui.Row(c.Key(m.Provider + "/" + m.ModelId)).Gap(8).Children(func() {
						text(c, clean(m.Name), 12, ink).Grow(1).SingleLine()
						text(c, clean(m.Provider), 11, muted).NoWrap()
					})
				}
				if total == 0 {
					text(c, "No matching models", 12, muted)
				} else if total > shown {
					text(c, fmt.Sprintf("Showing %d of %d models. Filter to narrow the list.", shown, total), 12, muted)
				}
			})
		})
	})
}

func (a *app) providerStatus(c *ui.Context, p protocol.ProviderInfo) {
	status := string(p.Status)
	if p.Enabled != nil && !*p.Enabled {
		status = "disabled"
	}
	if p.Source != nil && *p.Source != "" && (p.Enabled == nil || *p.Enabled) {
		status += " · " + *p.Source
	}
	if p.CatalogSource != nil {
		status += " · models: " + string(*p.CatalogSource)
	}
	ui.Column(c.Key("provider-" + p.Id)).Gap(4).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			text(c, clean(p.Name), 12, ink).Grow(1).SingleLine()
			text(c, clean(status), 11, muted).SingleLine().MaxWidth(250)
			if p.Enabled != nil {
				label := "Enable"
				if *p.Enabled {
					label = "Disable"
				}
				if action(c, label, a.modelsLoading) {
					a.setProvider(p.Id, !*p.Enabled)
				}
			}
			if p.ExternalAuth != nil && (p.Enabled == nil || *p.Enabled) && action(c, "Refresh login & models", a.modelsLoading) {
				a.refreshProvider(p.Id)
			}
		})
		if p.ExternalAuth != nil && (p.Enabled == nil || *p.Enabled) && p.Status != protocol.ProviderInfoStatusConfigured {
			text(c, clean(*p.ExternalAuth), 11, muted)
		}
	})
}

var (
	keepaliveChoices = []int{15, 30, 60, 120, 240}
	toolWaitChoices  = []int{15, 30, 60}
	parkedChoices    = []int{1, 2, 3, 4, 5, 6}
)

// providerSessions shows how long a provider keeps each conversation's
// session process, and how many it keeps.
func (a *app) providerSessions(c *ui.Context, id string, current protocol.ProviderSessions, blocked bool) {
	ui.Column(c.Key("sessions")).Gap(6).Padding(4, 0, 2, 12).Children(func() {
		a.sessionLimit(c, "keepalive", "Keep idle sessions", current.KeepaliveMinutes, keepaliveChoices, blocked, minutesLabel, func(v int) {
			next := current
			next.KeepaliveMinutes = v
			a.setProviderSessions(id, next)
		})
		a.sessionLimit(c, "tool-wait", "Keep sessions waiting on a tool", current.ToolWaitMinutes, toolWaitChoices, blocked, minutesLabel, func(v int) {
			next := current
			next.ToolWaitMinutes = v
			a.setProviderSessions(id, next)
		})
		a.sessionLimit(c, "parked", "Maximum parked sessions", current.MaxParked, parkedChoices, blocked, func(n int) string { return fmt.Sprint(n) }, func(v int) {
			next := current
			next.MaxParked = v
			a.setProviderSessions(id, next)
		})
		text(c, "This limits parked processes, not active runs. A parked session may be retired while waiting on a tool if the pool is full.", 11, muted)
	})
}

// sessionLimit shows one limit as a choice, keeping a saved value that is not
// among the usual ones.
func (a *app) sessionLimit(c *ui.Context, key, label string, current int, choices []int, blocked bool, format func(int) string, save func(int)) {
	if !slices.Contains(choices, current) {
		choices = append(slices.Clone(choices), current)
		slices.Sort(choices)
	}
	options := make([]string, len(choices))
	for i, choice := range choices {
		options[i] = format(choice)
	}
	selected := format(current)
	ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		text(c, label, 12, ink).Grow(1)
		before := selected
		ui.Select(c, &selected, options).Label(label).Disabled(a.modelsLoading || blocked)
		if selected != before {
			if i := slices.Index(options, selected); i >= 0 {
				save(choices[i])
			}
		}
	})
}

func minutesLabel(minutes int) string {
	switch {
	case minutes == 60:
		return "1 hour"
	case minutes%60 == 0:
		return fmt.Sprintf("%d hours", minutes/60)
	default:
		return fmt.Sprintf("%d min", minutes)
	}
}
