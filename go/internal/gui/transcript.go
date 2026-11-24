package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Cyenoch/mygoui"
	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
	diffeditor "github.com/egoist/mygo/ui/editor"
	mmd "github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
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

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type fileDiff struct {
	path, before, after string
	computed            bool
}

type transcriptBlock struct{ kind, text string }

type message struct {
	blocks                                    []transcriptBlock
	id, label, text, target, stats, errorText string
	diffPatch                                 string
	toolCommand                               string
	computed                                  []fileDiff
	diffIncomplete                            bool
	diffLayout                                int
	diffInline                                string
	tool, user, cacheNotice                   bool
	failed                                    bool
	imageCount                                int
}

const (
	turnGap    = 20
	toolGap    = 2
	slowToolMs = 500
)

// transcriptScrollGuard protects only scroll sequences that begin over prose.
// MyGo does not expose wheel gesture begin/end, so idle time approximates the
// end, including trackpad momentum. Each pane owns its own guard.
type transcriptScrollGuard struct {
	last  time.Time
	inner bool
}

const transcriptScrollIdle = 250 * time.Millisecond

func (g *transcriptScrollGuard) outer(e ui.InputEvent, scroll *ui.ScrollState) bool {
	if e.Kind != ui.InputScroll {
		return false
	}
	inner := g.inner
	g.inner = false
	if inner || e.Mods&(ui.Ctrl|ui.Shift) != 0 || e.DY == 0 {
		return false
	}
	active := !g.last.IsZero() && !e.Time.Before(g.last) && e.Time.Sub(g.last) < transcriptScrollIdle
	if active || (e.DY < 0 && scroll.Y > 0) || (e.DY > 0 && scroll.Y < scroll.MaxY) {
		g.last = e.Time
	}
	return false // MyGo handles normal transcript scrolling.
}

func (g *transcriptScrollGuard) embedded(e ui.InputEvent, scroll *ui.ScrollState) bool {
	if e.Kind != ui.InputScroll {
		return false
	}
	if e.Mods&(ui.Ctrl|ui.Shift) == 0 && e.DY != 0 && !g.last.IsZero() &&
		!e.Time.Before(g.last) && e.Time.Sub(g.last) < transcriptScrollIdle {
		g.last = e.Time
		scroll.Y = max(0, min(scroll.MaxY, scroll.Y+e.DY))
		return true
	}
	// The parent handler sees the same event before default scroll routing.
	g.inner = true
	return false
}

// cacheNotices estimates re-billed prompt tokens, following Pi's noise floor.
// Entries outside the loaded history are unknown, so the first request is not compared.
func cacheNotices(entries []protocol.Entry) map[string]string {
	notices := make(map[string]string)
	previous, reported := 0, false
	model := ""
	for _, entry := range entries {
		if strings.Contains(entry.Kind, "compaction") || strings.Contains(entry.Kind, "branch_summary") {
			previous, reported, model = 0, false, ""
		}
		if entry.CacheInput == nil || entry.CacheRead == nil || entry.CacheWrite == nil {
			continue
		}
		prompt := *entry.CacheInput + *entry.CacheRead + *entry.CacheWrite
		if prompt <= 0 {
			continue
		}
		activity := *entry.CacheRead+*entry.CacheWrite > 0
		missed := min(previous, prompt) - *entry.CacheRead
		if previous > 0 && (reported || activity) && missed > 1024 {
			notice := fmt.Sprintf("Possible cache miss: ~%s tokens reprocessed", formatCacheTokens(missed))
			if model != "" && entry.CacheModel != nil && model != *entry.CacheModel {
				notice += " · model changed"
			}
			notices[entry.Id] = notice
		}
		previous, reported = prompt, reported || activity
		if entry.CacheModel != nil {
			model = *entry.CacheModel
		}
	}
	return notices
}

func formatCacheTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func messageStats(entry protocol.Entry) string {
	stats := palette.Stats(entry.DurationMs, entry.Tokens)
	if entry.Timestamp != nil && *entry.Timestamp > 0 {
		stamp := time.UnixMilli(int64(*entry.Timestamp)).Local().Format("Jan 2, 3:04 PM")
		if stats != "" {
			stats += " · "
		}
		stats += stamp
	}
	return stats
}

