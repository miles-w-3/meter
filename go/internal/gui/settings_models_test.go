package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestProviderSessionLimitsShowSavedValues(t *testing.T) {
	// 45 minutes is not one of the usual choices; a saved value still shows.
	sessions := protocol.ProviderSessions{KeepaliveMinutes: 120, ToolWaitMinutes: 45, MaxParked: 3}
	enabled, busy, dateOverride := true, false, false
	sessions.DateOverride = &dateOverride
	p := protocol.ProviderInfo{Id: "pi-claude-cli", Name: "Claude Code", Status: protocol.ProviderInfoStatusConfigured, Enabled: &enabled, Sessions: &sessions, SessionsBusy: &busy, SessionStats: &protocol.ProviderSessionStats{LiveProcesses: 3, ActiveTurns: 1, ParkedIdle: 1, ParkedWaitingForTools: 1}}
	a := &app{modelsInventory: protocol.ModelInventory{Providers: []protocol.ProviderInfo{p}}}
	driver := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.claudePanel(c)
	}, 640, 320)
	driver.Frame()
	for _, want := range []string{"Keep idle sessions", "2 hours", "Keep sessions waiting on a tool", "45 min", "Maximum parked sessions", "Live processes: 3 · Active turns: 1", "Parked: 1 idle · 1 waiting for tool results", "Date reminder override (experimental)", "Off"} {
		if !driver.HasText(want) {
			t.Fatalf("missing %q: %v", want, driver.Texts())
		}
	}
	p.Sessions = nil
	a.modelsInventory.Providers[0] = p
	driver = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		a.claudePanel(c)
	}, 640, 320)
	driver.Frame()
	if driver.HasText("Keep idle sessions") {
		t.Fatal("a provider without session limits shows none")
	}
}

func TestMinutesLabel(t *testing.T) {
	for minutes, want := range map[int]string{15: "15 min", 60: "1 hour", 120: "2 hours", 90: "90 min"} {
		if got := minutesLabel(minutes); got != want {
			t.Errorf("minutesLabel(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestClaudeSessionControlsWarnWhenBusy(t *testing.T) {
	enabled, busy := true, true
	sessions := protocol.ProviderSessions{KeepaliveMinutes: 60, ToolWaitMinutes: 60, MaxParked: 3}
	p := protocol.ProviderInfo{Id: claudeProviderID, Name: "Claude Code", Status: protocol.ProviderInfoStatusConfigured, Enabled: &enabled, Sessions: &sessions, SessionsBusy: &busy}
	a := &app{modelsInventory: protocol.ModelInventory{Providers: []protocol.ProviderInfo{p}}}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.claudePanel(c) }, 640, 320)
	driver.Frame()
	if !driver.HasText("Wait for active conversations, tool calls, and queued messages to finish before changing session settings.") {
		t.Fatalf("missing busy warning: %v", driver.Texts())
	}
	if control, ok := driver.AccessByRole(ui.RolePopUpButton, "Maximum parked sessions"); !ok || !control.Disabled {
		t.Fatalf("busy session control should be disabled: %+v (found=%v)", control, ok)
	}
	enabled = false
	if _, ok := a.claudeProvider(); ok {
		t.Fatal("disabled provider should not have a Claude Code settings option")
	}
}

func TestClaudeSettingsSaveFailureRestoresValueAndShowsError(t *testing.T) {
	enabled, busy := true, false
	old := protocol.ProviderSessions{KeepaliveMinutes: 60, ToolWaitMinutes: 60, MaxParked: 3}
	p := protocol.ProviderInfo{Id: claudeProviderID, Enabled: &enabled, Sessions: &old, SessionsBusy: &busy}
	updates := make(chan func(), 1)
	a := &app{ctx: t.Context(), api: layoutTestClient(t), update: func(f func()) { updates <- f }, modelsInventory: protocol.ModelInventory{Providers: []protocol.ProviderInfo{p}}}
	next := old
	next.MaxParked = 1
	a.setProviderSessions(claudeProviderID, next)
	select {
	case f := <-updates:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("save response timed out")
	}
	if a.modelsError == "" {
		t.Fatal("save error was hidden")
	}
	if a.modelsLoading || *a.modelsInventory.Providers[0].Sessions != old {
		t.Fatal("failed save did not roll back")
	}
}

func TestClaudeSettingsMenuRequiresEnabledProvider(t *testing.T) {
	enabled := true
	a := &app{backendOpen: true, modelsLoaded: true, modelsInventory: protocol.ModelInventory{Providers: []protocol.ProviderInfo{{Id: claudeProviderID, Enabled: &enabled}}}}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.backendPanel(c) }, 1000, 700)
	driver.Frame()
	if !driver.HasText("Claude Code") {
		t.Fatalf("enabled provider missing from settings: %v", driver.Texts())
	}
	enabled = false
	driver.Frame()
	if driver.HasText("Claude Code") {
		t.Fatalf("disabled provider remains in settings: %v", driver.Texts())
	}
}
