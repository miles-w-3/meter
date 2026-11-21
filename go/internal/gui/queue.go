package gui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func queuedMessages(c *ui.Context, a *app, p *pane, conversation string) {
	if p.state.Current.Queued == nil || len(*p.state.Current.Queued) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for index, queued := range *p.state.Current.Queued {
			hasLater := index < len(*p.state.Current.Queued)-1
			ui.Row(c.Key(queued.Id)).FillWidth().Gap(8).Padding(7, 10).Radius(8).Background(panel).AlignItems(ui.Center).Children(func() {
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
					text(c, clean(content), 13, ink).NoWrap().Ellipsis("…")
				})
				if queued.Mode == "followUp" {
					button := ui.ButtonBase(c).Label("Steer queued message").Tooltip("Steer the active run").
						Padding(5, 9).Radius(6).Background(highlight).Disabled(!p.connected || p.state.Current.Status != "running" || p.queuePending != "")
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
				button := ui.ButtonBase(c).Label(label).Tooltip(tooltip).
					Size(28, 28).Radius(6).Background(highlight).Center().Disabled(!p.connected || p.queuePending != "")
				button.Children(func() { text(c, symbol, 17, ink) })
				if button.Clicked() {
					a.recallQueued(p, conversation, queued, hasLater)
				}
			})
		}
	})
}

func (a *app) recallQueued(p *pane, conversation string, queued protocol.QueuedMessage, clearDraft bool) {
	if p.queuePending != "" || !p.connected {
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