func (a *app) transcript(c *ui.Context, p *pane, conversationID string) {
	a.transcriptState(c, p, conversationID, &p.state, func() { a.history(p, conversationID) }, p.loading || !p.connected)
}

func (a *app) transcriptState(c *ui.Context, p *pane, conversationID string, state *client.State, older func(), loading bool) {
	entries := state.Entries()
	// A collapse during one stream must not make the next run start collapsed.
	run := valueOrEmpty(state.Current.AbortTarget)
	if run != p.thinkingRun {
		for key := range p.thinkingExpanded {
			if strings.HasPrefix(key, "partial:") {
				delete(p.thinkingExpanded, key)
			}
		}
		p.thinkingRun = run
	}
	messages := make([]message, 0, len(entries)+len(state.Current.Tools)+1)
	completed := make(map[string]bool)
	notices := cacheNotices(entries)
	for _, entry := range entries {
		if notice := notices[entry.Id]; notice != "" {
			messages = append(messages, message{id: "cache:" + entry.Id, text: notice, cacheNotice: true})
		}
		var blocks []transcriptBlock
		if entry.Blocks != nil {
			for _, block := range *entry.Blocks {
				if block.Text != "" && (!a.hideThinking || string(block.Type) != "thinking") {
					blocks = append(blocks, transcriptBlock{string(block.Type), clean(block.Text)})
				}
			}
		}
		if entry.Text == "" && len(blocks) == 0 && entry.ToolName == nil && (entry.Error == nil || *entry.Error == "") && (entry.ImageCount == nil || *entry.ImageCount == 0) {
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
			messages = append(messages, message{id: entry.Id, label: label, text: clean(entry.Text), blocks: blocks, errorText: errorText, user: label == "user", imageCount: imageCount,
				stats: messageStats(entry)})
			continue
		}
		completed[entry.Id] = true
		target := ""
		if entry.ToolTarget != nil {
			target = clean(*entry.ToolTarget)
		}
		stats := ""
		if entry.DurationMs != nil && *entry.DurationMs >= slowToolMs {
			stats = palette.Stats(entry.DurationMs, nil)
		}
		patch := ""
		if entry.DiffPatch != nil {
			patch = *entry.DiffPatch
		}
		var computed []fileDiff
		if entry.WriteDiffs != nil {
			for _, d := range *entry.WriteDiffs {
				computed = append(computed, fileDiff{d.Path, d.Before, d.After, true})
			}
		}
		if entry.ComputedDiffs != nil {
			for _, d := range *entry.ComputedDiffs {
				computed = append(computed, fileDiff{d.Path, d.Before, d.After, true})
			}
		}
		messages = append(messages, message{id: entry.Id, label: *entry.ToolName, text: clean(entry.Text), tool: true,
			target: target, stats: stats, toolCommand: valueOrEmpty(entry.ToolCommand), diffPatch: patch, computed: computed, diffIncomplete: entry.DiffIncomplete != nil && *entry.DiffIncomplete,
			diffLayout: a.diffLayout, diffInline: a.diffInline, failed: entry.ToolSucceeded != nil && !*entry.ToolSucceeded})
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
		messages = append(messages, message{id: key, label: tool.Name + " · " + tool.Status, text: output, tool: true, target: valueOrEmpty(tool.Target), toolCommand: valueOrEmpty(tool.Command)})
	}
	var partialBlocks []transcriptBlock
	if state.Current.PartialBlocks != nil {
		for _, block := range *state.Current.PartialBlocks {
			if block.Text != "" && (!a.hideThinking || string(block.Type) != "thinking") {
				partialBlocks = append(partialBlocks, transcriptBlock{string(block.Type), clean(block.Text)})
			}
		}
	}
	if len(partialBlocks) > 0 {
		messages = append(messages, message{id: "partial", label: "assistant · streaming", blocks: partialBlocks})
	} else if partial := state.Current.Partial; partial != nil {
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
			top := float32(turnGap)
			switch {
			case i == 0:
				top = 0
			case msg.tool && i > offset && messages[i-offset-1].tool:
				top = toolGap
			}
			ui.Column(c).FillWidth().MinWidth(0).Padding(top, 0, 0, 0).Children(func() {
				paneContent(c, func() {
					if msg.cacheNotice {
						text(c, msg.text, 12, warning).Padding(0, paneGutter, 0, paneTextInset)
						return
					}
					if msg.tool {
						toolRow(c, p, msg)
						return
					}
					a.messageBody(c, p, conversationID, msg)
				})
			})
		}).Grow(1).MinHeight(0).MinWidth(0).Padding(12, 0).TrackScroll(&p.transcriptScroll).HandleInput(func(e ui.InputEvent) bool {
			return p.transcriptGuard.outer(e, &p.transcriptScroll)
		})
		mygoui.ScrollFade(c, viewport, &p.transcriptScroll, mygoui.ScrollFadeOptions{
			Mode: mygoui.SurfaceFade, Surface: contentSurface, Edges: mygoui.FadeTop, Size: 24,
		})
	})
}

