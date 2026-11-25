package gui

import (
	"fmt"
	"math"
	"os"

	"github.com/Cyenoch/mygoicons/lucide"
	"github.com/Cyenoch/mygoui/content"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	mmd "github.com/egoist/mygo/ui/markdown"
	"pi-workspace.local/workspace/internal/mermaid"
)

// Render cache is accessed only on the UI thread; the helper itself is bounded
// and run on a background goroutine. Conversation history keeps source only.
type mermaidEntry struct {
	result      mermaid.Result
	image       *ui.SVG
	preview     *ui.Bitmap
	owner       *app
	detail      *ui.Bitmap
	detailBytes int // reserved or resident, never both
	views       map[*diagramCanvasState]ui.Services
	popouts     int
	err         error
	loading     bool
	lastUsed    uint64
}

var diagramJobs = make(chan struct{}, 2)

type mermaidBlockState struct{ source bool }
type mermaidBlockKey struct{}

func (a *app) diagram(source string) *mermaidEntry {
	if a.mermaidCache == nil {
		a.mermaidCache = make(map[string]*mermaidEntry)
	}
	a.mermaidClock++
	if item := a.mermaidCache[source]; item != nil {
		item.lastUsed = a.mermaidClock
		return item
	}
	// Do not evict running jobs or let the transcript queue unbounded work.
	if len(a.mermaidCache) >= 16 {
		a.evictOldestDiagram()
	}
	if len(a.mermaidCache) >= 16 || a.ctx == nil || a.update == nil {
		return &mermaidEntry{err: fmt.Errorf("renderer unavailable")}
	}
	item := &mermaidEntry{owner: a, loading: true, lastUsed: a.mermaidClock}
	a.mermaidCache[source] = item
	go func() {
		select {
		case diagramJobs <- struct{}{}:
			defer func() { <-diagramJobs }()
		case <-a.ctx.Done():
			return
		}
		result, err := mermaid.Render(a.ctx, source)
		var image *ui.SVG
		var bitmap *ui.Bitmap
		if err == nil {
			image, err = ui.ParseSVG([]byte(result.SVG))
		}
		if err == nil {
			bitmap = diagramPreview(result, image)
		}
		a.update(func() {
			if a.mermaidCache[source] != item {
				return
			}
			item.result, item.image, item.preview, item.err, item.loading = result, image, bitmap, err, false
		})
	}()
	return item
}

// evictOldestDiagram keeps recent results and never evicts an in-flight job.
func (a *app) evictOldestDiagram() {
	var oldest string
	var candidate *mermaidEntry
	for key, item := range a.mermaidCache {
		if !item.loading && item != a.detailJob && len(item.views) == 0 && item.popouts == 0 && (candidate == nil || item.lastUsed < candidate.lastUsed) {
			oldest, candidate = key, item
		}
	}
	if candidate != nil {
		delete(a.mermaidCache, oldest)
	}
}

var (
	diagramCodeIcon   = lucide.CodeXML()
	diagramSaveIcon   = lucide.Save()
	diagramPopOutIcon = lucide.ExternalLink()
)

func (a *app) mermaidBlock(c *ui.Context, b *mmd.CodeBlockView) {
	source := b.Code()
	item := a.diagram(source)
	root := ui.Column(c).FillWidth().MinWidth(0).Gap(6).Padding(8).Radius(6).Background(panel)
	state := *ui.Local(root, mermaidBlockKey{}, func() *mermaidBlockState { return &mermaidBlockState{} })
	actions := func() {
		toggle := iconButton(c, diagramCodeIcon, "Show source", false).Role(ui.RoleToggleButton).Checked(state.source)
		if state.source {
			toggle.Background(selectedFill)
		}
		if toggle.Clicked() {
			state.source = !state.source
		}
		if item.image != nil && item.err == nil && !item.loading {
			if iconButton(c, diagramSaveIcon, "Save SVG", false).Clicked() {
				a.saveDiagram(item.result.SVG)
			}
			if iconButton(c, diagramPopOutIcon, "Pop out diagram", false).Clicked() {
				a.openDiagram(item)
			}
		}
	}
	root.Children(func() {
		if item.loading || item.err != nil || state.source {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				actions()
			})
			if item.loading {
				text(c, "Rendering diagram…", 12, muted)
			} else if item.err != nil {
				text(c, "Diagram unavailable: "+item.err.Error(), 12, warning)
			}
			mermaidSource(c, source, b.Language)
			return
		}
		diagramCanvas(c, item, 220, actions)
	})
}

