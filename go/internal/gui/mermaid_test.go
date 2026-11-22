package gui

import (
	"context"
	"strings"
	"testing"

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
	tiles, err := diagramTiles(result)
	if err != nil {
		t.Fatal(err)
	}
	item := &mermaidEntry{result: result, image: image, tiles: tiles}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: item}}
	markdown := "```mermaid\n" + source + "```"
	driver := ui.NewTester(func(c *ui.Context) { app.prose(c, markdown, false) }, 900, 600)
	driver.Frame()
	for _, label := range []string{"Show source", "Save SVG", "Pop out diagram"} {
		if _, ok := driver.Find(label); !ok {
			t.Fatalf("missing control %q: %v", label, driver.Texts())
		}
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
	toggle, _ := driver.Find("Show source")
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

func TestLargeDiagramDrawsAtRetinaZoom(t *testing.T) {
	// The whole diagram would be 7,952 physical pixels wide at 400% on a 2x
	// display, exceeding MyGo's single-SVG drawing limit of 4,096 pixels.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 994 196" width="994" height="196"><rect x="0" y="0" width="994" height="196" fill="#333333"/><rect x="100" y="50" width="300" height="100" fill="#ff0000"/></svg>`
	image, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	result := mermaid.Result{SVG: svg, Width: 994, Height: 196}
	tiles, err := diagramTiles(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiles) == 0 {
		t.Fatal("expected tiled rendering")
	}
	item := &mermaidEntry{result: result, image: image, tiles: tiles}
	driver := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Clip().Background(ui.Hex("#ffffff")).Children(func() { drawDiagramImage(c, item, 4) })
	}, 700, 500)
	driver.SetScale(2)
	driver.Frame()
	frame := driver.Image()
	colored := 0
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			pixel := frame.RGBAAt(x, y)
			if pixel.R < 220 || pixel.G < 220 || pixel.B < 220 {
				colored++
			}
		}
	}
	if colored < 100 {
		t.Fatalf("large diagram did not paint: %d colored pixels", colored)
	}
}

func TestDiagramTilesMatchWholeImage(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 210" width="400" height="210"><rect x="0" y="0" width="400" height="210" fill="#333333"/><circle cx="200" cy="100" r="70" fill="#ff0000"/></svg>`
	img, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	result := mermaid.Result{SVG: svg, Width: 400, Height: 210}
	tiles, err := diagramTiles(result)
	if err != nil {
		t.Fatal(err)
	}
	whole := img.Image(400, 210, nil)
	var differences, total, y int
	for _, row := range tiles {
		x := 0
		for _, piece := range row {
			picture := piece.image.Image(int(piece.width), int(piece.height), nil)
			for py := 0; py < picture.Bounds().Dy(); py++ {
				for px := 0; px < picture.Bounds().Dx(); px++ {
					if picture.RGBAAt(px, py) != whole.RGBAAt(x+px, y+py) {
						differences++
					}
					total++
				}
			}
			x += int(piece.width)
		}
		y += int(row[0].height)
	}
	if total == 0 || differences > total/25 {
		t.Fatalf("tiled image differs at %d/%d pixels", differences, total)
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
	tiles, err := diagramTiles(result)
	if err != nil {
		t.Fatal(err)
	}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: {result: result, image: image, tiles: tiles}}}
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
	tiles, err := diagramTiles(result)
	if err != nil {
		t.Fatal(err)
	}
	app := &app{ctx: context.Background(), mermaidCache: map[string]*mermaidEntry{source: {result: result, image: image, tiles: tiles}}}
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
