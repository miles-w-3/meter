package gui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestProjectClosePersistsAcrossDaemonRestart(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")
	projectDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	start := func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, "node", "server/main.ts")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PI_PROVIDER=fixture", "PI_TRANSPORT=tcp", "METER_HOME="+data)
		var logs strings.Builder
		cmd.Stdout, cmd.Stderr = &logs, &logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if _, err := api.Info(ctx); err == nil {
				return cmd
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("daemon did not start: %s", logs.String())
		return nil
	}
	cmd := start()
	defer func() {
		_ = api.Stop(context.Background())
		_ = cmd.Wait()
	}()

	project, err := api.RegisterProject(ctx, projectDir, "Close test project")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(project.Id)
	conversation, err := api.CreateConversation(ctx, protocol.CreateInput{Cwd: projectDir, ProjectId: &id})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := api.Layout(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	selectProject(&layout, id)
	addTab(&layout, id, "Saved tab")
	layout.Tabs[0].Nodes[0].ConversationId = &conversation.Id
	layout.Tabs[0].Nodes[0].Empty = nil
	layout, err = api.SaveLayout(ctx, "default", layout)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := api.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 100)
	a := &app{ctx: ctx, api: api, name: "default", loaded: true, catalog: catalog, layout: layout,
		panes: make(map[uuid.UUID]*pane), update: func(fn func()) { updates <- fn }}
	driver := ui.NewTester(a.view, 1190, 780)
	saved := func() {
		t.Helper()
		for deadline := time.After(10 * time.Second); ; {
			select {
			case fn := <-updates:
				fn()
				driver.Frame()
				if !a.saving {
					if a.notice != "" {
						t.Fatal(a.notice)
					}
					return
				}
			case <-deadline:
				t.Fatal("timed out waiting for layout save")
			}
		}
	}
	if err := driver.RightClick(project.Name); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click("Close project"); err != nil {
		t.Fatal(err)
	}
	saved()
	if driver.HasText(project.Name) || len(a.recentConversations()) != 0 || a.activeTab() != nil {
		t.Fatalf("closed project still visible: %v", driver.Texts())
	}
	if _, err := api.Conversation(ctx, conversation.Id); err != nil {
		t.Fatalf("conversation was lost: %v", err)
	}
	if err := api.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	cmd = start()
	layout, err = api.Layout(ctx, "default")
	if err != nil || !projectClosed(layout, &id) || len(layout.Tabs) != 1 {
		t.Fatalf("closed layout not restored: %+v %v", layout, err)
	}
	a.layout = layout
	driver.Frame()
	if driver.HasText(project.Name) || !driver.HasText("Reopen project") {
		t.Fatalf("closed project visible after restart: %v", driver.Texts())
	}
	if err := driver.Click("Reopen project"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click(project.Name); err != nil {
		t.Fatal(err)
	}
	saved()
	if projectClosed(a.layout, &id) || a.activeTab() == nil || tabName(*a.activeTab()) != "Saved tab" || len(a.recentConversations()) != 1 {
		t.Fatalf("project was not restored: %+v", a.layout)
	}
}