// Keep the copy control in the code block header and copy the exact source;
// Highlighter's built-in copy trims whitespace around the displayed code.
func mermaidSource(c *ui.Context, source, language string) {
	ui.Box(c).FillWidth().MinWidth(0).Children(func() {
		content.Highlighter(c, content.HighlighterOptions{Code: source, Language: language, NoCopy: true})
		content.CopyButton(c, content.CopyButtonOptions{Text: source, Label: "Copy source", Size: uitheme.ButtonSmall}).Absolute().Top(4).Right(8)
	})
}

func (a *app) saveDiagram(svg string) {
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.window, Title: "Save Mermaid SVG", DefaultPath: "diagram.svg"})
		if err == nil && path != "" {
			err = os.WriteFile(path, []byte(svg), 0644)
		}
		if err != nil && a.update != nil {
			a.update(func() { a.notice = "Unable to save SVG: " + err.Error() })
		}
	}()
}

func (a *app) openDiagram(item *mermaidEntry) {
	item.popouts++
	win := mygo.NewWindow(mygo.WindowOptions{Title: "Mermaid diagram", Width: 1000, Height: 720, MinWidth: 440, MinHeight: 320, Content: ui.View(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Column(c).Fill().Padding(12).Gap(8).Background(background).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				text(c, "Mermaid diagram", 15, ink).Grow(1)
				if iconButton(c, diagramSaveIcon, "Save SVG", false).Clicked() {
					a.saveDiagram(item.result.SVG)
				}
			})
			diagramCanvas(c, item, 0)
		})
	})})
	win.OnClose(func(*mygo.CloseEvent) { item.popouts-- })
}

type diagramCanvasKey struct{}
type diagramCanvasState struct {
	scroll                ui.ScrollState
	zoom, fitZoom         float32
	ready, drag           bool
	auto                  bool
	lastWidth, lastHeight float32
	x, y                  float32
	scale                 float32
	pinned                *mermaidEntry
}

const diagramMargin float32 = 600

func (s *diagramCanvasState) input(e ui.InputEvent) bool {
	switch e.Kind {
	case ui.InputPointerDown:
		if e.Button != 0 {
			return false
		}
		s.drag = true
		s.auto = false
		s.x, s.y = e.X, e.Y
		return true
	case ui.InputPointerMove:
		if !s.drag {
			return false
		}
		s.scroll.X = max(0, min(s.scroll.MaxX, s.scroll.X+s.x-e.X))
		s.scroll.Y = max(0, min(s.scroll.MaxY, s.scroll.Y+s.y-e.Y))
		s.x, s.y = e.X, e.Y
		return true
	case ui.InputPointerUp:
		if !s.drag {
			return false
		}
		s.drag = false
		return true
	case ui.InputZoom:
		s.auto = false
		s.zoomAt(s.zoom*e.Scale, e.X, e.Y)
		return true
	}
	return false
}
func (s *diagramCanvasState) zoomAt(value, x, y float32) {
	if value <= 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return
	}
	// Keep the usual manual floor, but let large diagrams zoom back to Fit.
	floor := float32(.25)
	if s.fitZoom > 0 {
		floor = min(floor, s.fitZoom)
	}
	value = max(floor, min(4, value))
	factor := value / s.zoom
	s.scroll.X = max(0, diagramMargin+(s.scroll.X+x-diagramMargin)*factor-x)
	s.scroll.Y = max(0, diagramMargin+(s.scroll.Y+y-diagramMargin)*factor-y)
	s.zoom = value
}

