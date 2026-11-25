package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/mermaid"
)

func TestMermaidTranscriptControls(t *testing.T) {
	source := "flowchart LR\n A --> B\n"
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 60" width="100" height="60"><rect width="100" height="60" fill="red"/></svg>`
	image, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	result := mermaid.Result{SVG: svg, Width: 100, Height: 60}
	item := &mermaidEntry{result: result, image: image}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: item}}
	markdown := "```mermaid\n" + source + "```"
	driver := ui.NewTester(func(c *ui.Context) { app.prose(c, markdown, false) }, 900, 600)
	driver.Frame()
	for _, label := range []string{"Show source", "Save SVG", "Pop out diagram"} {
		if _, ok := driver.Find(label); !ok {
			t.Fatalf("missing control %q: %v", label, driver.Texts())
		}
	}
	zoom, ok := driver.Find("Fit")
	if !ok {
		t.Fatal("missing zoom controls")
	}
	toggle, ok := driver.Find("Show source")
	if !ok {
		t.Fatal("missing source toggle")
	}
	if zoom.Y >= toggle.Y+toggle.H || toggle.Y >= zoom.Y+zoom.H {
		t.Fatalf("zoom and actions must share a row: %v vs %v", zoom, toggle)
	}
	save, _ := driver.Find("Save SVG")
	popout, _ := driver.Find("Pop out diagram")
	if popout.X <= save.X {
		t.Fatal("pop-out should be to the right of save")
	}
	if _, ok := driver.Find("Copy source"); ok {
		t.Fatal("redundant toolbar copy control remains")
	}
	for _, text := range driver.Texts() {
		if text == "Mermaid" {
			t.Fatal("redundant outer Mermaid label remains")
		}
	}
	if err := driver.Click("Show source"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(driver.Texts(), " "), "flowchart") {
		t.Fatal("source not shown")
	}
	copyButton, found := driver.Find("Copy source")
	if !found {
		t.Fatal("source panel copy button missing")
	}
	toggle, _ = driver.Find("Show source")
	if copyButton.Y < toggle.Y+toggle.H {
		t.Fatalf("copy button should be in the source panel, below toolbar: %v vs %v", copyButton, toggle)
	}
	if err := driver.Click("Copy source"); err != nil {
		t.Fatal(err)
	}
	if driver.Clipboard() != source {
		t.Fatalf("copied source %q", driver.Clipboard())
	}
	if err := driver.Click("Show source"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(driver.Texts(), " "), "flowchart") {
		t.Fatal("source remains visible after toggle")
	}

}

func TestMermaidStreamingAndFailureFallback(t *testing.T) {
	source := "flowchart LR\n A --> B\n"
	app := &app{}
	markdown := "```mermaid\n" + source + "```"
	driver := ui.NewTester(func(c *ui.Context) { app.prose(c, markdown, true) }, 900, 600)
	driver.Frame()
	if driver.HasText("Save SVG") || len(app.mermaidCache) != 0 {
		t.Fatal("streaming message scheduled a render")
	}
	driver = ui.NewTester(func(c *ui.Context) { app.prose(c, markdown, false) }, 900, 600)
	driver.Frame()
	if !driver.HasText("Diagram unavailable: renderer unavailable") {
		t.Fatalf("missing error fallback: %v", driver.Texts())
	}
	if !strings.Contains(strings.Join(driver.Texts(), " "), "flowchart") {
		t.Fatal("source not shown on failure")
	}
}

