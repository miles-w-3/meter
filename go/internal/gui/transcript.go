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
	"pi-workspace.local/workspace/internal/transcript"
)

func ghost(c *ui.Context, label string, disabled bool) bool {
	return mygoui.Button(c, mygoui.ButtonOptions{Label: label, Variant: mygoui.ButtonGhost, Size: uitheme.ButtonExtraSmall, Disabled: disabled}).Clicked()
}

// updateMeter keeps the activity wave in sync with the running turn.
func (p *pane) updateMeter(now time.Time) {
	p.activity.Observe(now, p.connected && p.state.Current.Status == "running")
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

type message struct {
	blocks                                    []transcript.Part
	id, label, text, target, stats, errorText string
	diffPatch                                 string
	toolCommand                               string
	computed                                  []fileDiff
	diffCount                                 int
	diffIncomplete                            bool
	diffLayout                                int
	diffInline                                string
	tool, user, notice                        bool
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

// Cache the projection of committed entries separately from live content.
type transcriptKey struct {
	state        *client.State
	revision     uint64
	first        *protocol.Entry
	count        int
	hideThinking bool
	diffLayout   int
	diffInline   string
}
type transcriptCache struct {
	valid     bool
	key       transcriptKey
	messages  []message
	completed map[string]bool
	calls     map[string]string
}

func (a *app) committedMessages(p *pane, state *client.State) ([]message, map[string]bool) {
	key := transcriptKey{state: state, revision: state.Revision(), count: len(state.Current.Entries), hideThinking: a.hideThinking, diffLayout: a.diffLayout, diffInline: a.diffInline}
	if key.count > 0 {
		key.first = &state.Current.Entries[0]
	}
	if p.transcriptCache.valid && p.transcriptCache.key == key {
		return p.transcriptCache.messages, p.transcriptCache.completed
	}
	items, completed, calls := transcript.BuildEntries(state.Entries(), a.hideThinking)
	messages := make([]message, 0, len(items))
	for _, item := range items {
		messages = append(messages, a.transcriptMessage(item))
	}
	p.transcriptCache = transcriptCache{valid: true, key: key, messages: messages, completed: completed, calls: calls}
	return messages, completed
}
func (a *app) liveMessages(p *pane, state *client.State, completed map[string]bool) []message {
	items := transcript.BuildLive(&state.Current, a.hideThinking, p.expandedTools, completed, p.transcriptCache.calls)
	messages := make([]message, 0, len(items))
	for _, item := range items {
		messages = append(messages, a.transcriptMessage(item))
	}
	return messages
}

func (a *app) transcriptState(c *ui.Context, p *pane, conversationID string, state *client.State, older func(), loading bool) {
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
	committed, completed := a.committedMessages(p, state)
	live := a.liveMessages(p, state, completed)
	count := len(committed) + len(live)
	at := func(i int) message {
		if i < len(committed) {
			return committed[i]
		}
		return live[i-len(committed)]
	}
	if count == 0 && state.Before == nil {
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
		return at(row - offset).id
	}
	p.list.FollowEnd = true
	ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Background(contentSurface).Clip().Children(func() {
		viewport := ui.List(c, &p.list, count+offset, func(i int) {
			if offset == 1 && i == 0 {
				ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
					if ui.Button(c, "Older history").FontSize(12).TextColor(muted).Background(panel).Radius(12).Disabled(loading).Clicked() {
						older()
					}
				})
				return
			}
			msg := at(i - offset)
			top := float32(turnGap)
			switch {
			case i == 0:
				top = 0
			case msg.tool && i > offset && at(i-offset-1).tool:
				top = toolGap
			}
			ui.Column(c).FillWidth().MinWidth(0).Padding(top, 0, 0, 0).Children(func() {
				paneContent(c, func() {
					if msg.notice {
						text(c, msg.text, 12, warning).Padding(0, paneGutter, 0, paneTextInset)
						return
					}
					if msg.tool {
						var diffs *transcriptDiffs
						if msg.diffCount > 0 {
							// Only rows the virtualized list builds fetch their diffs.
							diffs = a.messageDiffs(p, conversationID, msg.id)
							msg.diffPatch, msg.computed = diffs.patch, diffs.files
						}
						toolRow(c, p, msg)
						if diffs != nil {
							diffStatus(c, p, msg.id, msg.diffCount, diffs)
						}
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

func (a *app) transcriptMessage(item transcript.Item) message {
	msg := message{
		id: item.ID, label: item.Label, text: item.Text, blocks: item.Parts,
		errorText: item.Error, target: item.Target, toolCommand: item.Command,
		tool: item.Kind == transcript.Tool, notice: item.Kind == transcript.Notice,
		user: item.Kind == transcript.Message && item.Label == "user",
	}
	if item.Live != nil {
		msg.label += " · " + item.Live.Status
		msg.stats = palette.LiveToolStats(item.Live.Status, item.Live.StartedAt, time.Now())
		return msg
	}
	entry := item.Entry
	if entry == nil {
		return msg
	}
	if !msg.tool {
		msg.stats = messageStats(*entry)
		if entry.ImageCount != nil {
			msg.imageCount = *entry.ImageCount
		}
		return msg
	}
	if entry.DurationMs != nil && *entry.DurationMs >= slowToolMs {
		msg.stats = palette.Stats(entry.DurationMs, nil)
	}
	if entry.DiffCount != nil {
		msg.diffCount = *entry.DiffCount
	}
	msg.diffIncomplete = entry.DiffIncomplete != nil && *entry.DiffIncomplete
	msg.diffLayout, msg.diffInline = a.diffLayout, a.diffInline
	msg.failed = entry.ToolSucceeded != nil && !*entry.ToolSucceeded
	return msg
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
				for _, block := range msg.blocks {
					if block.Thinking {
						key := fmt.Sprintf("%s:%d", msg.id, block.Index)
						expanded, set := p.thinkingExpanded[key]
						if !set {
							expanded = true
						}
						a.thinkingBlock(c.Key(key), p, block.Text, msg.id == transcript.PartialID, expanded, func() {
							if p.thinkingExpanded == nil {
								p.thinkingExpanded = make(map[string]bool)
							}
							p.thinkingExpanded[key] = !expanded
						})
					} else if strings.TrimSpace(block.Text) != "" {
						a.prose(c, block.Text, msg.id == transcript.PartialID, p)
					}
				}
			}
			if body := strings.TrimSpace(msg.text); body != "" {
				if msg.id == transcript.PartialID {
					body += " ▍"
				}
				a.prose(c, body, msg.id == transcript.PartialID, p)
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
func (a *app) thinkingBlock(c *ui.Context, p *pane, body string, streaming, expanded bool, toggle func()) {
	ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Start).Padding(3, 0, 12, 0).Gap(8).Children(func() {
		if expanded {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.renderProse(c, strings.TrimSpace(body), streaming, true, p)
			})
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
	a.renderProse(c, body, streaming, false, panes...)
}

func (a *app) renderProse(c *ui.Context, body string, streaming, thinking bool, panes ...*pane) {
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
	if thinking {
		color := ui.Hex("#a3aea6")
		st.theme.FontSize, st.theme.Text, st.theme.Strong, st.theme.Code = 14, color, color, color
	}
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
	// The patch or file contents diff was built from.
	patch string
	file  fileDiff
	diff  *diffeditor.DiffState
	open  bool
}

func toolDiff(c *ui.Context, msg message, computed *fileDiff) {
	root := ui.Column(c).FillWidth().MinWidth(0).Padding(0, paneGutter, 4, paneTextInset)
	state := *ui.Local(root, "tool-diff", func() *toolDiffState { return &toolDiffState{open: true} })
	// Compare the sources field by field rather than joining them into a key,
	// which copied whole file contents every frame.
	var file fileDiff
	if computed != nil {
		file = *computed
	}
	if state.patch != msg.diffPatch || state.file != file {
		state.patch, state.file, state.diff = msg.diffPatch, file, nil
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
