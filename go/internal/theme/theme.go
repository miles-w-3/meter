package theme

import "fmt"

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

const MeterCells = 10

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
