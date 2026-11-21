package gui

import (
	"math"
	"testing"
)

func TestActivityRanges(t *testing.T) {
	for _, tc := range []struct {
		phase, perimeter float32
		want             [][2]float32
	}{
		{0, 720, [][2]float32{{0, .15}}},
		{.95, 720, [][2]float32{{.95, 1}, {0, .1}}},
		{1.1, 720, [][2]float32{{.1, .25}}},
		{0, 100, [][2]float32{{0, .25}}},
		{0, 0, nil},
	} {
		got := activityRanges(tc.phase, tc.perimeter)
		if len(got) != len(tc.want) {
			t.Fatalf("phase %v: got %v, want %v", tc.phase, got, tc.want)
		}
		for i := range got {
			for j := range got[i] {
				if math.Abs(float64(got[i][j]-tc.want[i][j])) > 1e-6 {
					t.Errorf("phase %v: got %v, want %v", tc.phase, got, tc.want)
				}
			}
		}
	}
}
