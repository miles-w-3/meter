package gui

import (
	"context"
	"slices"
	"time"

	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

type BackendControls struct {
	Revision      string
	Dirty         bool
	StartupError  error
	CanRedeploy   bool
	Restart       func(context.Context, bool) (protocol.ServerInfo, error)
	Download      func(context.Context) error
	LoadAgentPath func() (string, error)
	SaveAgentPath func(string) error
}

func (a *app) applyBackendInfo(info protocol.ServerInfo) {
	if info.Generation != nil && (a.backendInfo.Generation == nil || *info.Generation != *a.backendInfo.Generation) {
		a.toolsLoaded = false
	}
	a.loadReadHeads(info.Id)
	a.backendInfo = info
	a.backendOnline = bool(info.Ready)
	if a.backendOnline {
		a.provider = info.Provider
	}
	a.mayRetry = slices.Contains(info.Capabilities, "retry-submit")
	a.mayAbort = slices.Contains(info.Capabilities, "abort-target")
	a.mayDictate = slices.Contains(info.Capabilities, "dictation")
	if a.update != nil && a.api != nil && a.backendOnline && !a.toolsLoaded {
		a.refreshTools()
	}
}

func backendRevision(revision string, dirty bool) string {
	if revision == "" {
		return "Unavailable"
	}
	if dirty {
		return revision + " (modified source)"
	}
	return revision
}

func (a *app) backendPanel(c *ui.Context) {
	if !a.backendOpen {
		return
	}
	ui.Modal(c.Key("settings"), &a.backendOpen, func() {
		ui.Column(c).Width(760).Gap(14).Children(func() {
			text(c, "Settings", 18, ink).FontWeight(650)
			ui.Row(c).Height(470).Gap(20).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Width(145).Gap(4).Children(func() {
					a.settingsItem(c, "backend", "Backend status")
					a.settingsItem(c, "models", "Models")
					if _, ok := a.claudeProvider(); ok {
						a.settingsItem(c, "claude", "Claude Code")
					}
					a.settingsItem(c, "tools", "Tools")
					a.settingsItem(c, "diffs", "Diffs")
				})
				ui.Box(c).Width(1).Background(theme.Border)
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					switch a.settingsSection {
					case "models":
						a.modelsPanel(c)
					case "claude":
						a.claudePanel(c)
					case "tools":
						a.toolsPanel(c)
					case "diffs":
						a.diffsPanel(c)
					default:
						ui.Scroll(c).Grow(1).MinHeight(0).Children(func() { a.backendStatus(c) })
					}
				})
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if action(c, "Done", false) {
					a.backendOpen = false
				}
			})
		})
	})
}

func (a *app) settingsItem(c *ui.Context, section, label string) {
	selected := a.settingsSection == section || (section == "backend" && a.settingsSection == "")
	row := sidebarRow(c.Key("settings-"+section), label, false)
	if selected {
		row.Background(selectedFill)
	}
	row.Children(func() { text(c, label, 13, ink) })
	if row.Clicked() {
		a.settingsSection = section
		if section == "tools" || section == "diffs" {
			a.refreshTools()
		}
		if section == "models" && !a.modelsLoaded && !a.modelsLoading {
			a.refreshModels()
		}
		if section == "claude" {
			a.claudeStatusAt = time.Time{}
		}
	}
}

func (a *app) backendStatus(c *ui.Context) {
	info := a.backendInfo
	state := "Unavailable"
	if a.backendOnline {
		state = "Running"
		if info.Transport != nil {
			state += " · " + string(*info.Transport)
		}
	}
	revision := ""
	if info.Revision != nil {
		revision = *info.Revision
	}
	modelStatus := "Unavailable"
	modelMissing := false
	if info.Dictation != nil {
		switch string(*info.Dictation) {
		case "ready":
			modelStatus = "Installed"
		case "model_missing":
			modelStatus, modelMissing = "Model missing", true
		case "worker_missing":
			modelStatus = "Worker missing"
		}
	} else if a.mayDictate {
		modelStatus = "Installed"
	}

	ui.Column(c).Gap(12).Children(func() {
		text(c, "Backend status", 16, ink).FontWeight(650)
		text(c, state, 13, muted)
		backendField(c, "Running revision", backendRevision(revision, info.Dirty != nil && *info.Dirty))
		backendField(c, "Bundled revision", backendRevision(a.backend.Revision, a.backend.Dirty))
		backendField(c, "Dictation", modelStatus)
		if modelMissing {
			text(c, "Download the 168 MiB model from Hugging Face for offline dictation.", 12, muted)
			if action(c, "Download model", a.backendPending != "" || a.backend.Download == nil) {
				a.runBackendAction("Download model")
			}
		}
		a.agentPathSettings(c)
		if a.backendNotice != "" {
			text(c, clean(a.backendNotice), 12, muted)
		}
		pending := a.backendPending != ""
		if a.backendConfirm != "" {
			text(c, "Active sessions will reconnect after the backend starts.", 12, muted)
			ui.Row(c).Gap(8).Children(func() {
				if action(c, "Cancel", pending) {
					a.backendConfirm = ""
				}
				if action(c, a.backendConfirm, pending) {
					operation := a.backendConfirm
					a.backendConfirm = ""
					a.runBackendAction(operation)
				}
			})
			return
		}
		ui.Row(c).Gap(8).Children(func() {
			if action(c, "Refresh", pending) {
				a.runBackendAction("Refresh")
			}
			if action(c, "Restart", pending || a.backend.Restart == nil) {
				a.backendConfirm = "Restart backend"
			}
			if action(c, "Redeploy bundled backend", pending || !a.backend.CanRedeploy) {
				a.backendConfirm = "Redeploy bundled backend"
			}
		})
	})
}

