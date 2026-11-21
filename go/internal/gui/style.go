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
	palette "pi-workspace.local/workspace/internal/theme"
)

const monoFont = "JetBrains Mono"

//go:embed fonts/Geist.ttf
var geist []byte

//go:embed fonts/JetBrainsMono.ttf
var jetBrainsMono []byte

//go:embed fonts/SourceSerif4.ttf
var sourceSerif []byte

//go:embed assets/fir-title.png
var firTitlePNG []byte

var firTitle = func() ui.ImageSource {
	image, err := ui.DecodeImage(firTitlePNG)
	if err != nil {
		panic(err)
	}
	return image
}()

var registerFonts = sync.OnceValue(func() error {
	return errors.Join(ui.RegisterFont(geist, "Geist"), ui.RegisterFont(jetBrainsMono, monoFont), ui.RegisterFont(sourceSerif, "Source Serif 4"))
})

var (
	theme          = meterTheme()
	background     = theme.Background
	sidebar        = theme.Background
	contentSurface = theme.Surface
	panel          = theme.SurfaceHover
	ink            = theme.Text
	muted          = theme.TextMuted
	accent         = theme.Accent
	warning        = theme.Warning
	idleStatus     = ui.Hex("#6b7b70")
	faint          = ui.Hex("#7e9183")
	runningColor   = ui.Hex(palette.Chanterelle)
	rain           = ui.Hex("#7f9fb0")
	selectedFill   = ui.RGBA(143, 174, 122, .17)
	highlight      = ui.RGBA(255, 255, 255, .04)
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
		Border: &theme.Border, Input: &theme.Border, Ring: &theme.Focus,
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
	t.Background, t.Surface, t.SurfaceHover = ui.Hex(palette.Background), ui.Hex(palette.Surface), ui.Hex(palette.Panel)
	t.SurfacePressed = ui.Hex("#222f27")
	t.Border = ui.RGBA(200, 220, 205, .08)
	t.Text, t.TextMuted = ui.Hex(palette.Fog), ui.Hex(palette.Muted)
	t.Accent, t.AccentText = ui.Hex(palette.Moss), ui.Hex(palette.Background)
	t.AccentHover, t.AccentPressed = ui.Hex("#a3bf8e"), ui.Hex("#7a9966")
	t.Danger, t.Warning, t.Success = ui.Hex(palette.Madrone), ui.Hex(palette.Chanterelle), ui.Hex("#86a878")
	t.Focus, t.InactiveAccent = ui.Hex(palette.Rain), t.Accent
	t.Selection = ui.RGBA(143, 174, 122, .18)
	t.Scrollbar = ui.RGBA(190, 210, 195, .22)
	t.ScrollbarStyle.Hover = ui.RGBA(190, 210, 195, .38)
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
	return ui.RichText(c, ui.Span{Text: prefix, Color: faint}, ui.Span{Text: last, Color: ink}).
		Font(monoFont).FontSize(12).MinWidth(0).Grow(1).NoWrap().Ellipsis("…").Tooltip(path)
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if clean := strings.Join(strings.Fields(line), " "); clean != "" {
			return clean
		}
	}
	return ""
}

const (
	paneGutter    = 12
	paneTextInset = 24
)

func paneContent(c *ui.Context, children func()) {
	ui.Row(c).FillWidth().MinWidth(0).Padding(0, paneGutter).Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MaxWidth(720).MinWidth(0).Children(children)
	})
}

var (
	newPaneIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect x="5" y="2.5" width="14" height="19" rx="2" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M12 8v8M8 12h8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`))
	folderIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 7v12h18V8H11L9 5H3z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>`))
	closeIcon    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="m6 6 12 12M6 18 18 6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`))
	refreshIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M20 10a8 8 0 0 0-14-4L3 9m0-5v5h5m-4 5a8 8 0 0 0 14 4l3-3m0 5v-5h-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	settingsIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M12 2h2l.6 2.5 2 .8 2.2-1.3 1.4 1.4-1.3 2.2.8 2L22 10v4l-2.3.5-.8 2 1.3 2.2-1.4 1.4-2.2-1.3-2 .8L14 22h-4l-.5-2.4-2-.8-2.2 1.3-1.4-1.4 1.3-2.2-.8-2L2 14v-4l2.4-.5.8-2-1.3-2.2 1.4-1.4 2.2 1.3 2-.8L10 2z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>`))
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
