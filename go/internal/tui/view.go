package tui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	gstyle "charm.land/glamour/v2/ansi"
	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"pi-workspace.local/workspace/internal/theme"
)

type terminalTheme struct {
	fog, user, muted, faint, moss, warning, rain, errorStyle  lip.Style
	tool                                                      lip.Style
	frameBackground, transcriptBackground, composerBackground string
	userBackground, toolBackground                            string
	version                                                   int
	profile                                                   colorprofile.Profile
	plain                                                     bool
}

func (t *terminalTheme) set(plain, transparent bool, profile colorprofile.Profile) {
	t.version++
	t.plain, t.profile = plain, profile
	t.fog, t.user = t.fg(theme.Fog), t.fg(theme.UserText)
	t.tool = t.fg(theme.ToolText)
	t.muted, t.faint = t.fg(theme.Muted), t.fg(theme.Faint)
	t.moss, t.warning, t.rain, t.errorStyle = t.fg(theme.Moss), t.fg(theme.Chanterelle), t.fg(theme.Rain), t.fg(theme.Madrone)
	t.frameBackground, t.transcriptBackground, t.composerBackground = "", "", ""
	t.userBackground, t.toolBackground = "", ""
	if !plain && !transparent && profile == colorprofile.TrueColor {
		t.frameBackground = background(theme.Background)
		t.transcriptBackground = background(theme.Surface)
		t.composerBackground = background(theme.Panel)
		t.userBackground = background(theme.Panel)
		t.toolBackground = ""
	}
}

func (t terminalTheme) blockBackground(kind blockKind) string {
	switch kind {
	case userBlock:
		return t.userBackground
	case toolBlock:
		return t.toolBackground
	default:
		return ""
	}
}

func background(hex string) string {
	return (ansi.Style{}).BackgroundColor(lip.Color(hex)).String()
}

func paintBackground(line, bg string) string {
	if bg == "" {
		return line
	}
	// Foreground spans from Glamour and Lip Gloss can reset the background.
	return bg + strings.NewReplacer("\x1b[m", "\x1b[m"+bg, "\x1b[0m", "\x1b[0m"+bg, "\x1b[49m", bg).Replace(line) + "\x1b[m"
}

func (t terminalTheme) color(hex string) string {
	if t.profile == colorprofile.ANSI {
		switch hex {
		case theme.Moss:
			return "2"
		case theme.Chanterelle:
			return "3"
		case theme.Rain, theme.ToolText:
			return "6"
		case theme.Madrone:
			return "1"
		case theme.Faint, theme.Muted:
			return "8"
		default:
			return "7"
		}
	}
	return hex
}

func (t terminalTheme) fg(hex string) lip.Style {
	s := lip.NewStyle()
	if !t.plain {
		s = s.Foreground(lip.Color(t.color(hex)))
	}
	return s
}

func (t terminalTheme) markdown(width int) *glamour.TermRenderer {
	if width < 8 {
		return nil
	}
	color := func(hex string) *string {
		if t.plain {
			return nil
		}
		value := t.color(hex)
		return &value
	}
	bold := !t.plain
	indent := uint(2)
	rule := t.faint.Render("│ ")
	style := gstyle.StyleConfig{
		Document:    gstyle.StyleBlock{StylePrimitive: gstyle.StylePrimitive{Color: color(theme.Fog)}},
		Heading:     gstyle.StyleBlock{StylePrimitive: gstyle.StylePrimitive{Color: color(theme.Moss), Bold: &bold}},
		Link:        gstyle.StylePrimitive{Color: color(theme.Rain)},
		LinkText:    gstyle.StylePrimitive{Color: color(theme.Rain)},
		Strong:      gstyle.StylePrimitive{Bold: &bold},
		Item:        gstyle.StylePrimitive{BlockPrefix: "• "},
		Enumeration: gstyle.StylePrimitive{BlockPrefix: ". "},
		List:        gstyle.StyleList{LevelIndent: 2},
		Code:        gstyle.StyleBlock{StylePrimitive: gstyle.StylePrimitive{Color: color(theme.Faint)}},
		CodeBlock: gstyle.StyleCodeBlock{StyleBlock: gstyle.StyleBlock{
			Indent:         &indent,
			StylePrimitive: gstyle.StylePrimitive{BlockPrefix: codeStart + "\n", BlockSuffix: codeEnd},
		}},
		BlockQuote: gstyle.StyleBlock{Indent: &indent, IndentToken: &rule},
	}
	if t.transcriptBackground != "" {
		surface, panel := theme.Surface, theme.Panel
		style.Document.BackgroundColor = &surface
		style.Code.BackgroundColor = &panel
	}
	renderer, _ := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width))
	return renderer
}