func TestDiagramCanvasPanAndZoom(t *testing.T) {
	s := diagramCanvasState{zoom: 1}
	s.scroll.X, s.scroll.Y = 600, 600
	s.scroll.MaxX, s.scroll.MaxY = 1200, 1200
	if !s.input(ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, X: 100, Y: 100}) {
		t.Fatal("press not captured")
	}
	s.input(ui.InputEvent{Kind: ui.InputPointerMove, Button: 0, X: 70, Y: 80})
	if s.scroll.X != 630 || s.scroll.Y != 620 {
		t.Fatalf("incorrect pan %v,%v", s.scroll.X, s.scroll.Y)
	}
	s.input(ui.InputEvent{Kind: ui.InputPointerUp, Canceled: true})
	if s.drag {
		t.Fatal("canceled drag remained active")
	}
	beforeX, beforeY := s.scroll.X, s.scroll.Y
	s.input(ui.InputEvent{Kind: ui.InputZoom, Scale: 2, X: 100, Y: 100})
	if s.zoom != 2 || s.scroll.X != diagramMargin+2*(beforeX+100-diagramMargin)-100 || s.scroll.Y != diagramMargin+2*(beforeY+100-diagramMargin)-100 {
		t.Fatal("incorrect zoom anchor")
	}
	s.input(ui.InputEvent{Kind: ui.InputZoom, Scale: 100, X: 100, Y: 100})
	if s.zoom != 4 {
		t.Fatalf("zoom not capped: %v", s.zoom)
	}
}

func TestMermaidInlineCanvasPaints(t *testing.T) {
	source := "flowchart LR\n A --> B\n"
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200" width="400" height="200"><rect width="400" height="200" fill="#ff0000"/></svg>`
	image, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	result := mermaid.Result{SVG: svg, Width: 400, Height: 200}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: {result: result, image: image}}}
	markdown := "```mermaid\n" + source + "```"
	driver := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { app.prose(c, markdown, false) }) }, 800, 600)
	driver.Frame()
	check := func(stage string) {
		t.Helper()
		img := driver.Image()
		n := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				p := img.RGBAAt(x, y)
				if p.R > 200 && p.G < 80 && p.B < 80 {
					n++
				}
			}
		}
		if n < 100 {
			t.Errorf("%s: image missing: %d red pixels; texts: %v", stage, n, driver.Texts())
		}
	}

	check("initial")
	if err := driver.Click("Fit"); err != nil {
		t.Fatal(err)
	}
	check("fit")
}

func TestMermaidPaintsInVirtualTranscript(t *testing.T) {
	source := "flowchart LR\n A --> B\n"
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200" width="400" height="200"><rect width="400" height="200" fill="#ff0000"/></svg>`
	image, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	result := mermaid.Result{SVG: svg, Width: 400, Height: 200}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: {result: result, image: image}}}
	state := &ui.ListState{}
	markdown := "```mermaid\n" + source + "```"
	driver := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.List(c, state, 1, func(int) { paneContent(c, func() { app.prose(c, markdown, false) }) }).Grow(1).MinHeight(0).MinWidth(0)
		})
	}, 800, 600)
	driver.SetScale(2)
	driver.Frame()
	check := func(stage string) {
		t.Helper()
		frame := driver.Image()
		colored := 0
		for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
			for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
				p := frame.RGBAAt(x, y)
				if p.R > 200 && p.G < 80 && p.B < 80 {
					colored++
				}
			}
		}
		if colored < 100 {
			t.Errorf("%s: diagram was not painted in list (%d red pixels)", stage, colored)
		}
	}
	check("initial")
	if err := driver.Click("Fit"); err != nil {
		t.Fatal(err)
	}
	check("fit")
}

func TestInlineDiagramGeometry(t *testing.T) {
	for _, tc := range []struct {
		name         string
		width        float32
		result       mermaid.Result
		zoom, height float32
	}{
		{"small", 600, mermaid.Result{Width: 100, Height: 60}, 1, 140},
		{"wide", 524, mermaid.Result{Width: 1000, Height: 200}, .5, 162},
		{"tall", 600, mermaid.Result{Width: 200, Height: 1000}, 1, 420},
		{"huge", 224, mermaid.Result{Width: 4000, Height: 3000}, .25, 420},
	} {
		t.Run(tc.name, func(t *testing.T) {
			zoom, height := inlineDiagramGeometry(tc.width, tc.result)
			if zoom != tc.zoom || height != tc.height {
				t.Fatalf("got %v, %v; want %v, %v", zoom, height, tc.zoom, tc.height)
			}
		})
	}
}