func (a *app) messageBody(c *ui.Context, p *pane, conversationID string, msg message) {
	bodyView := ui.Column(c).FillWidth().MinWidth(0).Padding(8, paneGutter, 8, paneTextInset)
	if msg.user {
		bodyView.Background(panel).Radius(inputRadius)
	}
	bodyView.Children(func() {
		if msg.user && msg.imageCount > 0 {
			a.messageImages(c, p, conversationID, msg.id, msg.imageCount)
		}
		if msg.user {
			if body := strings.TrimSpace(msg.text); body != "" {
				text(c, body, 14, ink).Selectable().LineHeight(1.5)
			}
		} else {
			if len(msg.blocks) > 0 {
				for i, block := range msg.blocks {
					if block.kind == "thinking" {
						key := fmt.Sprintf("%s:%d", msg.id, i)
						expanded, set := p.thinkingExpanded[key]
						if !set {
							expanded = true
						}
						thinkingBlock(c.Key(key), block.text, expanded, func() {
							if p.thinkingExpanded == nil {
								p.thinkingExpanded = make(map[string]bool)
							}
							p.thinkingExpanded[key] = !expanded
						})
					} else if strings.TrimSpace(block.text) != "" {
						a.prose(c, block.text, msg.id == "partial", p)
					}
				}
			} else if body := strings.TrimSpace(msg.text); body != "" {
				a.prose(c, body, msg.id == "partial", p)
			}
			if msg.stats != "" {
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() { text(c, msg.stats, 11, faint).Font(monoFont).FontFeatures("tnum") })
			}
		}
		if msg.errorText != "" {
			text(c, "Error: "+msg.errorText, 13, warning).Selectable().LineHeight(1.5)
		}
	})
}

// A small plus/minus control keeps the trace collapsible without a heading or rail.
func thinkingBlock(c *ui.Context, body string, expanded bool, toggle func()) {
	ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Start).Padding(3, 0, 12, 0).Gap(8).Children(func() {
		if expanded {
			text(c, strings.TrimSpace(body), 14, ui.Hex("#a3aea6")).Font("Source Serif 4").Italic().Selectable().LineHeight(1.6).Grow(1).MinWidth(0)
		} else {
			ui.Box(c).Grow(1)
		}
		label, symbol := "Expand thinking", "+"
		if expanded {
			label, symbol = "Collapse thinking", "−"
		}
		button := ui.ButtonBase(c).Label(label).Tooltip(label).Size(22, 22).Shrink(0).Radius(5).Center()
		if button.Hovered() {
			button.Background(highlight)
		}
		button.Children(func() { text(c, symbol, 15, faint) })
		if button.Clicked() {
			toggle()
		}
	})
}

type proseState struct {
	options mmd.Options
	theme   mmd.Theme
	body    string
	columns int
	width   float32
}
type proseKey struct{}

