package theme

import "testing"

func TestMeterCellColor(t *testing.T) {
	tests := []struct {
		cell    int
		percent float64
		want    string
	}{
		{0, 0, MeterEmpty},
		{0, 10, Moss},
		{1, 10, MeterEmpty},
		{1, 11, "#2d3c2f"},
		{1, 15, "#596f51"},
		{1, 19, "#84a172"},
		{1, 20, Moss},
		{7, 75, "#7a6c43"},
		{8, 85, "#7a6c43"},
		{9, 95, "#755041"},
		{9, 100, Madrone},
		{9, 200, Madrone},
		{0, -1, MeterEmpty},
	}
	for _, tt := range tests {
		if got := MeterCellColor(tt.cell, tt.percent); got != tt.want {
			t.Errorf("MeterCellColor(%d, %v) = %s; want %s", tt.cell, tt.percent, got, tt.want)
		}
	}
}
