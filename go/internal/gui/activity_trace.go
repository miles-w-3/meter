package gui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

const activityPeriod = 5 * time.Second

// activityRanges returns one or two normalized path intervals for a short
// segment moving clockwise around a rounded rectangle. Split at the seam so
// the segment remains continuous as it wraps around.
func activityRanges(phase, perimeter float32) [][2]float32 {
	if perimeter <= 0 {
		return nil
	}
	phase -= float32(math.Floor(float64(phase)))
	length := min(float32(108)/perimeter, float32(0.25))
	end := phase + length
	if end <= 1 {
		return [][2]float32{{phase, end}}
	}
	return [][2]float32{{phase, 1}, {0, end - 1}}
}

func drawActivityTrace(p *ui.Painter, box ui.Rect, reduceMotion bool) {
	// Keep the stroke inside the pane's clip, directly over its existing border.
	const width = float32(1.5)
	x, y := box.X+width/2, box.Y+width/2
	w, h := box.W-width, box.H-width
	if w <= 0 || h <= 0 {
		return
	}
	r := min(float32(12)-width/2, min(w, h)/2)
	perimeter := 2*(w+h-4*r) + 2*float32(math.Pi)*r
	phase := float32(0)
	if !reduceMotion {
		phase = float32(p.Now().UnixNano()%int64(activityPeriod)) / float32(activityPeriod)
		p.AnimationFrameRate(30)
	}
	// Quadratic corners track the pane's rounded border. Trim uses path length,
	// so the highlight moves at a steady speed around edges and corners.
	path := new(ui.Path)
	path.MoveTo(x+r, y).LineTo(x+w-r, y).
		QuadTo(x+w, y, x+w, y+r).LineTo(x+w, y+h-r).
		QuadTo(x+w, y+h, x+w-r, y+h).LineTo(x+r, y+h).
		QuadTo(x, y+h, x, y+h-r).LineTo(x, y+r).
		QuadTo(x, y, x+r, y).Close()
	for _, interval := range activityRanges(phase, perimeter) {
		path.Trim(interval[0], interval[1])
		p.StrokePath(path, width, runningColor)
	}
}