const frameTop = 4

type frame struct {
	left, width, rows, composerRows, composerGap int
	scrollbar, hint                              bool
}

func composerLayout(height int) (capacity, gap int, hint bool) {
	hint = height >= 20
	available := height - frameTop - 2
	if height >= 12 {
		available -= 3
	}
	gap = min(1, max(0, available-2))
	return min(6, max(1, available-gap-1)), gap, hint
}

func (m *Model) frame() frame {
	w, h := max(1, m.width), max(1, m.height)
	body := min(max(1, w-4), 80)
	capacity, gap, hint := composerLayout(h)
	composerRows := min(max(1, m.draft.Height()), capacity)
	padding := 0
	if h >= 12 {
		padding = 3
	}
	rows := h - frameTop - 2 - composerRows - gap - padding
	return frame{(w - body) / 2, body, max(0, rows), composerRows, gap, w >= 60, hint}
}

func (f frame) transcriptEnd() int          { return frameTop + f.rows }
func (f frame) composerStart() int          { return f.transcriptEnd() + f.composerGap }
func (f frame) bodyWidth() int              { return max(1, f.width-3) }
func (f frame) transcriptPad(total int) int { return max(0, f.rows-total) }

func (f frame) row(body string) string {
	return strings.Repeat(" ", f.left) + ansi.Truncate(body, f.width, "")
}

func (m *Model) View() tea.View {
	f, w := m.frame(), max(1, m.width)
	c := m.colors
	lines := []string{"", f.row(m.header(f.width)), "", ""}
	lines = append(lines, m.viewport(f)...)
	lines = append(lines, make([]string, f.composerGap)...)
	if m.height >= 12 {
		lines = append(lines, f.row(""))
	}
	composer := strings.Split(m.draft.View(), "\n")
	for i := range f.composerRows {
		line := ""
		if i < len(composer) {
			line = composer[i]
		}
		lines = append(lines, f.row("   "+line))
	}
	if m.height >= 12 {
		lines = append(lines, f.row(""), "")
	}
	status := m.status(f.width)

	lines = append(lines, f.row(status), "")
	if m.overlay.kind != overlayNone {
		m.drawOverlay(lines, f)
	}
	for i, line := range lines {
		line = ansi.Truncate(line, w, "")
		bg := c.frameBackground
		if i >= frameTop && i < f.composerStart() {
			bg = c.transcriptBackground
		}
		if i >= f.composerStart() && i < f.composerStart()+f.composerRows+2 && m.height >= 12 {
			body := ansi.Cut(line, f.left, f.left+f.width)
			body += strings.Repeat(" ", max(0, f.width-ansi.StringWidth(body)))
			line = strings.Repeat(" ", f.left) + paintBackground(body, c.composerBackground)
		}
		if m.plain {
			line = ansi.Strip(line)
		}
		if bg != "" {
			line += strings.Repeat(" ", max(0, w-ansi.StringWidth(line)))
		}
		lines[i] = paintBackground(line, bg)
	}
	lines = lines[:min(len(lines), max(1, m.height))]
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = !m.dumb
	if !m.options.NoMouse && !m.dumb {
		view.MouseMode = tea.MouseModeCellMotion
	}
	if !m.dumb {
		view.KeyboardEnhancements.ReportEventTypes = true
		view.KeyboardEnhancements.ReportAlternateKeys = true
		view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
		view.KeyboardEnhancements.ReportAssociatedText = true
	}
	if m.overlay.kind == overlayNone && m.draft.Focused() && f.composerStart() < len(lines) {
		if cursor := m.draft.Cursor(); cursor != nil {
			cursor.X = min(w-1, cursor.X+f.left+3)
			cursor.Shape = tea.CursorBlock
			cursor.Y += f.composerStart()
			if m.height >= 12 {
				cursor.Y++
			}
			view.Cursor = cursor
		}
	}
	return view
}

