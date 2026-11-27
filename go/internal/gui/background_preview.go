package gui

// This preview is deliberately gated and uses simulated state. It lets us
// review the layout in the real application before the job protocol exists.
import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type previewBackgroundTask struct {
	name, kind, command, status, latest string
	events                              []string
}

var backgroundStopIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.6"/><rect x="9" y="9" width="6" height="6" rx="0.8" fill="currentColor"/></svg>`))

var previewBackgroundTasks = []previewBackgroundTask{
	{
		name: "Unit tests", kind: "Job", command: "npm test",
		status: "Running · elapsed 2m 14s", latest: "Running integration tests…",
		events: []string{"PASS parser.test.ts", "PASS utils.test.ts", "Running integration tests…"},
	},
	{
		name: "CI status", kind: "Monitor", command: "./scripts/watch-ci.sh",
		status: "Running · elapsed 1m 48s · 3m 12s left", latest: "integration tests failed",
		events: []string{"10:42:01   CI queued", "10:43:18   Unit tests passed", "10:44:06   Integration tests running", "10:45:22   integration tests failed"},
	},
}

const backgroundPreviewRetention = 30 * time.Second

func (a *app) stopBackgroundPreview(p *pane, index int) {
	if p.backgroundPreviewStopped[index] {
		return
	}
	p.backgroundPreviewStopped[index] = true
	if a.ctx == nil || a.update == nil {
		return
	}
	time.AfterFunc(backgroundPreviewRetention, func() {
		if a.ctx.Err() != nil {
			return
		}
		a.update(func() { p.backgroundPreviewCleared[index] = true })
	})
}

func (a *app) backgroundPreview(c *ui.Context, p *pane) {
	if os.Getenv("METER_BACKGROUND_PREVIEW") != "1" || a.provider != "fixture" {
		return
	}
	found := false
	for _, entry := range p.state.Entries() {
		if strings.Contains(entry.Text, "fixture:background-preview") {
			found = true
			break
		}
	}
	if !found || (p.backgroundPreviewCleared[0] && p.backgroundPreviewCleared[1]) {
		return
	}
	count := 0
	for _, stopped := range p.backgroundPreviewStopped {
		if !stopped {
			count++
		}
	}
	ui.Column(c.Key("background-preview")).FillWidth().MinWidth(0).Padding(4).Radius(6).Background(panel).Children(func() {
		button := ui.ButtonBase(c.Key("background-toggle")).Label("Toggle background jobs").FillWidth().MinHeight(32).Padding(4, 6).Radius(5).AlignItems(ui.Center)
		if button.Hovered() {
			button.Background(highlight)
		}
		button.Children(func() {
			glyph := "›"
			if p.backgroundPreviewExpanded {
				glyph = "⌄"
			}
			text(c, glyph, 14, muted).Width(17)
			text(c, "Background", 12, ink)
			ui.Box(c).Grow(1)
			text(c, fmt.Sprintf("%d running", count), 11, muted)
		})
		if button.Clicked() {
			p.backgroundPreviewExpanded = !p.backgroundPreviewExpanded
		}
		if p.backgroundPreviewExpanded {
			ui.Column(c).FillWidth().Padding(0, 6, 4).Children(func() {
				for i, task := range previewBackgroundTasks {
					if p.backgroundPreviewCleared[i] {
						continue
					}
					i, task := i, task
					ui.Column(c.Key(i)).FillWidth().MinWidth(0).Padding(3, 0).Children(func() {
						ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Children(func() {
							row := ui.ButtonBase(c.Key("details")).Label("Toggle "+task.name+" details").Grow(1).MinWidth(0).MinHeight(26).Padding(2, 0).AlignItems(ui.Center).Radius(5)
							if row.Hovered() {
								row.Background(highlight)
							}
							row.Children(func() {
								glyph := "›"
								if p.backgroundPreviewDetail == i+1 {
									glyph = "⌄"
								}
								text(c, glyph, 14, muted).Width(17)
								color := accent
								if p.backgroundPreviewStopped[i] {
									color = muted
								}
								text(c, "●", 10, color).Margin(0, 7, 0, 0)
								text(c, task.name, 12, ink).MinWidth(0).SingleLine()
								ui.Box(c).Grow(1)
								status := task.status
								if p.backgroundPreviewStopped[i] {
									status = "Stopped"
								}
								text(c, status, 11, muted).MinWidth(0).SingleLine()
							})
							if row.Clicked() {
								if p.backgroundPreviewDetail == i+1 {
									p.backgroundPreviewDetail = 0
								} else {
									p.backgroundPreviewDetail = i + 1
								}
							}
							if !p.backgroundPreviewStopped[i] {
								stop := ui.ButtonBase(c.Key("stop")).Label("Stop "+task.name).Tooltip("Stop job").Size(30, 30).Radius(5).Center()
								color := muted
								if stop.Hovered() {
									stop.Background(highlight)
									color = theme.Danger
								}
								stop.Children(func() { ui.Icon(c, backgroundStopIcon).Size(20, 20).TextColor(color) })
								if stop.Clicked() {
									a.stopBackgroundPreview(p, i)
								}
							}
						})
						ui.Column(c).FillWidth().Padding(0, 24, 3, 31).Gap(3).Children(func() {
							text(c, task.kind+" · "+task.command, 11, muted).Font(monoFont).SingleLine()
							if p.backgroundPreviewDetail != i+1 {
								prefix := "Last output: "
								if task.kind == "Monitor" {
									prefix = "Last event: "
								}
								text(c, prefix+task.latest, 11, muted).SingleLine()
							} else {
								label := "Output"
								if task.kind == "Monitor" {
									label = "Events · 4 total"
								}
								text(c, label, 11, ink).Padding(5, 0, 2)
								for _, event := range task.events {
									text(c, event, 11, muted).Font(monoFont).SingleLine()
								}
								text(c, "Working directory · ~/projects/my-app", 11, muted).Padding(7, 0, 0)
							}
						})
					})
				}
			})
		}
	})
}
