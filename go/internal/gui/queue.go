package gui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

// Only adjacent events from the same monitor are grouped: never reorder
// notifications around a user message or a job-exit notice.
type queuedGroup struct {
	items      []protocol.QueuedMessage
	firstIndex int
}

func groupQueued(items []protocol.QueuedMessage) []queuedGroup {
	var groups []queuedGroup
	for i, item := range items {
		if item.Background != nil && item.Background.Notice == "output" && item.Background.JobId != "" && len(groups) > 0 {
			last := &groups[len(groups)-1]
			prev := last.items[0].Background
			if prev != nil && prev.Notice == "output" && prev.JobId == item.Background.JobId {
				last.items = append(last.items, item)
				continue
			}
		}
		groups = append(groups, queuedGroup{items: []protocol.QueuedMessage{item}, firstIndex: i})
	}
	return groups
}

func latestUserQueued(items []protocol.QueuedMessage) (protocol.QueuedMessage, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Background == nil {
			return items[i], true
		}
	}
	return protocol.QueuedMessage{}, false
}

func queuedBackgroundText(item protocol.QueuedMessage) string {
	content := item.Text
	if i := strings.Index(content, "\nOutput events:\n"); i >= 0 {
		return content[i+len("\nOutput events:\n"):]
	}
	if i := strings.Index(content, "\nStatus: "); i >= 0 {
		return content[i+1:]
	}
	return content
}

func queuedMessages(c *ui.Context, a *app, p *pane, conversation string) {
	if p.state.Current.Queued == nil || len(*p.state.Current.Queued) == 0 {
		return
	}
	items := *p.state.Current.Queued
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for _, group := range groupQueued(items) {
			group := group
			queued := group.items[0]
			if queued.Background != nil {
				background := queued.Background
				ui.Column(c.Key("background:"+queued.Id)).FillWidth().MinWidth(0).Padding(7, 10).Radius(8).Background(panel).Children(func() {
					label := "Background event"
					if background.Mode != nil && *background.Mode == "monitor" {
						label = "Monitor event"
					}
					if background.Notice == "exit" {
						label = "Background job ended"
					}
					if background.Label != "" {
						label += " · " + clean(background.Label)
					}
					if len(group.items) > 1 {
						label += fmt.Sprintf(" · %d batches", len(group.items))
					}
					button := ui.ButtonBase(c.Key("expand")).Label("Toggle " + label).FillWidth().MinHeight(24).AlignItems(ui.Center).Radius(5)
					if button.Hovered() {
						button.Background(highlight)
					}
					button.Children(func() {
						glyph := "›"
						if p.queueExpanded[queued.Id] {
							glyph = "⌄"
						}
						text(c, glyph+"  "+label, 11, muted).Grow(1).MinWidth(0).SingleLine()
					})
					if button.Clicked() {
						if p.queueExpanded == nil {
							p.queueExpanded = make(map[string]bool)
						}
						p.queueExpanded[queued.Id] = !p.queueExpanded[queued.Id]
					}
					if p.queueExpanded[queued.Id] {
						limit := min(len(group.items), 6)
						for _, item := range group.items[:limit] {
							text(c, clean(queuedBackgroundText(item)), 12, ink).FillWidth().Padding(4, 0, 0, 14)
						}
						if len(group.items) > limit {
							text(c, fmt.Sprintf("%d additional batches", len(group.items)-limit), 11, muted).Padding(4, 0, 0, 14)
						}
					} else {
						preview := strings.Split(clean(queuedBackgroundText(group.items[len(group.items)-1])), "\n")[0]
						text(c, preview, 12, ink).FillWidth().SingleLine().Padding(1, 0, 0, 14)
					}
				})
				continue
			}
			hasLater := false
			for _, later := range items[group.firstIndex+1:] {
				if later.Background == nil {
					hasLater = true
					break
				}
			}
			ui.Row(c.Key(queued.Id)).FillWidth().MinWidth(0).Gap(8).Padding(7, 10).Radius(8).Background(panel).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					label := "Queued"
					if queued.Mode == "steer" {
						label = "Steering"
					}
					text(c, label, 11, muted)
					content := queued.Text
					if queued.Images > 0 {
						content += fmt.Sprintf("  ·  %d image(s)", queued.Images)
					}
					text(c, clean(content), 13, ink).FillWidth()
				})
				if queued.Mode == "followUp" {
					button := ui.ButtonBase(c).Label("Steer queued message").Tooltip("Steer the active run").Padding(5, 9).Shrink(0).Radius(6).Background(highlight).Disabled(!p.connected || p.state.Current.Status != "running" || p.queuePending != "")
					button.Children(func() { text(c, "Steer", 12, ink) })
					if button.Clicked() {
						go func() {
							err := a.api.SteerQueued(a.ctx, conversation, queued.Id)
							a.update(func() {
								if err != nil {
									p.notice = err.Error()
								}
							})
						}()
					}
				}
				label, tooltip, symbol := "Return queued message to draft", "Return to draft", "↓"
				if hasLater {
					label, tooltip, symbol = "Return queued message and clear later messages", "Return to draft and clear newer queue", "⇊"
				}
				button := ui.ButtonBase(c).Label(label).Tooltip(tooltip).Size(28, 28).Shrink(0).Radius(6).Background(highlight).Center().Disabled(!p.connected || p.queuePending != "")
				button.Children(func() { text(c, symbol, 17, ink) })
				if button.Clicked() {
					a.recallQueued(p, conversation, queued, hasLater)
				}
			})
		}
	})
}

func (a *app) recallQueued(p *pane, conversation string, queued protocol.QueuedMessage, clearDraft bool) {
	if p.queuePending != "" || !p.connected || queued.Background != nil {
		return
	}
	if !clearDraft && queued.Images+len(p.images) > 4 {
		p.notice = "Remove an image before returning this message to the draft."
		return
	}
	p.queuePending = queued.Id
	go func() {
		message, err := a.api.RecallQueued(a.ctx, conversation, queued.Id)
		if err != nil && client.Uncertain(err) {
			message, err = a.api.RecallQueued(a.ctx, conversation, queued.Id)
		}
		a.update(func() {
			p.queuePending = ""
			if err != nil {
				p.notice = err.Error()
				return
			}
			if clearDraft || message.ClearedAfter > 0 {
				p.draft = ""
				p.images = nil
			}
			p.restoreDraft(message.Content, &message.Images)
			p.notice = ""
		})
	}()
}
