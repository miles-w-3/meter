package gui

import (
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/mermaid"
)

const (
	detailLimit = 48 << 20
	detailPool  = 96 << 20
)

func directDiagram(w, h, scale float32) bool {
	pw, ph := math.Ceil(float64(w*scale)), math.Ceil(float64(h*scale))
	return max(pw, ph) <= 4096 && pw*ph <= 2<<20
}

// Whole surfaces keep their pixel dimensions throughout pan and zoom. The
// texture cap is independent of MyGo's smaller direct-SVG picture limit.
func surfaceSize(r mermaid.Result, sample float64, side, bytes int) (int, int) {
	if bytes < 4 {
		return 0, 0
	}
	sample = min(sample, float64(side)/max(r.Width, r.Height), math.Sqrt(float64(bytes/4)/(r.Width*r.Height)))
	w, h := max(1, int(r.Width*sample)), max(1, int(r.Height*sample))
	// A very thin surface may round up to one pixel on its short axis.
	w = min(w, bytes/4)
	h = min(h, bytes/4/w)
	return w, h
}

func diagramPreview(r mermaid.Result, svg *ui.SVG) *ui.Bitmap {
	w, h := surfaceSize(r, 1, 1536, 2<<20)
	return ui.NewBitmap(svg.Image(w, h, nil))
}

func (s *diagramCanvasState) releaseDetail() {
	if item := s.pinned; item != nil {
		item.owner.mermaidClock++
		item.lastUsed = item.owner.mermaidClock
		delete(item.views, s)
		s.pinned = nil
		item.owner.scheduleDiagramDetail()
	}
}

func (s *diagramCanvasState) draw(c *ui.Context, item *mermaidEntry, bounds ui.Rect) {
	w, h := float32(item.result.Width)*s.zoom, float32(item.result.Height)*s.zoom
	// Do not start work at the provisional 100% size before auto-fit runs.
	measured := s.scale > 0 && bounds.W > 0 && bounds.H > 0
	if (s.pinned == nil || item.detail == nil) && directDiagram(w, h, s.scaleOrDefault()) {
		s.releaseDetail()
		ui.Image(c, item.image).Label("Mermaid image").Size(w, h)
		return
	}
	if s.pinned == nil && measured && item.owner != nil {
		s.pinned = item
		if item.views == nil {
			item.views = make(map[*diagramCanvasState]ui.Services)
		}
		item.views[s] = c.Services()
		a := item.owner
		a.mermaidClock++
		item.lastUsed = a.mermaidClock
		a.scheduleDiagramDetail()
	}
	bitmap := item.preview
	if s.pinned == item && item.detail != nil {
		bitmap = item.detail
	}
	if bitmap != nil {
		ui.Image(c, bitmap).Label("Mermaid image").Size(w, h).Fit(ui.FillBox)
	} else {
		ui.Box(c).Size(w, h)
	}
}

func (s *diagramCanvasState) scaleOrDefault() float32 {
	if s.scale > 0 {
		return s.scale
	}
	return 2
}

// All admission, reservations, pins and eviction are owned by the UI thread.
// Unmount or completion drives the queue, never a pan event or a polling timer.
func (a *app) scheduleDiagramDetail() {
	if a.detailJob != nil || a.ctx == nil || a.ctx.Err() != nil {
		return
	}
	available := detailPool
	for _, e := range a.mermaidCache {
		if len(e.views) > 0 {
			available -= e.detailBytes
		}
	}
	side := 4096 // conservative for backends without an exposed texture limit
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		side = 16384 // Apple Silicon Metal
	}
	var next *mermaidEntry
	var w, h int
	for _, e := range a.mermaidCache {
		if len(e.views) == 0 || e.detail != nil {
			continue
		}
		sample := float32(1)
		for view := range e.views {
			sample = max(sample, min(2, view.scaleOrDefault()))
		}
		pw, ph := surfaceSize(e.result, float64(sample), side, min(detailLimit, available))
		ow, oh := e.preview.Size()
		// Keep the preview unless detail adds at least 25% linear resolution.
		if max(pw, ph)*4 <= max(ow, oh)*5 {
			continue
		}
		if next == nil || e.lastUsed < next.lastUsed {
			next, w, h = e, pw, ph
		}
	}
	if next == nil {
		return
	}
	cost := 4 * w * h
	free := detailPool
	for _, e := range a.mermaidCache {
		free -= e.detailBytes
	}
	for free < cost {
		var oldest *mermaidEntry
		for _, e := range a.mermaidCache {
			if e.detail != nil && len(e.views) == 0 && (oldest == nil || e.lastUsed < oldest.lastUsed) {
				oldest = e
			}
		}
		if oldest == nil {
			return
		}
		free += oldest.detailBytes
		oldest.detail, oldest.detailBytes = nil, 0
	}
	next.detailBytes = cost
	a.detailJob = next
	go func() {
		b := ui.NewBitmap(next.image.Image(w, h, nil))
		a.update(func() {
			next.detail = b
			a.detailJob = nil
			for _, v := range next.views {
				v.Invalidate()
			}
			a.scheduleDiagramDetail()
		})
	}()
}