func (a *app) prose(c *ui.Context, body string, streaming bool, panes ...*pane) {
	root := ui.Column(c).FillWidth().MinWidth(0)
	st := *ui.Local(root, proseKey{}, func() *proseState {
		s := &proseState{}
		s.options.Theme = &s.theme
		return s
	})
	st.options.CodeBlock = func(c *ui.Context, b *mmd.CodeBlockView) {
		block := ui.Column(c).FillWidth().MinWidth(0)
		if len(panes) > 0 && panes[0] != nil {
			p := panes[0]
			block.HandleInput(func(e ui.InputEvent) bool { return p.transcriptGuard.embedded(e, &p.transcriptScroll) })
		}
		block.Children(func() {
			if strings.EqualFold(b.Language, "mermaid") && !b.Open && !streaming {
				a.mermaidBlock(c, b)
				return
			}
			content.Highlighter(c, content.HighlighterOptions{Code: b.Code(), Language: b.Language})
		})
	}
	st.theme = mmd.Theme{Font: "Source Serif 4", CodeFont: monoFont, FontSize: 15, LineHeight: 1.6,
		Text: ui.Hex("#e0e9dc"), Muted: muted, Link: rain, Strong: ui.Hex("#e0e9dc"), Code: ink, CodeBackground: panel,
		QuoteBar: theme.Border, QuoteText: muted, Rule: theme.Border, TableBorder: theme.Border,
		TableHeader: panel, Checkbox: accent, BlockSpacing: .6}
	if st.body != body {
		st.body, st.columns = body, 0
		doc := parser.Parse(body)
		for i := 0; i < doc.Len(); i++ {
			if b := doc.Block(i); b.Kind == parser.Table {
				st.columns = max(st.columns, len(b.Align))
			}
		}
	}
	if st.columns > 0 {
		st.theme.TableMaxColumn, st.theme.TableCellPadding[0] = transcriptTableSizing(st.width, st.theme.FontSize, st.columns)

		// Use the laid-out prose width, after pane gutters, the reading-width
		// cap, and message padding. Context.Size is the whole window.
		root.DrawOver(func(_ *ui.Painter, bounds ui.Rect) {
			if bounds.W != st.width {

				st.width = bounds.W
				c.Invalidate()
			}
		})
	}
	root.Children(func() {
		// Open Markdown tables retain grown column widths. Reset that cache
		// when the width budget changes so columns can also shrink on resize.
		mmd.View(c.Key([2]float32{st.theme.TableMaxColumn, st.theme.TableCellPadding[0]}), body, &st.options)
	})
}