// fit centers the complete diagram, leaving 12 DIPs around its edges.
func (s *diagramCanvasState) fit(bounds ui.Rect, result mermaid.Result) {
	w, h := max(float32(1), bounds.W-24), max(float32(1), bounds.H-24)
	s.fitZoom = min(4, min(w/float32(result.Width), h/float32(result.Height)))
	s.zoom = s.fitZoom
	s.scroll.X = max(0, diagramMargin+float32(result.Width)*s.zoom/2-bounds.W/2)
	s.scroll.Y = max(0, diagramMargin+float32(result.Height)*s.zoom/2-bounds.H/2)
}

// inlineDiagramGeometry keeps small diagrams at native scale, fits wide ones
// down to the viewport, and bounds the inline height for tall diagrams.
func inlineDiagramGeometry(width float32, result mermaid.Result) (float32, float32) {
	const minCanvas, maxCanvas, chrome, inset = 140, 420, 38, 24
	if width <= 0 {
		width = 600
	}
	zoom := max(float32(.25), min(float32(1), max(float32(1), width-inset)/float32(result.Width)))
	height := max(float32(minCanvas), min(float32(maxCanvas), float32(result.Height)*zoom+chrome+inset))
	return zoom, height
}
func diagramCanvas(c *ui.Context, item *mermaidEntry, height float32, actions ...func()) {
	root := ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Gap(4)
	s := *ui.Local(root, diagramCanvasKey{}, func() *diagramCanvasState { return &diagramCanvasState{zoom: 1, auto: true} })
	// MyGo exposes the backing scale on the painter, not the build context.
	// Observe this window (including monitor changes) and rebuild on change.
	services := c.Services()
	root.Draw(func(p *ui.Painter, _ ui.Rect) {
		if scale := p.Scale(); scale > 0 && scale != s.scale {
			s.scale = scale
			services.Invalidate()
		}
	})
	if s.pinned != nil && s.pinned != item {
		s.releaseDetail()
	}
	root.OnUnmount(s.releaseDetail)
	if !s.ready {
		s.ready = true
		s.scroll.X, s.scroll.Y = diagramMargin, diagramMargin
	}
	if height > 0 {
		width := root.Bounds().W
		_, measuredHeight := inlineDiagramGeometry(width, item.result)
		root.Grow(0).Height(measuredHeight).MinHeight(measuredHeight).Shrink(0)
	}
	root.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			bounds := s.scroll.Bounds(c)
			if s.auto && bounds.W > 0 && bounds.H > 0 &&
				(math.Abs(float64(bounds.W-s.lastWidth)) > .5 || math.Abs(float64(bounds.H-s.lastHeight)) > .5) {
				s.fit(bounds, item.result)
				s.lastWidth, s.lastHeight = bounds.W, bounds.H
			}
			if ghost(c, "−", false) {
				s.auto = false
				s.zoomAt(s.zoom/1.25, bounds.W/2, bounds.H/2)
			}
			text(c, fmt.Sprintf("%.0f%%", s.zoom*100), 12, muted)
			if ghost(c, "+", false) {
				s.auto = false
				s.zoomAt(s.zoom*1.25, bounds.W/2, bounds.H/2)
			}
			if ghost(c, "Fit", false) && bounds.W > 0 && bounds.H > 0 {
				s.auto = false
				s.fit(bounds, item.result)
			}
			if ghost(c, "Actual Size", false) {
				s.auto = false
				s.zoom = 1
				s.scroll.X, s.scroll.Y = diagramMargin, diagramMargin
			}
			if len(actions) > 0 && actions[0] != nil {
				ui.Spacer(c)
				actions[0]()
			}
		})
		cursor := ui.CursorGrab
		if s.drag {
			cursor = ui.CursorGrabbing
		}
		ui.ScrollBoth(c).Label("Diagram viewport").Grow(1).MinHeight(0).MinWidth(0).Scrollbars(true).TrackScroll(&s.scroll).Cursor(cursor).HandleInput(s.input).Background(background).Children(func() {
			ui.Column(c).Padding(diagramMargin).Children(func() {
				s.draw(c, item, s.scroll.Bounds(c))
			})
		})
	})
}
