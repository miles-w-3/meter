package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
)

var backgroundStopIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.6"/><rect x="9" y="9" width="6" height="6" rx="0.8" fill="currentColor"/></svg>`))

// The daemon owns jobs. Polling is scoped to the lifetime of this pane's
// subscription; closing a pane never stops its jobs.
func (a *app) watchBackground(ctx context.Context, paneID uuid.UUID, p *pane, conversation string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		jobs, err := a.api.Background(ctx, conversation)
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			if a.panes[paneID] != p || p.conversation != conversation {
				return
			}
			if err != nil {
				p.backgroundError = "Background jobs unavailable"
				return
			}
			p.backgroundError = ""
			p.backgroundJobs = jobs
		})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func backgroundDuration(start int64, end *int64) string {
	until := time.Now().UnixMilli()
	if end != nil {
		until = *end
	}
	d := time.Duration(max(int64(0), until-start)) * time.Millisecond
	if d >= time.Hour {
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func lastBackgroundLine(output string) string {
	lines := strings.Split(strings.TrimSpace(clean(output)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		return "No output yet"
	}
	return lines[len(lines)-1]
}

func (a *app) backgroundView(c *ui.Context, p *pane) {
	if len(p.backgroundJobs) == 0 && p.backgroundError == "" {
		return
	}
	count := 0
	for _, job := range p.backgroundJobs {
		if job.Status == "running" {
			count++
		}
	}
	ui.Column(c.Key("background-jobs")).FillWidth().MinWidth(0).Padding(4).Radius(6).Background(panel).Children(func() {
		button := ui.ButtonBase(c.Key("background-toggle")).Label("Toggle background jobs").FillWidth().MinHeight(32).Padding(4, 6).Radius(5).AlignItems(ui.Center)
		if button.Hovered() {
			button.Background(highlight)
		}
		button.Children(func() {
			glyph := "›"
			if p.backgroundExpanded {
				glyph = "⌄"
			}
			text(c, glyph, 14, muted).Width(17)
			text(c, "Background", 12, ink)
			ui.Box(c).Grow(1)
			if p.backgroundError != "" {
				text(c, p.backgroundError, 11, warning)
			} else {
				text(c, fmt.Sprintf("%d running", count), 11, muted)
			}
		})
		if button.Clicked() {
			p.backgroundExpanded = !p.backgroundExpanded
		}
		if p.backgroundExpanded {
			ui.Column(c).FillWidth().Padding(0, 6, 4).Children(func() {
				for _, job := range p.backgroundJobs {
					job := job
					ui.Column(c.Key(job.ID)).FillWidth().MinWidth(0).Padding(3, 0).Children(func() {
						ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Children(func() {
							row := ui.ButtonBase(c.Key("details")).Label("Toggle "+job.Label+" details").Grow(1).MinWidth(0).MinHeight(26).Padding(2, 0).AlignItems(ui.Center).Radius(5)
							if row.Hovered() {
								row.Background(highlight)
							}
							row.Children(func() {
								glyph := "›"
								if p.backgroundDetail == job.ID {
									glyph = "⌄"
								}
								text(c, glyph, 14, muted).Width(17)
								color := accent
								if job.Status != "running" {
									color = muted
								}
								text(c, "●", 10, color).Margin(0, 7, 0, 0)
								text(c, clean(job.Label), 12, ink).MinWidth(0).SingleLine()
								ui.Box(c).Grow(1)
								status := job.Status + " · elapsed " + backgroundDuration(job.StartedAt, job.EndedAt)
								if job.DeadlineAt != nil && job.Status == "running" {
									now := time.Now().UnixMilli()
									status += " · " + backgroundDuration(now, job.DeadlineAt) + " left"
								}
								if job.ExitCode != nil && job.Status == "exited" {
									status += fmt.Sprintf(" · exit %d", *job.ExitCode)
								}
								text(c, status, 11, muted).MinWidth(0).SingleLine()
							})
							if row.Clicked() {
								if p.backgroundDetail == job.ID {
									p.backgroundDetail = ""
								} else {
									p.backgroundDetail = job.ID
								}
							}
							if job.Status == "running" {
								stopping := p.backgroundStopping[job.ID]
								stop := ui.ButtonBase(c.Key("stop")).Label("Stop "+job.Label).Tooltip("Stop job").Size(30, 30).Radius(5).Center().Disabled(stopping)
								color := muted
								if stop.Hovered() {
									stop.Background(highlight)
									color = theme.Danger
								}
								stop.Children(func() { ui.Icon(c, backgroundStopIcon).Size(20, 20).TextColor(color) })
								if stop.Clicked() && !stopping {
									if p.backgroundStopping == nil {
										p.backgroundStopping = make(map[string]bool)
									}
									p.backgroundStopping[job.ID] = true
									conversation := p.conversation
									go func() {
										err := a.api.StopBackground(a.ctx, conversation, job.ID)
										a.update(func() {
											delete(p.backgroundStopping, job.ID)
											if err != nil {
												p.notice = "Stop background job: " + err.Error()
											}
										})
									}()
								}
							}
						})
						ui.Column(c).FillWidth().Padding(0, 24, 3, 31).Gap(3).Children(func() {
							kind := "Job"
							if job.Mode == "monitor" {
								kind = "Monitor"
							}
							text(c, kind+" · "+clean(job.Command), 11, muted).Font(monoFont).SingleLine()
							if p.backgroundDetail != job.ID {
								text(c, "Last output: "+lastBackgroundLine(job.Output), 11, muted).SingleLine()
							} else {
								label := "Recent output"
								if job.Mode == "monitor" {
									label += fmt.Sprintf(" · %d events", job.Events)
									if job.DroppedEvents > 0 {
										label += fmt.Sprintf(" · %d dropped", job.DroppedEvents)
									}
								}
								text(c, label, 11, ink).Padding(5, 0, 2)
								lines := strings.Split(strings.TrimSpace(clean(job.Output)), "\n")
								if len(lines) > 6 {
									lines = lines[len(lines)-6:]
								}
								for _, line := range lines {
									text(c, line, 11, muted).Font(monoFont).SingleLine()
								}
								text(c, "Working directory · "+clean(job.Cwd), 11, muted).Padding(7, 0, 0)
							}
						})
					})
				}
			})
		}
	})
}
