package theme

import (
	"math"
	"time"
)

// SweepPeriod is one round trip of the activity wave across the meter.
const SweepPeriod = 2 * time.Second

// Activity tracks the start of a running turn, independent of whether it is
// thinking, using tools, or streaming text.
type Activity struct {
	running bool
	since   time.Time
}

// Observe starts or stops the wave when the turn's running state changes.
func (a *Activity) Observe(now time.Time, running bool) {
	if !running {
		*a = Activity{}
	} else if !a.running {
		*a = Activity{running: true, since: now}
	}
}

// Wave is the meter's activity at an instant.
type Wave struct {
	// Position is the wave's peak, in cells from the meter's left end.
	Position float64
}

// Wave reports false when no turn is running.
func (a *Activity) Wave(now time.Time) (Wave, bool) {
	if !a.running {
		return Wave{}, false
	}
	return Wave{Position: SweepPosition(now.Sub(a.since))}, true
}

// Tint is how far toward Rain the wave turns a cell.
func (w Wave) Tint(cell int) float64 {
	// Keep the nearest cell fully tinted even between cell centers.
	d := max(0, math.Abs(float64(cell)-w.Position)-0.5) / 1.1
	return math.Exp(-d * d)
}

// SweepPosition eases back and forth over the meter like a pendulum, slowing
// at each end, starting from the left.
func SweepPosition(t time.Duration) float64 {
	phase := math.Mod(t.Seconds()/SweepPeriod.Seconds(), 1)
	return (1 - math.Cos(2*math.Pi*phase)) / 2 * (MeterCells - 1)
}
