package gui

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

func (a *app) refreshTools() {
	a.loadToolSettings(a.api.Tools)
}

func (a *app) updateTools(change protocol.ToolSettingsChange) {
	a.loadToolSettings(func(ctx context.Context) (protocol.ToolsStatus, error) {
		return a.api.UpdateTools(ctx, change)
	})
}

func (a *app) loadToolSettings(load func(context.Context) (protocol.ToolsStatus, error)) {
	if a.toolsPending || !a.backendOnline || !slices.Contains(a.backendInfo.Capabilities, "tool-settings") {
		return
	}
	a.toolsPending, a.toolsError = true, ""
	go func() {
		status, err := load(a.ctx)
		a.update(func() {
			a.toolsPending = false
			if err != nil {
				a.toolsError = err.Error()
				if a.toolsLoaded {
					a.applyDiffSettings(a.toolsStatus)
				}
				return
			}
			a.toolsStatus, a.toolsLoaded = status, true
			a.applyDiffSettings(status)
		})
	}()
}

func (a *app) applyDiffSettings(status protocol.ToolsStatus) {
	a.hideThinking = status.ShowThinking != nil && !*status.ShowThinking
	a.diffLayout = 0
	if status.DiffLayout == "unified" {
		a.diffLayout = 1
	}
	switch status.DiffInline {
	case "characters":
		a.diffInline = "Characters"
	case "off":
		a.diffInline = "Off"
	default:
		a.diffInline = "Words"
	}
}

func (a *app) toolsPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			text(c, "Tools", 16, ink).FontWeight(650).Grow(1)
			if action(c, "Refresh", a.toolsPending || !a.backendOnline || !slices.Contains(a.backendInfo.Capabilities, "tool-settings")) {
				a.refreshTools()
			}
		})
		if !a.backendOnline {
			text(c, "Backend unavailable", 13, muted)
			return
		}
		if !slices.Contains(a.backendInfo.Capabilities, "tool-settings") {
			text(c, "The running backend needs an update to configure tools.", 13, muted)
			return
		}
		if a.toolsError != "" {
			text(c, clean(a.toolsError), 12, warning)
		}
		if !a.toolsLoaded {
			if a.toolsPending {
				text(c, "Loading tools…", 13, muted)
			}
			return
		}
		if fetch := a.toolsStatus.WebFetch; fetch != nil {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				text(c, "Web Fetch", 14, ink).FontWeight(600).Grow(1)
				if action(c, toolSwitchLabel(fetch.Enabled), a.toolsPending) {
					var change protocol.ToolSettingsChange
					change.WebFetch = new(struct {
						Enabled *bool `json:"enabled,omitempty"`
					})
					enabled := !fetch.Enabled
					change.WebFetch.Enabled = &enabled
					a.updateTools(change)
				}
			})
			text(c, "Fetch public URLs directly. Pages are untrusted and may contain instructions.", 12, muted)
		}
		web := a.toolsStatus.WebSearch
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			text(c, "Web Search", 14, ink).FontWeight(600).Grow(1)
			if action(c, toolSwitchLabel(web.Enabled), a.toolsPending) {
				var change protocol.ToolSettingsChange
				change.WebSearch = new(struct {
					Enabled  *bool                          `json:"enabled,omitempty"`
					Provider *protocol.SearchProviderChange `json:"provider,omitempty"`
				})
				enabled := !web.Enabled
				change.WebSearch.Enabled = &enabled
				a.updateTools(change)
			}
		})
		text(c, "Search falls back to the next enabled provider when one is unavailable. Each provider receives the search query.", 12, muted)
		for _, provider := range web.Providers {
			name, key := searchProviderLabel(provider.Id)
			ui.Row(c.Key(string(provider.Id))).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					text(c, name, 13, ink).FontWeight(600)
					text(c, searchProviderStatus(provider, key), 12, muted)
				})
				if action(c, toolSwitchLabel(provider.Enabled), a.toolsPending) {
					var change protocol.ToolSettingsChange
					change.WebSearch = new(struct {
						Enabled  *bool                          `json:"enabled,omitempty"`
						Provider *protocol.SearchProviderChange `json:"provider,omitempty"`
					})
					change.WebSearch.Provider = &protocol.SearchProviderChange{
						Id: protocol.SearchProviderChangeId(provider.Id), Enabled: !provider.Enabled,
					}
					a.updateTools(change)
				}
			})
		}
	})
}

func toolSwitchLabel(enabled bool) string {
	if enabled {
		return "Turn off"
	}
	return "Turn on"
}

func searchProviderLabel(id protocol.SearchProviderStatusId) (string, string) {
	switch string(id) {
	case "brave":
		return "Brave", "BRAVE_SEARCH_API_KEY"
	case "exa":
		return "Exa", "EXA_API_KEY"
	case "firecrawl":
		return "Firecrawl", "FIRECRAWL_API_KEY"
	default:
		return string(id), ""
	}
}

func searchProviderStatus(provider protocol.SearchProviderStatus, key string) string {
	switch string(provider.Status) {
	case "disabled":
		return "Off"
	case "missing_key":
		return "Set " + key + " on the backend"
	case "storage_rights_required":
		return "Requires a Brave plan with storage rights"
	case "rate_limited":
		return "Rate limited"
	case "unavailable":
		return "Temporarily unavailable"
	case "auth_error":
		return "Check API credentials or access"
	case "quota_exhausted":
		return "Quota exhausted"
	default:
		return "Ready"
	}
}
