package theme

import (
	"math"
	"testing"
	"time"
)

func TestSweepPosition(t *testing.T) {
	for _, tc := range []struct {
		at   time.Duration
		want float64
	}{
		{0, 0},
		{SweepPeriod / 4, (MeterCells - 1) / 2.0},
		{SweepPeriod / 2, MeterCells - 1},
		{SweepPeriod, 0},
	} {
		if got := SweepPosition(tc.at); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("SweepPosition(%v) = %v, want %v", tc.at, got, tc.want)
		}
	}
}

func TestActivitySweepsThroughoutRunningTurn(t *testing.T) {
	var a Activity
	now := time.Unix(1_000, 0)
	if _, ok := a.Wave(now); ok {
		t.Fatal("no wave before a turn runs")
	}
	a.Observe(now, true)
	for _, elapsed := range []time.Duration{0, SweepPeriod / 4, SweepPeriod / 2, 3 * SweepPeriod / 4, 4*SweepPeriod + SweepPeriod/4} {
		wave, ok := a.Wave(now.Add(elapsed))
		if !ok || math.Abs(wave.Position-SweepPosition(elapsed)) > 1e-9 {
			t.Fatalf("wave at %v = %+v, %v", elapsed, wave, ok)
		}
		nearest := int(math.Round(wave.Position))
		if wave.Tint(nearest) != 1 {
			t.Fatalf("wave must remain fully visible at %v: %+v", elapsed, wave)
		}
	}
	// Further state updates while running must not reset the animation.
	a.Observe(now.Add(time.Second), true)
	if wave, _ := a.Wave(now.Add(time.Second)); math.Abs(wave.Position-(MeterCells-1)) > 1e-9 {
		t.Fatalf("update reset a running wave: %+v", wave)
	}
	a.Observe(now.Add(2*time.Second), false)
	if _, ok := a.Wave(now.Add(2 * time.Second)); ok {
		t.Fatal("no wave after the turn")
	}
	a.Observe(now.Add(3*time.Second), true)
	if wave, ok := a.Wave(now.Add(3 * time.Second)); !ok || wave.Position != 0 {
		t.Fatalf("new turn should start at the left: %+v %v", wave, ok)
	}
}

func TestWaveTint(t *testing.T) {
	w := Wave{Position: 4.5}
	if w.Tint(4) != 1 || w.Tint(5) != 1 || w.Tint(8) > 0.01 {
		t.Fatalf("tint should remain full between cells and fall off away from the wave: %v %v %v", w.Tint(4), w.Tint(5), w.Tint(8))
	}
}
