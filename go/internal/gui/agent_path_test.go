package gui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestAgentPathLoadAndSaveStayOffTheFrame(t *testing.T) {
	updates := make(chan func(), 1)
	release := make(chan struct{})
	saved := make(chan string, 1)
	a := &app{update: func(f func()) { updates <- f }, backend: BackendControls{
		LoadAgentPath: func() (string, error) {
			<-release
			return "/opt/bin", nil
		},
		SaveAgentPath: func(path string) error {
			<-release
			saved <- path
			return nil
		},
	}}
	apply := func(what string) {
		t.Helper()
		select {
		case f := <-updates:
			f()
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not finish", what)
		}
	}
	driver := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme); a.agentPathSettings(c) }, 700, 300)
	// A blocked load must not stall the frame.
	driver.Frame()
	driver.Frame()
	if a.agentPathLoaded || !a.agentPathPending {
		t.Fatal("load should be in flight")
	}
	release <- struct{}{}
	apply("load")
	if !a.agentPathLoaded || a.agentPathPending || a.agentPathDraft != "/opt/bin" || a.agentPathSaved != "/opt/bin" {
		t.Fatalf("load result not applied: loaded=%v pending=%v draft=%q saved=%q", a.agentPathLoaded, a.agentPathPending, a.agentPathDraft, a.agentPathSaved)
	}
	driver.Frame()

	a.agentPathDraft = "/usr/bin"
	a.saveAgentPath(a.agentPathDraft)
	driver.Frame()
	if a.agentPathSaved != "/opt/bin" || !a.agentPathPending {
		t.Fatal("save should be in flight")
	}
	release <- struct{}{}
	apply("save")
	if got := <-saved; got != "/usr/bin" {
		t.Fatalf("saved %q", got)
	}
	if a.agentPathPending || a.agentPathSaved != "/usr/bin" || a.agentPathNotice != "" {
		t.Fatalf("save result not applied: pending=%v saved=%q notice=%q", a.agentPathPending, a.agentPathSaved, a.agentPathNotice)
	}
}
