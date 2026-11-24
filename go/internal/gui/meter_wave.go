package gui

import (
	"github.com/egoist/mygo/ui"
	palette "pi-workspace.local/workspace/internal/theme"
)

// The context meter's cells, which the generation wave paints over.
const (
	meterCellWidth  = 4
	meterCellHeight = 10
	meterGap        = 2
	// With Reduce Motion, a running turn tints the whole meter steadily.
	reducedTint = 0.35
)

// drawMeterWave tints the meter's cells toward Rain where the wave passes, so
// the context fill beneath stays readable.
func drawMeterWave(p *ui.Painter, bounds ui.Rect, activity *palette.Activity, reduceMotion bool) {
	wave, ok := activity.Wave(p.Now())
	if !ok {
		return
	}
	y := bounds.Y + (bounds.H-meterCellHeight)/2
	for i := range palette.MeterCells {
		tint := wave.Tint(i)
		if reduceMotion {
			tint = reducedTint
		}
		if tint < 0.01 {
			continue
		}
		cell := ui.Rect{X: bounds.X + float32(i*(meterCellWidth+meterGap)), Y: y, W: meterCellWidth, H: meterCellHeight}
		p.Fill(cell, rain.Alpha(float32(tint)), 1)
	}
	if !reduceMotion {
		p.AnimationFrameRate(30)
	}
}
