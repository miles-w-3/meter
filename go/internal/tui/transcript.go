package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/theme"
	projection "pi-workspace.local/workspace/internal/transcript"
)

type blockKind uint8

const (
	userBlock blockKind = iota
	assistantBlock
	toolBlock
	noticeBlock
)

const streamingID = projection.PartialID
const toolPreviewLines = 20
const codeStart, codeEnd = "\uE000", "\uE001"

type Block struct {
	id                                       string
	kind                                     blockKind
	text, tool, target, status, stats, entry string
	added, removed                           int
	diff, clipped                            bool
}

type renderedBlock struct {
	block    Block
	expanded bool
	lines    []string
}

type blockRange struct {
	block Block
	start int
	lines []string
}

type transcript struct {
	blocks                    []blockRange
	cache                     map[string]renderedBlock
	expanded                  map[string]bool
	width, total, top, cursor int
	follow, unseen            bool
	markdown                  *glamour.TermRenderer
	colorVersion              int
	streamPrefix              string
	streamLines               []string
}

func blocksFor(state client.State, expanded map[string]bool, hideThinking bool) []Block {
	items := projection.Build(&state, hideThinking, expanded)
	blocks := make([]Block, 0, len(items))
	for _, item := range items {
		block := Block{id: item.ID, kind: noticeBlock, text: item.Text}
		entry := item.Entry
		switch {
		case item.Kind == projection.Tool:
			block.kind, block.tool, block.target = toolBlock, item.Label, item.Target
			if item.Live != nil {
				block.status = item.Live.Status
				block.stats = theme.LiveToolStats(item.Live.Status, item.Live.StartedAt, time.Now())
			} else {
				block.entry, block.clipped = entry.Id, value(entry.OutputClipped)
				block.stats = theme.Stats(entry.DurationMs, entry.Tokens)
				block.added, block.removed = value(entry.Added), value(entry.Removed)
				block.diff = entry.Added != nil || entry.Removed != nil
				if entry.ToolSucceeded != nil {
					block.status = "✓"
					if !*entry.ToolSucceeded {
						block.status = "✗"
					}
				}
			}
		case item.Kind == projection.Message:
			switch item.Label {
			case "user":
				block.kind = userBlock
			case "assistant":
				block.kind = assistantBlock
			}
			var sections []string
			if entry != nil {
				block.stats = theme.Stats(entry.DurationMs, entry.Tokens)
				if count := value(entry.ImageCount); count > 0 {
					sections = append(sections, plural(count, "image")+" attached")
				}
			}
			for _, part := range item.Parts {
				content := part.Text
				if part.Thinking {
					content = "> Thinking\n>\n> " + strings.ReplaceAll(content, "\n", "\n> ")
				}
				sections = append(sections, content)
			}
			if item.Text != "" {
				sections = append(sections, item.Text)
			}
			if item.Error != "" {
				sections = append(sections, "Error: "+item.Error)
			}
			block.text = strings.Join(sections, "\n\n")
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func (m *Model) refresh() {
	t := &m.transcript
	f := m.frame()
	topBlock, topLine := t.anchor(t.top)
	cursorBlock, cursorLine := t.anchor(t.cursor)
	widthChanged := t.width != f.bodyWidth() || t.colorVersion != m.colors.version
	if t.cache == nil || widthChanged {
		t.cache = make(map[string]renderedBlock)
		t.width, t.colorVersion = f.bodyWidth(), m.colors.version
		t.markdown = m.colors.markdown(t.width)
		t.streamPrefix, t.streamLines = "", nil
	}
	if t.expanded == nil {
		t.expanded = make(map[string]bool)
	}
	blocks := blocksFor(m.state, t.expanded, m.options.HideThinking)
	previous := t.blocks
	bottomChanged := len(previous) == 0 && len(blocks) > 0
	if len(previous) > 0 && len(blocks) > 0 {
		bottomChanged = bottomChanged || previous[len(previous)-1].block != blocks[len(blocks)-1]
	}
	t.blocks = t.blocks[:0]
	t.total = 0
	for i, block := range blocks {
		expanded := t.expanded[block.id]
		cached, ok := t.cache[block.id]
		if !ok || cached.block != block || cached.expanded != expanded {
			cached = renderedBlock{block, expanded, m.renderBlock(block, expanded)}
			t.cache[block.id] = cached
		}
		if i > 0 && (block.kind != toolBlock || blocks[i-1].kind != toolBlock || t.expanded[blocks[i-1].id] && blocks[i-1].text != "") {
			t.total++
		}
		t.blocks = append(t.blocks, blockRange{block, t.total, cached.lines})
		t.total += len(cached.lines)
	}
	if len(t.cache) != len(blocks) {
		active := make(map[string]bool, len(blocks))
		for _, block := range blocks {
			active[block.id] = true
		}
		for id := range t.cache {
			if !active[id] {
				delete(t.cache, id)
				delete(t.expanded, id)
			}
		}
	}
	if t.follow {
		t.toBottom(f.rows)
	} else {
		t.top = t.restore(topBlock, topLine, t.top, widthChanged)
		t.cursor = t.restore(cursorBlock, cursorLine, t.cursor, widthChanged)
		t.unseen = t.unseen || bottomChanged
		t.clamp(f.rows)
	}
}

func (t *transcript) at(line int) (blockRange, bool) {
	i := sort.Search(len(t.blocks), func(i int) bool { return t.blocks[i].start > line }) - 1
	if i >= 0 && line < t.blocks[i].start+len(t.blocks[i].lines) {
		return t.blocks[i], true
	}
	return blockRange{}, false
}

func (t *transcript) anchor(line int) (string, int) {
	if block, ok := t.at(line); ok {
		return block.block.id, line - block.start
	}
	// A blank row belongs to the upcoming turn for anchoring purposes.
	if block, ok := t.at(line + 1); ok {
		return block.block.id, -1
	}
	return "", 0
}

func (t *transcript) restore(id string, line, fallback int, resized bool) int {
	for _, b := range t.blocks {
		if b.block.id == id {
			if resized {
				return b.start
			}
			return max(0, b.start+min(line, len(b.lines)-1))
		}
	}
	return fallback
}

func (t *transcript) toBottom(rows int) {
	t.top, t.cursor = max(0, t.total-rows), max(0, t.total-1)
	t.follow, t.unseen = true, false
}

func (t *transcript) clamp(rows int) {
	if t.follow {
		t.toBottom(rows)
		return
	}
	t.top = min(max(0, t.top), max(0, t.total-rows))
	t.cursor = min(max(t.top, t.cursor), min(max(0, t.total-1), t.top+max(0, rows-1)))
}

func (m *Model) scroll(delta int, wheel bool) tea.Cmd {
	t := &m.transcript
	rows := m.frame().rows
	m.scrollbarUntil = time.Now().Add(time.Second)
	fade := tea.Tick(time.Second, func(time.Time) tea.Msg { return scrollbarFadeMsg{} })
	if delta < 0 {
		t.follow = false
	}
	if wheel {
		t.top = max(0, t.top+delta)
		t.clamp(rows)
	} else {
		m.moveTo(t.cursor + delta)
	}
	if delta > 0 && t.top >= max(0, t.total-rows) && (wheel || t.cursor >= t.total-1) {
		t.follow, t.unseen = true, false
	}
	if delta < 0 && t.top == 0 {
		return tea.Batch(m.loadHistory(), fade)
	}
	return fade
}

func (m *Model) moveTo(line int) {
	t := &m.transcript
	rows := max(1, m.frame().rows)
	t.cursor = min(max(0, line), max(0, t.total-1))
	if t.cursor < t.top {
		t.top = t.cursor
	} else if t.cursor >= t.top+rows {
		t.top = t.cursor - rows + 1
	}
}

func (m *Model) renderBlock(block Block, expanded bool) []string {
	t := &m.transcript
	c := m.colors
	switch block.kind {
	case userBlock:
		lines := wrap(block.text, t.width)
		for i, line := range lines {
			lines[i] = c.user.Render(line)
		}
		return append(append([]string{""}, lines...), "")
	case assistantBlock:
		if block.id == streamingID {
			prefix, last := lastParagraph(block.text)
			if prefix != t.streamPrefix {
				t.streamPrefix = prefix
				t.streamLines = t.markdownLines(prefix, c)
			}
			lines := append([]string{}, t.streamLines...)
			if prefix != "" {
				lines = append(lines, "")
			}
			for _, line := range wrap(last+" ▍", t.width) {
				lines = append(lines, c.fog.Render(line))
			}
			return lines
		}
		lines := t.markdownLines(block.text, c)
		if block.stats != "" && t.width >= ansi.StringWidth(block.stats) {
			lines = append(lines, c.faint.Render(block.stats))
		}
		return lines
	case toolBlock:
		return m.renderTool(block, expanded)
	default:
		return wrap(c.faint.Render(block.text), t.width)
	}
}

// Keep a partially streamed fenced block together even if it contains blank lines.
func lastParagraph(text string) (string, string) {
	fence := ""
	boundary, offset := 0, 0
	for line := range strings.SplitSeq(text, "\n") {
		if fence == "" {
			if start := fenceMarker(line); start != "" {
				fence = start
			} else if strings.TrimSpace(line) == "" && offset+len(line) < len(text) {
				boundary = offset + len(line) + 1
			}
		} else if marker := fenceMarker(line); strings.HasPrefix(marker, fence) {
			fence = ""
		}
		offset += len(line) + 1
	}
	return strings.TrimRight(text[:boundary], "\n"), text[boundary:]
}

func fenceMarker(line string) string {
	s := strings.TrimSpace(line)
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return ""
	}
	i := 1
	for i < len(s) && s[i] == s[0] {
		i++
	}
	if i < 3 {
		return ""
	}
	return s[:i]
}

func (m *Model) renderTool(b Block, expanded bool) []string {
	c, width := m.colors, m.transcript.width
	target := b.target
	if target == "" {
		target = firstLine(b.text)
	}
	target = strings.TrimPrefix(target, "Successfully wrote to ")
	if filepath.IsAbs(target) && !strings.Contains(target, " ") {
		target = shortenPath(target)
	}
	kind := ansi.Truncate(b.tool, 6, "…")
	label := c.rain.Bold(!c.plain).Render(kind)
	if target != "" && width >= 9 {
		label += strings.Repeat(" ", 7-ansi.StringWidth(kind))
		changes := ""
		if b.diff {
			changes = " " + c.moss.Render(fmt.Sprintf("+%d", b.added)) + " " + c.errorStyle.Render(fmt.Sprintf("−%d", b.removed))
		}
		available := width - ansi.StringWidth(label) - ansi.StringWidth(b.stats) - ansi.StringWidth(changes)
		if b.stats != "" {
			available--
		}
		if available > 0 {
			label += c.tool.Render(middle(target, available)) + changes
		}
	}
	lines := []string{columns(label, c.faint.Render(b.stats), width)}
	if !expanded || b.text == "" {
		return lines
	}
	lines = append(lines, "")
	output := wrap(b.text, width-2)
	for _, line := range output[:min(toolPreviewLines, len(output))] {
		lines = append(lines, "  "+c.muted.Render(line))
	}
	hint := ""
	if b.clipped {
		hint = "… more · ctrl+o"
	} else if len(output) > toolPreviewLines {
		hint = fmt.Sprintf("… %d more · ctrl+o", len(output)-toolPreviewLines)
	}
	if hint != "" {
		lines = append(lines, "  "+c.faint.Render(hint))
	}
	return lines
}

func (t *transcript) markdownLines(text string, colors terminalTheme) []string {
	if text != "" && t.markdown != nil {
		text = strings.NewReplacer(codeStart, "�", codeEnd, "�").Replace(text)
		if rendered, err := t.markdown.Render(text); err == nil {
			var lines []string
			inCode := false
			for line := range strings.SplitSeq(strings.Trim(rendered, "\n"), "\n") {
				plain := strings.TrimSpace(ansi.Strip(line))
				if plain == codeStart {
					inCode = true
					continue
				}
				ended := strings.Contains(plain, codeEnd)
				if ended {
					line = strings.ReplaceAll(line, codeEnd, "")
					if plain == codeEnd {
						inCode = false
						continue
					}
				}
				if inCode {
					line = colors.faint.Render("│ ") + ansi.Cut(line, 2, ansi.StringWidth(line))
				}
				lines = append(lines, line)
				if ended {
					inCode = false
				}
			}
			for len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
				lines = lines[1:]
			}
			for len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
				lines = lines[:len(lines)-1]
			}
			return lines
		}
	}
	lines := wrap(text, t.width)
	for i, line := range lines {
		lines[i] = colors.fog.Render(line)
	}
	return lines
}

func wrap(text string, width int) []string {
	text = strings.ReplaceAll(text, "\t", "    ")
	return strings.Split(ansi.Wrap(text, max(1, width), ""), "\n")
}

func firstLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if s := strings.Join(strings.Fields(line), " "); s != "" {
			return s
		}
	}
	return ""
}

func middle(text string, width int) string {
	length := ansi.StringWidth(text)
	if length <= width {
		return text
	}
	if width <= 1 {
		return ansi.Truncate(text, max(0, width), "…")
	}
	left := (width - 1) / 2
	return ansi.Cut(text, 0, left) + "…" + ansi.Cut(text, length-(width-1-left), length)
}
