package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Cyenoch/mygoui"
	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
	mmd "github.com/egoist/mygo/ui/markdown"
	"pi-workspace.local/workspace/internal/client"
	palette "pi-workspace.local/workspace/internal/theme"
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

type message struct {
	id, label, text, target, stats, errorText string
	tool, user                                bool
	succeeded                                 bool
	imageCount                                int
}

func (a *app) transcript(c *ui.Context, p *pane, conversationID string) {
	a.transcriptState(c, p, conversationID, &p.state, func() { a.history(p, conversationID) }, p.loading || !p.connected)
}

func (a *app) transcriptState(c *ui.Context, p *pane, conversationID string, state *client.State, older func(), loading bool) {
	entries := state.Entries()
	messages := make([]message, 0, len(entries)+len(state.Current.Tools)+1)
	completed := make(map[string]bool)
	for _, entry := range entries {
		if entry.Text == "" && entry.ToolName == nil && (entry.Error == nil || *entry.Error == "") && (entry.ImageCount == nil || *entry.ImageCount == 0) {
			continue
		}
		label := strings.TrimPrefix(entry.Kind, "pi.")
		if entry.ToolName == nil {
			imageCount := 0
			if entry.ImageCount != nil {
				imageCount = *entry.ImageCount
			}
			errorText := ""
			if entry.Error != nil {
				errorText = clean(*entry.Error)
			}
			messages = append(messages, message{id: entry.Id, label: label, text: clean(entry.Text), errorText: errorText, user: label == "user", imageCount: imageCount,
				stats: palette.Stats(entry.DurationMs, entry.Tokens)})
			continue
		}
		completed[entry.Id] = true
		target := ""
		if entry.ToolTarget != nil {
			target = clean(*entry.ToolTarget)
		}
		messages = append(messages, message{id: entry.Id, label: *entry.ToolName, text: clean(entry.Text), tool: true,
			target: target, stats: palette.Stats(entry.DurationMs, nil), succeeded: entry.ToolSucceeded != nil && *entry.ToolSucceeded})
	}
	for i, tool := range state.Current.Tools {
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
	if partial := state.Current.Partial; partial != nil {
		messages = append(messages, message{id: "partial", label: "assistant · streaming", text: clean(*partial) + " ▍"})
	}
	if len(messages) == 0 && state.Before == nil {
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			text(c, "Messages appear here", 14, muted)
		})
		return
	}
	offset := 0
	if state.Before != nil {
		offset = 1
	}
	p.list.Key = func(row int) any {
		if offset == 1 && row == 0 {
			return "older-history"
		}
		return messages[row-offset].id
	}
	p.list.FollowEnd = true
	ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Background(contentSurface).Clip().Children(func() {
		viewport := ui.List(c, &p.list, len(messages)+offset, func(i int) {
			if offset == 1 && i == 0 {
				ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
					if ui.Button(c, "Older history").FontSize(12).TextColor(muted).Background(panel).Radius(12).Disabled(loading).Clicked() {
						older()
					}
				})
				return
			}
			msg := messages[i-offset]
			paneContent(c, func() {
				if msg.tool {
					toolRow(c, p, msg)
					return
				}
				a.messageBody(c, p, conversationID, msg)
			})
		}).Grow(1).MinHeight(0).MinWidth(0).Padding(12, 0).Gap(20).TrackScroll(&p.transcriptScroll)
		mygoui.ScrollFade(c, viewport, &p.transcriptScroll, mygoui.ScrollFadeOptions{
			Mode: mygoui.SurfaceFade, Surface: contentSurface, Edges: mygoui.FadeTop, Size: 24,
		})
	})
}

func (a *app) messageBody(c *ui.Context, p *pane, conversationID string, msg message) {
	bodyView := ui.Column(c).FillWidth().MinWidth(0).Padding(8, paneGutter, 8, paneTextInset)
	if msg.user {
		bodyView.Background(panel).Radius(8)
	}
	bodyView.Children(func() {
		if msg.user && msg.imageCount > 0 {
			a.messageImages(c, p, conversationID, msg.id, msg.imageCount)
		}
		if body := strings.TrimSpace(msg.text); body != "" {
			if msg.user {
				text(c, body, 14, ink).Selectable().LineHeight(1.5)
			} else {
				a.prose(c, body, msg.id == "partial")
				if msg.stats != "" {
					ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
						text(c, msg.stats, 11, faint).Font(monoFont).FontFeatures("tnum")
					})
				}
			}
		}
		if msg.errorText != "" {
			text(c, "Error: "+msg.errorText, 13, warning).Selectable().LineHeight(1.5)
		}
	})
}

type proseState struct {
	options mmd.Options
	theme   mmd.Theme
}
type proseKey struct{}

func (a *app) prose(c *ui.Context, body string, streaming bool) {
	root := ui.Column(c).FillWidth().MinWidth(0)
	st := *ui.Local(root, proseKey{}, func() *proseState {
		s := &proseState{}
		s.options.Theme = &s.theme
		return s
	})
	st.options.CodeBlock = func(c *ui.Context, b *mmd.CodeBlockView) {
		if strings.EqualFold(b.Language, "mermaid") && !b.Open && !streaming {
			a.mermaidBlock(c, b)
			return
		}
		content.Highlighter(c, content.HighlighterOptions{Code: b.Code(), Language: b.Language})
	}
	st.theme = mmd.Theme{Font: "Source Serif 4", CodeFont: monoFont, FontSize: 15, LineHeight: 1.6,
		Text: ui.Hex("#e0e9dc"), Muted: muted, Link: rain, Strong: ui.Hex("#e0e9dc"), Code: ink, CodeBackground: panel,
		QuoteBar: theme.Border, QuoteText: muted, Rule: theme.Border, TableBorder: theme.Border,
		TableHeader: panel, Checkbox: accent, BlockSpacing: .6}
	root.Children(func() { mmd.View(c, body, &st.options) })
}

func toolRow(c *ui.Context, p *pane, msg message) {
	expanded := p.expandedTools[msg.id]
	kind, status, _ := strings.Cut(clean(msg.label), " · ")
	detail := firstLine(msg.text)
	if msg.target != "" {
		detail = firstLine(msg.target)
	}
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
	ui.Column(c).MinWidth(0).Children(func() {
		button := ui.ButtonBase(c).Label(summary).AlignSelf(ui.Stretch).MinWidth(0).MinHeight(26).
			Justify(ui.Start).AlignItems(ui.Center).Padding(0, paneGutter, 0, 0).Radius(6).Disabled(msg.text == "")
		if button.Hovered() {
			button.Background(sidebar)
		}
		button.Children(func() {
			ui.Row(c).Width(paneTextInset).Shrink(0).Center().Children(func() {
				if msg.text != "" {
					text(c, glyph, 13, muted)
				}
			})
			text(c, kind, 11, muted).Font(monoFont).Margin(0, 6, 0, 0)
			if status != "" {
				text(c, status, 11, muted).Font(monoFont).Grow(1).NoWrap().Ellipsis("…")
			}
			if msg.succeeded {
				text(c, "✓", 12, muted).Tooltip("Succeeded")
			}
			if msg.stats != "" {
				text(c, msg.stats, 11, faint).Font(monoFont).FontFeatures("tnum").Shrink(0).Margin(0, 0, 0, 6)
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
			text(c, msg.text, 12, ink).Font(monoFont).Selectable().Padding(4, paneGutter, 7, paneTextInset+paneGutter)
		}
	})
}
