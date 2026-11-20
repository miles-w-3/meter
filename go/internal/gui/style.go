package gui

import (
	_ "embed"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Cyenoch/mygoui"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
)

const monoFont = "JetBrains Mono"

//go:embed fonts/Geist.ttf
var geist []byte

//go:embed fonts/JetBrainsMono.ttf
var jetBrainsMono []byte

var registerFonts = sync.OnceValue(func() error {
	return errors.Join(ui.RegisterFont(geist, "Geist"), ui.RegisterFont(jetBrainsMono, monoFont))
})

var (
	theme      = meterTheme()
	background = theme.Background
	sidebar    = theme.Surface
	panel      = theme.SurfaceHover
	ink        = theme.Text
	muted      = theme.TextMuted
	accent     = theme.Accent
	warning    = theme.Warning
	gauge      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M4 17a8 8 0 1 1 16 0M12 15l4-5M6 14h1m10 0h1M12 7v1" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/><circle cx="12" cy="15" r="1.4" fill="currentColor"/></svg>`))
)

// Configure the component library from the same palette as the native controls.
func init() {
	patch := &uitheme.PalettePatch{
		Background: &background, Foreground: &ink,
		Card: &sidebar, CardForeground: &ink, Popover: &panel, PopoverForeground: &ink,
		Primary: &accent, PrimaryForeground: &background,
		Secondary: &panel, SecondaryForeground: &ink,
		MutedBackground: &panel, MutedForeground: &muted,
		Accent: &panel, AccentForeground: &ink,
		Destructive: &theme.Danger, DestructiveForeground: &ink,
		Border: &theme.Border, Input: &theme.Border, Ring: &accent,
		Sidebar: &sidebar, SidebarForeground: &ink, SidebarBorder: &theme.Border,
		Code: &sidebar, CodeForeground: &ink,
	}
	uitheme.Configure(uitheme.Preferences{
		FontFamily: "Geist", MonoFontFamily: monoFont, Radius: 7,
		Palette: uitheme.PaletteOverrides{Dark: patch},
	})
}

func meterTheme() *ui.Theme {
	t := ui.DarkTheme()
	t.Background, t.Surface, t.SurfaceHover = ui.Hex("#0c110e"), ui.Hex("#142019"), ui.Hex("#1b2c21")
	t.SurfacePressed = ui.Hex("#294334")
	t.Border = ui.Hex("#30483a")
	t.Text, t.TextMuted = ui.Hex("#e5eee7"), ui.Hex("#8ca596")
	t.Accent, t.AccentText = ui.Hex("#6db888"), ui.Hex("#0c110e")
	t.AccentHover, t.AccentPressed = ui.Hex("#8dcca3"), ui.Hex("#4b9368")
	t.Danger, t.Warning, t.Success = ui.Hex("#e46d61"), t.TextMuted, t.Accent
	t.Focus, t.InactiveAccent = t.Accent, t.Accent
	t.Selection = ui.RGBA(109, 184, 136, .25)
	t.Scrollbar = ui.RGBA(140, 165, 150, .35)
	t.Font, t.Radius = "Geist", 7
	return t
}

func pathLabel(c *ui.Context, path string) ui.Element {
	last := filepath.Base(path)
	prefix := ""
	if path == "" {
		last = ""
	} else if dir := filepath.Dir(path); dir != "." && dir != path {
		prefix = strings.TrimSuffix(dir, string(filepath.Separator)) + string(filepath.Separator)
		if parent := filepath.Dir(dir); parent != "." && parent != dir {
			prefix = "…" + string(filepath.Separator) + filepath.Base(dir) + string(filepath.Separator)
		}
	}
	return ui.RichText(c, ui.Span{Text: prefix, Color: muted}, ui.Span{Text: last, Color: ink}).
		Font(monoFont).FontSize(11).MinWidth(0).Grow(1).NoWrap().Ellipsis("…").Tooltip(path)
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if clean := strings.Join(strings.Fields(line), " "); clean != "" {
			return clean
		}
	}
	return ""
}

const paneGutter = 12

var (
	folderIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 7v12h18V8H11L9 5H3z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>`))
	closeIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="m6 6 12 12M6 18 18 6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`))
	refreshIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M20 10a8 8 0 0 0-14-4L3 9m0-5v5h5m-4 5a8 8 0 0 0 14 4l3-3m0 5v-5h-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
)

func iconButton(c *ui.Context, icon *ui.SVG, label string, disabled bool) ui.Element {
	return mygoui.Button(c, mygoui.ButtonOptions{
		Label: label, Icon: icon, IconOnly: true, Variant: mygoui.ButtonGhost,
		Size: uitheme.ButtonExtraSmall, Tooltip: label, Disabled: disabled,
	})
}

func ghostPlus(c *ui.Context, label string, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Size(20, 20).Label(label).Tooltip(label).Disabled(disabled)
	if b.Hovered() && !disabled {
		b.Background(panel)
	}
	b.Children(func() { text(c, "+", 17, muted) })
	return b
}

func sectionLabel(c *ui.Context, label string) ui.Element {
	return text(c, label, 11, muted).LetterSpacing(1.15).FontWeight(600)
}
