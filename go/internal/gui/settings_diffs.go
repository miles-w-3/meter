package gui

import (
	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

func (a *app) diffsPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(16).Children(func() {
		text(c, "Diffs", 16, ink).FontWeight(650)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			text(c, "Layout", 13, ink).Grow(1)
			previous := a.diffLayout
			ui.Segmented(c, &a.diffLayout, "Split", "Unified").Disabled(a.toolsPending || !a.toolsLoaded)
			if previous != a.diffLayout {
				value := protocol.ToolSettingsChangeDiffLayout("split")
				if a.diffLayout == 1 {
					value = "unified"
				}
				a.updateTools(protocol.ToolSettingsChange{DiffLayout: &value})
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			text(c, "Inline highlighting", 13, ink).Grow(1)
			if a.diffInline == "" {
				a.diffInline = "Words"
			}
			previous := a.diffInline
			ui.Select(c, &a.diffInline, []string{"Words", "Characters", "Off"}).Disabled(a.toolsPending || !a.toolsLoaded)
			if previous != a.diffInline {
				value := protocol.ToolSettingsChangeDiffInline("words")
				if a.diffInline == "Characters" {
					value = "characters"
				} else if a.diffInline == "Off" {
					value = "off"
				}
				a.updateTools(protocol.ToolSettingsChange{DiffInline: &value})
			}
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				text(c, "Computed diffs for Bash", 13, ink).FontWeight(600)
				text(c, "Collect best-effort snapshots around future Bash calls.", 12, muted)
			})
			if !a.toolsLoaded {
				text(c, "Unavailable", 12, muted)
			} else if action(c, toolSwitchLabel(a.toolsStatus.ComputedDiffs), a.toolsPending || !a.backendOnline) {
				enabled := !a.toolsStatus.ComputedDiffs
				a.updateTools(protocol.ToolSettingsChange{ComputedDiffs: &enabled})
			}
		})
		if a.toolsError != "" {
			text(c, clean(a.toolsError), 12, warning)
		}
		text(c, "Snapshots cover the working directory only. Generated directories, binary and large files may be excluded.", 12, muted)
	})
}
