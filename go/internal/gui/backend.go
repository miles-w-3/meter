package gui

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

type BackendControls struct {
	Revision     string
	Dirty        bool
	StartupError error
	CanRedeploy  bool
	Restart      func(context.Context, bool) (protocol.ServerInfo, error)
	Download     func(context.Context) error
}

func (a *app) applyBackendInfo(info protocol.ServerInfo) {
	if info.Generation != nil && (a.backendInfo.Generation == nil || *info.Generation != *a.backendInfo.Generation) {
		a.toolsLoaded = false
	}
	a.backendInfo = info
	a.backendOnline = bool(info.Ready)
	if a.backendOnline {
		a.provider = info.Provider
	}
	a.mayRetry = slices.Contains(info.Capabilities, "retry-submit")
	a.mayAbort = slices.Contains(info.Capabilities, "abort-target")
	a.mayDictate = slices.Contains(info.Capabilities, "dictation")
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
					a.settingsItem(c, "tools", "Tools")
				})
				ui.Box(c).Width(1).Background(theme.Border)
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					switch a.settingsSection {
					case "models":
						a.modelsPanel(c)
					case "tools":
						a.toolsPanel(c)
					default:
						a.backendStatus(c)
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
		if section == "tools" {
			a.refreshTools()
		}
		if section == "models" && !a.modelsLoaded && !a.modelsLoading {
			a.refreshModels()
		}
	}
}

func (a *app) backendStatus(c *ui.Context) {
	info := a.backendInfo
	state := "Unavailable"
	if a.backendOnline {
		state = "Running · " + info.Provider
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
