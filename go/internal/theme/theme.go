package theme

import (
	"fmt"
	"math"
	"strconv"
)

const (
	Background  = "#0e1511"
	Surface     = "#141d18"
	Panel       = "#1a251f"
	Fog         = "#d3dcd0"
	UserText    = "#aebbb1"
	ToolText    = "#9fbac9"
	UserSurface = "#212c23"
	ToolSurface = "#19252c"
	Muted       = "#8e9f92"
	Faint       = "#647568"
	Moss        = "#8fae7a"
	Chanterelle = "#d2a85f"
	Rain        = "#86a5b5"
	Madrone     = "#c8705a"
)

const (
	MeterCells = 10
	MeterEmpty = "#222f27"
)

func MeterColor(cell int) string {
	switch {
	case cell < 7:
		return Moss
	case cell < 9:
		return Chanterelle
	default:
		return Madrone
	}
}

// MeterCellColor blends the empty and band colors according to how much of
// this cell's 10% interval is used. Completed and future cells stay solid.
func MeterCellColor(cell int, percent float64) string {
	fraction := min(1, max(0, percent*MeterCells/100-float64(cell)))
	if fraction <= 0 {
		return MeterEmpty
	}
	full := MeterColor(cell)
	if fraction >= 1 {
		return full
	}
	emptyRGB, _ := strconv.ParseUint(MeterEmpty[1:], 16, 32)
	fullRGB, _ := strconv.ParseUint(full[1:], 16, 32)
	channel := func(shift uint) int {
		start := float64((emptyRGB >> shift) & 0xff)
		end := float64((fullRGB >> shift) & 0xff)
		return int(math.Round(start + (end-start)*fraction))
	}
	return fmt.Sprintf("#%02x%02x%02x", channel(16), channel(8), channel(0))
}

func Percent(tokens, window int) float64 {
	if window <= 0 {
		return 0
	}
	return min(100, max(0, 100*float64(tokens)/float64(window)))
}

func PercentLabel(percent float64) string {
	if percent > 0 && percent < 1 {
		return "<1%"
	}
	return fmt.Sprintf("%.0f%%", percent)
}

func Stats(duration, tokens *int) string {
	value := ""
	if duration != nil {
		value = fmt.Sprintf("%.1fs", float64(*duration)/1000)
	}
	if tokens != nil && *tokens > 0 {
		if value != "" {
			value += " · "
		}
		if *tokens >= 1000 {
			return value + fmt.Sprintf("%.1fk tok", float64(*tokens)/1000)
		}
		return value + fmt.Sprintf("%d tok", *tokens)
	}
	return value
}
