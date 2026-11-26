package theme

import (
	"testing"
	"time"
)

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

func TestLiveToolStats(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	start := int(now.Add(-10 * time.Second).UnixMilli())
	if got := LiveToolStats("running", &start, now.Add(-time.Millisecond)); got != "" {
		t.Fatalf("before threshold = %q", got)
	}
	if got := LiveToolStats("running", &start, now); got != "10s" {
		t.Fatalf("at threshold = %q", got)
	}
	if got := LiveToolStats("running", &start, now.Add(3*time.Second)); got != "13s" {
		t.Fatalf("later = %q", got)
	}
	if LiveToolStats("pending", &start, now) != "" || LiveToolStats("done", &start, now) != "" || LiveToolStats("running", nil, now) != "" {
		t.Fatal("non-running or unknown start must not show a duration")
	}
	future := int(now.Add(time.Second).UnixMilli())
	if LiveToolStats("running", &future, now) != "" {
		t.Fatal("future timestamp must not show a duration")
	}
}
