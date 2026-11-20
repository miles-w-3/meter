package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Cyenoch/mygoui"
	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
)

func ghost(c *ui.Context, label string, disabled bool) bool {
	return mygoui.Button(c, mygoui.ButtonOptions{Label: label, Variant: mygoui.ButtonGhost, Size: uitheme.ButtonExtraSmall, Disabled: disabled}).Clicked()
}

// updateMeter keeps the level in the pane model. During generation it estimates
// throughput from streamed text; otherwise it displays estimated context fill.
func (p *pane) updateMeter(now time.Time) {
	usage := p.state.Current.ContextUsage
	if !p.connected {
		p.meterLevel = 0
	} else if p.state.Current.Status == "running" {
		length := 0
		if p.state.Current.Partial != nil {
			length = len([]rune(*p.state.Current.Partial))
		}
		if elapsed := now.Sub(p.meterAt).Seconds(); !p.meterAt.IsZero() && elapsed > 0 && length > p.partialLength {
			// Roughly four characters per token, normalized to 80 tokens/sec.
			p.meterLevel = min(1, float32(float64(length-p.partialLength)/elapsed/320))
		} else if length < p.partialLength {
			p.meterLevel = 0
		}
		p.partialLength = length
	} else {
		p.partialLength, p.meterLevel = 0, 0
		if usage != nil && usage.Window > 0 {
			p.meterLevel = min(1, max(0, float32(usage.Tokens)/float32(usage.Window)))
		}
	}
	p.meterAt = now
}

func meter(c *ui.Context, p *pane) {
	const segments = 40
	column := ui.Column(c.Key("meter")).Width(6).MinWidth(6).Gap(2).Justify(ui.End)
	tooltip := "Context usage unavailable"
	if usage := p.state.Current.ContextUsage; p.connected && usage != nil && usage.Window > 0 {
		tooltip = fmt.Sprintf("Estimated context · %d of %d tokens", usage.Tokens, usage.Window)
	}
	if p.connected && p.state.Current.Status == "running" {
		tooltip = "Estimated streaming throughput"
	}
	column.Tooltip(tooltip).Children(func() {
		level := p.meterLevel
		if p.connected && p.state.Current.Status == "running" && !c.Preferences().ReduceMotion {
			column.Loop("throughput", time.Second, ui.Linear)
			level *= max(0, 1-float32(c.Now().Sub(p.meterAt).Seconds()))
		}
		fill := column.Animate("segments", level*segments, 120*time.Millisecond)
		for i := range segments {
			filled := min(1, max(0, fill-float32(segments-1-i)))
			ui.Box(c.Key(i)).Size(6, 3).Radius(1).Background(theme.Border.Mix(accent, filled))
		}
	})
}

type message struct {
	id, label, text string
	tool, user      bool
	succeeded       bool
}