func TestInlineDiagramFitsAvailableWidth(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 100" width="1000" height="100"><rect width="1000" height="100" fill="red"/></svg>`
	image, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	item := &mermaidEntry{result: mermaid.Result{SVG: svg, Width: 1000, Height: 100}, image: image}
	driver := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { diagramCanvas(c, item, 220) }) }, 400, 500)
	driver.Frame()
	driver.Frame()
	driver.Frame()
	if !driver.HasText("38%") {
		t.Fatalf("diagram did not fit 400px viewport, got %v", driver.Texts())
	}
}

func TestDiagramDefaultsToFitAndActualSize(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height float64
		inline        bool
	}{
		{"tall inline", 200, 1000, true}, {"small inline", 100, 60, true}, {"popout", 100, 60, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" width="%g" height="%g"><rect width="%g" height="%g" fill="red"/></svg>`, tc.width, tc.height, tc.width, tc.height, tc.width, tc.height)
			image, err := ui.ParseSVG([]byte(svg))
			if err != nil {
				t.Fatal(err)
			}
			item := &mermaidEntry{result: mermaid.Result{SVG: svg, Width: tc.width, Height: tc.height}, image: image}
			h := float32(0)
			if tc.inline {
				h = 220
			}
			driver := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { diagramCanvas(c, item, h) }) }, 400, 500)
			for range 5 {
				driver.Frame()
			}
			percent := func() string {
				for _, text := range driver.Texts() {
					if strings.HasSuffix(text, "%") {
						return text
					}
				}
				return ""
			}
			before := percent()
			if before == "" {
				t.Fatal("missing zoom")
			}
			reset, _ := driver.Find("Actual Size")
			fit, _ := driver.Find("Fit")
			plus, _ := driver.Find("+")
			if !(plus.X < fit.X && fit.X < reset.X) {
				t.Fatalf("wrong order: + %v reset %v fit %v", plus, reset, fit)
			}
			if err := driver.Click("Fit"); err != nil {
				t.Fatal(err)
			}
			driver.Frame()
			if got := percent(); got != before {
				t.Fatalf("default %s differs from fit %s", before, got)
			}
			if err := driver.Click("Actual Size"); err != nil {
				t.Fatal(err)
			}
			driver.Frame()
			if got := percent(); got != "100%" {
				t.Fatalf("actual size = %s", got)
			}
		})
	}
}

