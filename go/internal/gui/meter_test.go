package gui

import (
	"testing"
	"time"

	"pi-workspace.local/workspace/internal/protocol"
)

func TestPaneMeter(t *testing.T) {
	p := &pane{connected: true}
	p.state.Current.ContextUsage = &protocol.ContextUsage{Tokens: 25, Window: 100}
	now := time.Now()
	p.updateMeter(now)
	if p.meterLevel != .25 {
		t.Fatalf("idle level = %v, want .25", p.meterLevel)
	}
	p.state.Current.Status = "running"
	p.state.Current.Partial = new(string)
	p.updateMeter(now.Add(time.Second))
	*p.state.Current.Partial = "12345678901234567890123456789012"
	p.updateMeter(now.Add(2 * time.Second))
	if p.meterLevel < .099 || p.meterLevel > .101 {
		t.Fatalf("streaming level = %v, want .1", p.meterLevel)
	}
	p.connected = false
	p.updateMeter(now.Add(3 * time.Second))
	if p.meterLevel != 0 {
		t.Fatalf("disconnected level = %v, want 0", p.meterLevel)
	}
}