func (a *app) transcript(c *ui.Context, p *pane, tint ui.Color) {
	entries := p.state.Entries()
	messages := make([]message, 0, len(entries)+len(p.state.Current.Tools)+1)
	completed := make(map[string]bool)
	for _, entry := range entries {
		if entry.Text == "" && entry.ToolName == nil {
			continue
		}
		label := strings.TrimPrefix(entry.Kind, "pi.")
		if entry.ToolName == nil {
			messages = append(messages, message{id: entry.Id, label: label, text: clean(entry.Text), user: label == "user"})
			continue
		}
		completed[entry.Id] = true
		messages = append(messages, message{id: entry.Id, label: *entry.ToolName, text: clean(entry.Text), tool: true,
			succeeded: entry.ToolSucceeded != nil && *entry.ToolSucceeded})
	}
	for i, tool := range p.state.Current.Tools {
		key := fmt.Sprintf("live:%s:%d", tool.Name, i)
		if tool.CallId != nil {
			key = "call:" + *tool.CallId
		}
		if tool.TaskId != nil {
			key = "tool:" + *tool.TaskId
		}
		if tool.EntryId != nil {
			if p.expandedTools[key] {
				p.expandedTools[*tool.EntryId] = true
				delete(p.expandedTools, key)
			}
			key = *tool.EntryId
			if completed[key] {
				continue
			}
		}
		output := ""
		if tool.Output != nil {
			output = clean(*tool.Output)
		}
		messages = append(messages, message{id: key, label: tool.Name + " · " + tool.Status, text: output, tool: true})
	}
	if partial := p.state.Current.Partial; partial != nil {
		messages = append(messages, message{id: "partial", label: "assistant · streaming", text: clean(*partial) + " ▍"})
	}
	if len(messages) == 0 {
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			text(c, "Messages appear here", 14, muted)
		})
		return
	}
	p.list.Key = func(row int) any { return messages[row].id }
	p.list.FollowEnd = true
	ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Background(background).Clip().Children(func() {
		viewport := ui.List(c, &p.list, len(messages), func(i int) {
			msg := messages[i]
			if msg.tool {
				toolRow(c, p, msg)
				return
			}
			row := ui.Row(c).MinWidth(0).AlignItems(ui.Stretch)
			if msg.user {
				row.Background(panel).Radius(5)
			}
			row.Children(func() {
				ui.Row(c).Width(56).MinWidth(56).Padding(9, 12).Justify(ui.End).Children(func() {
					if msg.user {
						text(c, "›", 16, accent).Font(monoFont)
					}
				})
				messageBody(c, msg)
			})
		}).Grow(1).MinHeight(0).MinWidth(0).Padding(10, 0).Gap(7).TrackScroll(&p.transcriptScroll)
		mygoui.ScrollFade(c, viewport, &p.transcriptScroll, mygoui.ScrollFadeOptions{
			Mode: mygoui.SurfaceFade, Surface: background, Edges: mygoui.FadeTop | mygoui.FadeBottom, Size: 24,
		})
	})
}

func messageBody(c *ui.Context, msg message) {
	ui.Column(c).Grow(1).MinWidth(0).Padding(9, paneGutter, 9, 0).Children(func() {
		if body := strings.TrimSpace(msg.text); body != "" {
			if msg.user {
				text(c, body, 14, ink).Selectable().LineHeight(1.5)
			} else {
				content.Markdown(c, content.MarkdownOptions{Source: body, Compact: true})
			}
		}
	})
}

func toolRow(c *ui.Context, p *pane, msg message) {
	expanded := p.expandedTools[msg.id]
	kind, status, _ := strings.Cut(clean(msg.label), " · ")
	detail := firstLine(msg.text)
	if chars := []rune(detail); len(chars) > 72 {
		detail = string(chars[:71]) + "…"
	}
	if status != "" && detail != "" {
		status += " · " + detail
	} else if status == "" {
		status = detail
	}
	summary := kind
	if status != "" {
		summary += " · " + status
	}
	glyph := "›"
	if expanded {
		glyph = "⌄"
	}
	color := muted
	ui.Column(c).MinWidth(0).Children(func() {
		button := ui.ButtonBase(c).Label(summary).AlignSelf(ui.Stretch).MinWidth(0).MinHeight(26).
			Justify(ui.Start).AlignItems(ui.Center).Padding(0, paneGutter).Gap(6).Radius(6).Disabled(msg.text == "")
		if button.Hovered() {
			button.Background(sidebar)
		}
		button.Children(func() {
			text(c, glyph, 13, muted).Width(8)
			text(c, kind, 11, color).Font(monoFont)
			if status != "" {
				text(c, status, 11, muted).Font(monoFont).Grow(1).NoWrap().Ellipsis("…")
			}
			if msg.succeeded {
				text(c, "✓", 12, muted).Tooltip("Succeeded")
			}
		})
		if button.Clicked() {
			if p.expandedTools == nil {
				p.expandedTools = make(map[string]bool)
			}
			if expanded {
				delete(p.expandedTools, msg.id)
			} else {
				p.expandedTools[msg.id] = true
			}
		}
		if expanded && msg.text != "" {
			ui.Row(c).Padding(0, paneGutter, 0, paneGutter).AlignItems(ui.Stretch).Gap(8).Children(func() {
				ui.Box(c).Width(2).Background(color)
				text(c, msg.text, 12, ink).Font(monoFont).Selectable().Grow(1).Padding(4, 0, 7)
			})
		}
	})
}
