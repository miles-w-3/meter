package gui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

const claudeProviderID = "pi-claude-cli"

// Only show the page once the backend confirms the provider is enabled.
func (a *app) claudeProvider() (protocol.ProviderInfo, bool) {
	for _, p := range a.modelsInventory.Providers {
		if p.Id == claudeProviderID && p.Enabled != nil && *p.Enabled {
			return p, true
		}
	}
	return protocol.ProviderInfo{}, false
}

// The UI status is advisory: the backend independently enforces the guard.
// Poll while this page is visible so background conversations and other
// clients can unblock the controls without reopening Settings.
func (a *app) pollClaudeStatus(c *ui.Context) {
	const interval = 3 * time.Second
	if a.api == nil || a.ctx == nil || a.update == nil {
		return
	}
	if !a.claudeStatusLoading && !a.modelsLoading && time.Since(a.claudeStatusAt) >= interval {
		a.claudeStatusAt = time.Now()
		a.claudeStatusLoading = true
		epoch := a.claudeStatusEpoch
		go func() {
			inventory, err := a.api.Models(a.ctx)
			a.update(func() {
				a.claudeStatusLoading = false
				if epoch != a.claudeStatusEpoch {
					return
				}
				a.claudeStatusError = err != nil
				if err == nil {
					a.modelsInventory = inventory
					a.modelsLoaded = true
				}
			})
		}()
	}
	c.After(interval)
}

func (a *app) claudePanel(c *ui.Context) {
	a.pollClaudeStatus(c)
	ui.Column(c).Fill().Gap(10).Children(func() {
		text(c, "Claude Code", 16, ink).FontWeight(650)
		if a.modelsError != "" {
			text(c, clean(a.modelsError), 12, warning)
		}
		p, enabled := a.claudeProvider()
		if !enabled {
			text(c, "Claude Code is disabled. Enable it in Models to configure its sessions.", 12, muted)
			return
		}
		a.providerStatus(c, p)
		if p.Sessions == nil {
			text(c, "Session settings are unavailable on this backend.", 12, muted)
			return
		}
		blocked := a.claudeStatusError || p.SessionsBusy == nil || *p.SessionsBusy
		if blocked {
			ui.Row(c).FillWidth().Padding(8).Radius(6).Background(panel).Children(func() {
				if a.claudeStatusError || p.SessionsBusy == nil {
					text(c, "Unable to verify conversation activity. Refresh or reconnect before changing session limits.", 12, warning)
				} else {
					text(c, "Wait for active conversations, tool calls, and queued messages to finish before changing session limits.", 12, warning)
				}
			})
		}
		a.providerSessions(c, p.Id, *p.Sessions, blocked)
	})
}