// The Markdown renderer sizes each column to its natural width up to this
// cap. Budget for the widest table in the message; shorter cells stay narrow.
// Below the readable minimum, let the renderer's horizontal viewport scroll
// instead of squeezing columns further or reducing the font size.
func transcriptTableSizing(width, fontSize float32, columns int) (cap, padding float32) {
	const minColumn, maxColumn = 5, 24 // em
	padding = 0.6
	if width <= 0 {
		return maxColumn, padding
	}
	// Reserve the grid's outer border and the renderer's per-column divider.
	perColumn := ((width-2)/float32(columns) - 1) / fontSize
	if perColumn < minColumn+2*padding {
		padding = 0.3
	}
	return min(maxColumn, max(minColumn, perColumn-2*padding)), padding
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
			Justify(ui.Start).AlignItems(ui.Center).Padding(0, paneGutter, 0, 0).Radius(6).Disabled(msg.text == "" && msg.toolCommand == "")
		if button.Hovered() {
			button.Background(sidebar)
		}
		button.Children(func() {
			ui.Row(c).Width(paneTextInset).Shrink(0).Center().Children(func() {
				if msg.text != "" || msg.toolCommand != "" {
					text(c, glyph, 13, muted)
				}
			})
			// Success is the default and goes unmarked; only a failure is.
			kindColor := muted
			if msg.failed {
				kindColor = theme.Danger
			}
			text(c, kind, 11, kindColor).Font(monoFont).Margin(0, 6, 0, 0)
			if status != "" {
				text(c, status, 11, muted).Font(monoFont).Grow(1).MinWidth(0).SingleLine()
			} else {
				ui.Box(c).Grow(1)
			}
			if msg.failed {
				text(c, "✕", 12, theme.Danger).Tooltip("Failed").Shrink(0)
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
		if expanded {
			if msg.toolCommand != "" {
				prefix := "Input:\n"
				if strings.EqualFold(kind, "bash") {
					prefix = "$ "
				}
				text(c, prefix+clean(msg.toolCommand), 12, ink).Font(monoFont).Selectable().Padding(4, paneGutter, 3, paneTextInset+paneGutter)
			}
			if msg.text != "" {
				text(c, msg.text, 12, ink).Font(monoFont).Selectable().Padding(4, paneGutter, 7, paneTextInset+paneGutter)
			}
		}
		if msg.diffPatch != "" {
			toolDiff(c, msg, nil)
		}
		for i := range msg.computed {
			toolDiff(c.Key(i), msg, &msg.computed[i])
		}
		// TODO: Restore a useful snapshot coverage warning once expected build
		// artifacts can be distinguished from unexpected scan failures or limits.
	})
}

type toolDiffState struct {
	patch string
	diff  *diffeditor.DiffState
	open  bool
}

func toolDiff(c *ui.Context, msg message, computed *fileDiff) {
	root := ui.Column(c).FillWidth().MinWidth(0).Padding(0, paneGutter, 4, paneTextInset)
	state := *ui.Local(root, "tool-diff", func() *toolDiffState { return &toolDiffState{open: true} })
	key := msg.diffPatch
	if computed != nil {
		key = computed.path + "\x00" + computed.before + "\x00" + computed.after
	}
	if state.patch != key {
		state.patch, state.diff = key, nil
		if computed != nil {
			old, next := diffeditor.NewBuffer(computed.before), diffeditor.NewBuffer(computed.after)
			old.SetLanguage(diffeditor.LanguageFor(computed.path))
			next.SetLanguage(diffeditor.LanguageFor(computed.path))
			state.diff = &diffeditor.DiffState{Old: old, New: next, FitContent: true, LineSelection: true}
		} else {
			files, err := diffeditor.ParsePatch(msg.diffPatch)
			if err == nil && len(files) == 1 {
				state.diff = &diffeditor.DiffState{FitContent: true, LineSelection: true}
				state.diff.SetPatch(files[0])
			}
		}
	}
	if state.diff == nil {
		return
	}
	state.diff.Layout = diffeditor.DiffLayout(msg.diffLayout)
	switch msg.diffInline {
	case "Characters":
		state.diff.Inline = diffeditor.DiffInlineChar
	case "Off":
		state.diff.Inline = diffeditor.DiffInlineNone
	default:
		state.diff.Inline = diffeditor.DiffInlineWordAlt
	}
	root.Children(func() {
		path := msg.target
		if computed != nil {
			path = computed.path
		}
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(26).AlignItems(ui.Center).Children(func() {
			toggle := ui.ButtonBase(c).Label("Toggle file diff").MinWidth(0).Shrink(1).MinHeight(26).AlignItems(ui.Center).Radius(6).Padding(0, 4)
			if toggle.Hovered() {
				toggle.Background(sidebar)
			}
			toggle.Children(func() {
				glyph := "›"
				if state.open {
					glyph = "⌄"
				}
				text(c, glyph, 13, muted).Shrink(0).Margin(0, 6, 0, 0)
				text(c, path, 11, ink).Font(monoFont).MinWidth(0).Shrink(1).SingleLine().Tooltip(path)
			})
			if toggle.Clicked() {
				state.open = !state.open
			}
			if path != "" {
				content.CopyButton(c, content.CopyButtonOptions{Text: path, Label: "Copy file path", Size: uitheme.ButtonExtraSmall})
			}
			ui.Box(c).Grow(1)
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
				if computed != nil && computed.computed {
					text(c, "Computed", 11, faint).SingleLine().Shrink(0).Margin(0, 6, 0, 0)
					tip := "This diff is computed from best-effort snapshots around bash calls"
					if msg.label == "write" {
						tip = "This diff is computed from file contents observed before the write and the content supplied to the tool."
					}
					text(c, "ⓘ", 12, muted).SingleLine().Shrink(0).Tooltip(tip).Margin(0, 6, 0, 0)
				}
				added, removed := state.diff.Stats()
				text(c, fmt.Sprintf("+%d", added), 11, theme.Success).Font(monoFont).SingleLine().Shrink(0).Margin(0, 6, 0, 0)
				text(c, fmt.Sprintf("−%d", removed), 11, theme.Danger).Font(monoFont).SingleLine().Shrink(0)
			})
		})
		if state.open {
			ui.Column(c).FillWidth().MinWidth(0).Background(panel).Radius(controlRadius).Clip().Children(func() {
				diffeditor.DiffView(c, state.diff).FillWidth()
			})
		}
	})
}