func (m *Model) header(width int) string {
	c := m.colors
	label := c.moss.Render("meter") + "  " + c.fog.Render(m.itemFor(m.conversation).crumb())
	if m.reconnecting {
		return columns(label, c.warning.Render("○ reconnecting"), width)
	}
	if m.width < 60 {
		return ansi.Truncate(label, width, "…")
	}
	return ansi.Truncate(label, width, "…")
}

func (m *Model) viewport(f frame) []string {
	t, c := &m.transcript, m.colors
	lines := make([]string, f.rows)
	pad := f.transcriptPad(t.total)
	thumbSize, thumbStart := 0, 0
	if t.total > f.rows && f.rows > 0 {
		thumbSize = max(1, f.rows*f.rows/t.total)
		thumbStart = t.top * (f.rows - thumbSize) / (t.total - f.rows)
	}
	for i := range lines {
		line := t.top + i - pad
		body, bg := "", ""
		if b, ok := t.at(line); ok {
			bg = c.blockBackground(b.block.kind)
			selected := !m.focused && line == t.cursor
			gutter := "   "
			if b.block.kind == toolBlock && line == b.start {
				gutter = m.toolGutter(b.block, selected)
			}
			if selected {
				bg = c.userBackground
			}
			body = gutter + b.lines[line-b.start]
		} else if t.total == 0 && i == f.rows/2 {
			body = "  " + c.faint.Render("◴  "+clean(m.conversation.Cwd))
		}
		if t.unseen && i == len(lines)-1 && f.width >= 20 {
			body = columns(body, c.moss.Render("↓ new output · G"), f.width)
		}
		if bg != "" {
			body = ansi.Truncate(body, f.width, "")
			body = paintBackground(body+strings.Repeat(" ", f.width-ansi.StringWidth(body)), bg)
		}
		lines[i] = f.row(body)
		if f.scrollbar && thumbSize > 0 && time.Now().Before(m.scrollbarUntil) {
			glyph := c.faint.Render("▏")
			if i >= thumbStart && i < thumbStart+thumbSize {
				glyph = c.muted.Render("▎")
			}
			lines[i] = columns(lines[i], glyph, f.left+f.width+2)
		}
	}
	return lines
}

func (m *Model) toolGutter(b Block, selected bool) string {
	c := m.colors
	state := " "
	switch b.status {
	case "✓":
		state = c.faint.Render("✓")
	case "✗":
		state = c.errorStyle.Render("✗")
	case "running":
		state = c.warning.Render(m.spinner.View())
	case "pending":
		state = c.faint.Render("○")
	}
	return " " + state + " "
}

type shortHelp []key.Binding

func (h shortHelp) ShortHelp() []key.Binding  { return h }
func (h shortHelp) FullHelp() [][]key.Binding { return [][]key.Binding{h} }