func (a *app) agentPathSettings(c *ui.Context) {
	if a.backend.LoadAgentPath == nil || a.backend.SaveAgentPath == nil {
		return
	}
	if !a.agentPathLoaded {
		path, err := a.backend.LoadAgentPath()
		a.agentPathSaved, a.agentPathDraft = path, path
		if err != nil {
			a.agentPathNotice = err.Error()
		}
		a.agentPathLoaded = true
	}
	pending := a.backendPending != ""
	ui.Column(c.Key("agent-path")).Gap(4).MinWidth(0).Children(func() {
		text(c, "Agent PATH", 12, muted)
		if source := a.backendInfo.AgentPathSource; source != nil {
			text(c, "Source: "+clean(*source), 12, muted)
		}
		if current := a.backendInfo.AgentPath; current != nil {
			display := *current
			if display == "" {
				display = "(empty)"
			}
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Children(func() {
				text(c, "Running PATH: "+clean(display), 12, muted).Font(monoFont).Grow(1).MinWidth(0).SingleLine().Tooltip(clean(display))
				content.CopyButton(c, content.CopyButtonOptions{Text: *current, Label: "Copy running PATH", Size: uitheme.ButtonExtraSmall})
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInputBase(c, &a.agentPathDraft).Label("Agent PATH").Placeholder("Automatic").
				Grow(1).MinWidth(0).Padding(7, 9).Radius(6).Background(panel).ReadOnly(pending)
			if action(c, "Save PATH", pending || a.agentPathDraft == a.agentPathSaved) {
				if err := a.backend.SaveAgentPath(a.agentPathDraft); err != nil {
					a.agentPathNotice = err.Error()
				} else {
					a.agentPathSaved = a.agentPathDraft
					a.agentPathNotice = ""
				}
			}
		})
		if a.agentPathNotice != "" {
			text(c, clean(a.agentPathNotice), 12, warning)
		}
		text(c, "Override replaces PATH. Use colon-separated directories; blank means automatic. Restart backend to apply.", 12, muted)
	})
}

func backendField(c *ui.Context, label, value string) {
	ui.Column(c).Gap(2).Children(func() {
		text(c, label, 12, muted)
		text(c, value, 12, ink).Font(monoFont)
	})
}

func (a *app) runBackendAction(operation string) {
	if a.backendPending != "" {
		return
	}
	a.backendPending = operation
	a.backendNotice = map[string]string{
		"Refresh": "Checking backend…", "Download model": "Downloading and verifying model…",
		"Restart backend": "Restarting backend…", "Redeploy bundled backend": "Deploying bundled backend…",
	}[operation]
	go func() {
		var info protocol.ServerInfo
		var err error
		switch operation {
		case "Download model":
			err = a.backend.Download(a.ctx)
			info, _ = a.api.Info(a.ctx)
		case "Restart backend", "Redeploy bundled backend":
			info, err = a.backend.Restart(a.ctx, operation == "Redeploy bundled backend")
		default:
			info, err = a.api.Info(a.ctx)
		}
		a.update(func() {
			a.backendPending = ""
			a.applyBackendInfo(info)
			a.backendNotice = ""
			if err != nil {
				a.backendNotice = err.Error()
			}
			if (operation == "Restart backend" || operation == "Redeploy bundled backend") && a.backendOnline {
				a.reload()
			}
		})
	}()
}
