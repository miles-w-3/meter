package gui

import (
	"fmt"
	"strings"

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
	if p.Source != nil && *p.Source != "" {
		status += " · " + *p.Source
	}
	if p.CatalogSource != nil {
		status += " · models: " + string(*p.CatalogSource)
	}
	ui.Column(c.Key("provider-" + p.Id)).Gap(4).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			text(c, clean(p.Name), 12, ink).Grow(1).SingleLine()
			text(c, clean(status), 11, muted).SingleLine().MaxWidth(250)
			if p.ExternalAuth != nil && action(c, "Refresh login & models", a.modelsLoading) {
				a.refreshProvider(p.Id)
			}
		})
		if p.ExternalAuth != nil && p.Status != protocol.ProviderInfoStatusConfigured {
			text(c, clean(*p.ExternalAuth), 11, muted)
		}
	})
}