func renderDiagramFixture(t *testing.T, source []byte) mermaid.Result {
	t.Helper()
	var out bytes.Buffer
	if err := mermaid.Helper(bytes.NewReader(source), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result mermaid.Result `json:"result"`
		Error  string         `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error != "" {
		t.Fatal(reply.Error)
	}
	return reply.Result
}

func TestDiagramCacheEvictsLeastRecentlyUsed(t *testing.T) {
	a := &app{mermaidClock: 3, mermaidCache: map[string]*mermaidEntry{
		"old": {lastUsed: 1}, "recent": {lastUsed: 2}, "loading": {lastUsed: 0, loading: true},
	}}
	if a.diagram("old") != a.mermaidCache["old"] {
		t.Fatal("cache hit was not returned")
	}
	a.evictOldestDiagram()
	if a.mermaidCache["recent"] != nil || a.mermaidCache["old"] == nil || a.mermaidCache["loading"] == nil {
		t.Fatalf("incorrect eviction: %#v", a.mermaidCache)
	}
}

func TestDiagramSurfaceLimits(t *testing.T) {
	if !directDiagram(4096, 512, 1) || directDiagram(4097, 1, 1) || directDiagram(1024, 513, 2) {
		t.Fatal("incorrect physical SVG threshold")
	}
	for _, r := range []mermaid.Result{{Width: 8192, Height: 8192}, {Width: 8192, Height: .01}, {Width: .01, Height: 8192}} {
		for _, budget := range []int{0, 4, 2048, detailLimit} {
			w, h := surfaceSize(r, 2, 4096, budget)
			if 4*w*h > budget || max(w, h) > 4096 {
				t.Fatalf("%dx%d exceeds budget %d", w, h, budget)
			}
		}
	}
}

func TestWideDiagramWholeBitmap(t *testing.T) {
	src, err := os.ReadFile("testdata/wide-diagram.mmd")
	if err != nil {
		t.Fatal(err)
	}
	r := renderDiagramFixture(t, src)
	svg, err := ui.ParseSVG([]byte(r.SVG))
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 1)
	a := &app{ctx: context.Background(), update: func(f func()) { updates <- f }}
	item := &mermaidEntry{result: r, image: svg, preview: diagramPreview(r, svg), owner: a}
	a.mermaidCache = map[string]*mermaidEntry{"wide-data": item}
	show := true
	canvas := func() *ui.Tester {
		d := ui.NewTester(func(c *ui.Context) {
			if show {
				ui.Column(c).Fill().Children(func() { diagramCanvas(c, item, 0) })
			}
		}, 1000, 500)
		d.SetScale(2)
		d.Frame()
		return d
	}
	first, second := canvas(), canvas()
	if !first.HasText("22%") || a.detailJob != nil {
		t.Fatal("Fit should not request detail")
	}
	for _, d := range []*ui.Tester{first, second} {
		if err := d.Click("Actual Size"); err != nil {
			t.Fatal(err)
		}
	}
	if a.detailJob != item || item.detailBytes == 0 || len(item.views) != 2 {
		t.Fatal("expected shared reservation")
	}
	select {
	case f := <-updates:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("detail timeout")
	}
	b := item.detail
	if b == nil {
		t.Fatal("no detail")
	}
	w, h := b.Size()
	if w <= 1536 || w*h*4 != item.detailBytes || item.detailBytes > detailLimit {
		t.Fatalf("unexpected detail size %dx%d", w, h)
	}
	if err := first.Click("Fit"); err != nil {
		t.Fatal(err)
	}
	if len(item.views) != 2 || item.detail != b || a.detailJob != nil {
		t.Fatal("changing zoom discarded detail")
	}
	show = false
	first.Frame()
	second.Frame()
	if len(item.views) != 0 {
		t.Fatal("leaked canvas pin")
	}
}

func TestDetailBudgetProtectsPinnedAndEvictsOldest(t *testing.T) {
	r := mermaid.Result{Width: 2000, Height: 100}
	svg := ui.MustParseSVG([]byte(`<svg viewBox="0 0 2000 100"><rect width="2000" height="100"/></svg>`))
	preview := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 100, 5)))
	updates := make(chan func(), 1)
	a := &app{ctx: context.Background(), update: func(f func()) { updates <- f }}
	pin := func() map[*diagramCanvasState]ui.Services {
		return map[*diagramCanvasState]ui.Services{new(diagramCanvasState): {}}
	}
	protected := &mermaidEntry{detail: preview, detailBytes: detailLimit, views: pin()}
	old := &mermaidEntry{detail: preview, detailBytes: detailLimit / 2, lastUsed: 1}
	recent := &mermaidEntry{detail: preview, detailBytes: detailLimit / 2, lastUsed: 2}
	demand := &mermaidEntry{result: r, image: svg, preview: preview, views: pin()}
	a.mermaidCache = map[string]*mermaidEntry{"p": protected, "old": old, "recent": recent, "new": demand}
	// With all pixels pinned, leave the demand on its preview. On unpin,
	// admit it without evicting a still-displayed detail bitmap.
	old.views, recent.views = pin(), pin()
	a.scheduleDiagramDetail()
	if a.detailJob != nil || demand.detailBytes != 0 {
		t.Fatal("started work without budget")
	}
	old.views, recent.views = nil, nil
	a.scheduleDiagramDetail()
	if a.detailJob != demand || old.detail != nil || recent.detail == nil || protected.detail == nil {
		t.Fatal("incorrect admission/eviction")
	}
	select {
	case publish := <-updates:
		publish()
	case <-time.After(5 * time.Second):
		t.Fatal("detail timeout")
	}
}