func (m *Model) hintLine(width int) string {
	keys := func(k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
	}
	h := m.help
	h.SetWidth(width)
	h.Styles = help.Styles{ShortKey: m.colors.muted, ShortDesc: m.colors.faint, ShortSeparator: m.colors.faint}
	switch m.overlay.kind {
	case overlayModel:
		return h.View(shortHelp{keys("↑↓", "choose"), keys("⏎", "select"), keys("esc", "close")})
	case overlaySearch:
		return h.View(shortHelp{keys("⏎", "find"), keys("esc", "close")})
	case overlaySwitch:
		return h.View(shortHelp{keys("↑↓", "choose"), keys("⏎", "open"), keys("tab", "projects"), keys("esc", "close")})
	case overlayPager:
		return h.View(shortHelp{keys("j/k", "scroll"), keys("g/G", "top/bottom"), keys("y", "copy"), keys("esc", "close")})
	}
	if m.overlay.kind == overlayHelp {
		return h.View(shortHelp{keys("↑↓", "scroll"), keys("esc", "close")})
	}
	action := "send"
	if m.running() {
		action = "queue"
	}
	h.ShortSeparator = "  "
	return h.View(shortHelp{keys("⏎", action), keys("esc", "scroll"), keys("^k", "switch")})
}

func (m *Model) status(width int) string {
	c := m.colors
	now := time.Now()
	switch {
	case now.Before(m.stopUntil):
		return c.warning.Render("esc again to stop")
	case now.Before(m.quitUntil):
		return c.warning.Render("ctrl+c again to quit")
	}
	if m.notice != "" {
		return ansi.Truncate(c.warning.Render(m.notice), width, "…")
	}
	if now.Before(m.mouseHintUntil) {
		return ansi.Truncate(c.muted.Render("Hold Shift (or Option) to select · --no-mouse"), width, "…")
	}
	path := homePath(clean(m.conversation.Cwd))
	right := c.muted.Render(m.modelLabel())
	if usage := m.state.Current.ContextUsage; usage != nil && usage.Window > 0 {
		pct := theme.Percent(usage.Tokens, usage.Window)
		right += "  " + c.muted.Render("ctx ") + c.contextMeter(pct) + " " + c.muted.Render(fmt.Sprintf("%.0f%%", math.Ceil(pct)))
	}
	if r := m.recording; r != nil && !r.released {
		indicator := " "
		if r.ready {
			indicator = c.errorStyle.Render("■")
		} else if r.blink {
			indicator = c.warning.Render("■")
		}
		right = indicator + "  " + right
	} else if m.running() {
		right = c.warning.Render(m.spinner.View()) + "  " + right
	}
	// Preserve the useful end of a long directory, while reserving the right side.
	available := max(0, width-ansi.StringWidth(right)-1)
	if ansi.StringWidth(path) > available {
		path = ansi.TruncateLeft(path, max(0, ansi.StringWidth(path)-available+1), "…")
	}
	return columns(c.fog.Render(path), right, width)
}

func shortenPath(path string) string {
	if path == "" || path == string(filepath.Separator) {
		return path
	}
	name := filepath.Base(path)
	if path == name {
		return path
	}
	return "…" + string(filepath.Separator) + name
}
func columns(left, right string, width int) string {
	if right == "" {
		return ansi.Truncate(left, max(0, width), "…")
	}
	right = ansi.Truncate(right, max(0, width), "…")
	remain := width - ansi.StringWidth(right)
	left = ansi.Truncate(left, max(0, remain-1), "…")
	return left + strings.Repeat(" ", max(0, remain-ansi.StringWidth(left))) + right
}

func (t terminalTheme) contextMeter(percent float64) string {
	steps := int(math.Ceil(percent * theme.MeterCells / 100))
	fill := t.muted
	if percent > 90 {
		fill = t.errorStyle
	} else if percent > 70 {
		fill = t.warning
	}
	var meter strings.Builder
	for i := range theme.MeterCells {
		if i < steps {
			meter.WriteString(fill.Render("━"))
		} else {
			meter.WriteString(t.faint.Render("─"))
		}
	}
	return meter.String()
}

func activityAge(ms *int) string {
	if ms == nil {
		return ""
	}
	elapsed := time.Since(time.UnixMilli(int64(*ms)))
	switch {
	case elapsed < time.Minute:
		return "now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}
