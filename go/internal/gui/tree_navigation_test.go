package gui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestTreeSelectionAndForkSwitch(t *testing.T) {
	forked := make(chan protocol.ForkInput, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/default":
			var layout protocol.Layout
			if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
				t.Error(err)
				return
			}
			layout.Revision++
			_ = json.NewEncoder(w).Encode(layout)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/conversations/1/fork-messages":
			var input protocol.ForkInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				return
			}
			forked <- input
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(protocol.ForkReceipt{ConversationId: "3", SubmissionId: "4"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	data := t.TempDir()
	endpoint, _ := json.Marshal(map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "token": strings.Repeat("a", 64)})
	if err := os.WriteFile(filepath.Join(data, "endpoint.json"), endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 32)
	project, paneID := uuid.New(), uuid.New()
	conversation := "1"
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{{Id: paneID, ConversationId: &conversation}})
	p := &pane{conversation: conversation, connected: true}
	a := &app{ctx: t.Context(), api: api, name: "default", loaded: true, update: func(fn func()) { updates <- fn }, panes: map[uuid.UUID]*pane{paneID: p}, selectedPane: paneID}
	a.layout.Tabs = []protocol.LayoutTab{tab}
	preview := &treePreview{pane: paneID, collapsed: make(map[string]bool), rows: []treePreviewRow{{id: "entry:2", label: "assistant · answer", entryID: "2", conversation: "1", kind: "assistant"}}}
	a.tree = preview
	step := func() {
		t.Helper()
		select {
		case fn := <-updates:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for update")
		}
	}
	driver := ui.NewTester(func(c *ui.Context) { a.treePane(c, preview) }, 800, 600)
	driver.Frame()
	if err := driver.Click("Select entry:2"); err != nil {
		t.Fatal(err)
	}
	driver.Frame()
	if err := driver.Click("Jump to selection"); err != nil {
		t.Fatal(err)
	}
	step()
	node := a.layout.Tabs[0].Nodes[0]
	if node.TreePosition == nil || node.TreePosition.EntryId != "2" || a.tree != nil {
		t.Fatalf("selection not persisted: %+v", node)
	}
	p.draft = "new branch"
	a.beginFork(tab.Id, node, p)
	step() // persisted request; starts fork
	if a.layout.Tabs[0].Nodes[0].PendingFork == nil {
		t.Fatal("request not persisted before sending")
	}
	var first protocol.ForkInput
	select {
	case first = <-forked:
		if first.At != "2" || first.Content != "new branch" || first.RequestId == "" {
			t.Fatalf("bad input: %+v", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fork not sent")
	}
	// Simulate losing the receipt and restarting from the persisted layout.
	restartUpdates := make(chan func(), 32)
	restartedPane := &pane{conversation: conversation, connected: true}
	restarted := &app{ctx: t.Context(), api: api, name: "default", loaded: true, layout: a.layout, panes: map[uuid.UUID]*pane{paneID: restartedPane}, update: func(fn func()) { restartUpdates <- fn }}
	restarted.syncPanes()
	select {
	case retry := <-forked:
		if retry.RequestId != first.RequestId || retry.Content != first.Content || retry.At != first.At {
			t.Fatalf("restart changed the pending operation: %+v / %+v", first, retry)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not retry the pending fork")
	}
	for range 2 {
		select {
		case fn := <-restartUpdates:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("restart did not complete the branch switch")
		}
	}
	node = restarted.layout.Tabs[0].Nodes[0]
	if node.ConversationId == nil || *node.ConversationId != "3" || node.TreePosition != nil || node.PendingFork != nil {
		t.Fatalf("fork switch not saved: %+v", node)
	}
}

func TestUserSelectionPreservesExistingDraft(t *testing.T) {
	paneID, project := uuid.New(), uuid.New()
	conversation := "1"
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{{Id: paneID, ConversationId: &conversation}})
	p := &pane{conversation: conversation, connected: true, draft: "do not overwrite"}
	a := &app{loaded: true, panes: map[uuid.UUID]*pane{paneID: p}}
	a.layout.Tabs = []protocol.LayoutTab{tab}
	preview := &treePreview{pane: paneID, selected: "entry:3", rows: []treePreviewRow{{id: "entry:3", entryID: "3", entryParent: "2", conversation: "1", kind: "user"}}}
	a.tree = preview
	a.treeNavigate(preview, false)
	if p.draft != "do not overwrite" || preview.notice == "" || a.saving {
		t.Fatal("user selection must not overwrite the existing draft")
	}
}

func TestUserSelectionRestoresMessageAndImagesInMemory(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	attachment := protocol.ImageAttachment{Data: base64.StdEncoding.EncodeToString(encoded.Bytes()), MimeType: "image/png"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/conversations/1/entries/3/message":
			_ = json.NewEncoder(w).Encode(protocol.UserMessage{Content: "editable message", Images: []protocol.ImageAttachment{attachment}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/default":
			var layout protocol.Layout
			if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
				t.Error(err)
				return
			}
			layout.Revision++
			_ = json.NewEncoder(w).Encode(layout)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	data := t.TempDir()
	endpoint, _ := json.Marshal(map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "token": strings.Repeat("a", 64)})
	if err := os.WriteFile(filepath.Join(data, "endpoint.json"), endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 16)
	project, paneID, conversation := uuid.New(), uuid.New(), "1"
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{{Id: paneID, ConversationId: &conversation}})
	p := &pane{conversation: conversation, connected: true}
	a := &app{ctx: t.Context(), api: api, name: "default", loaded: true, update: func(fn func()) { updates <- fn }, panes: map[uuid.UUID]*pane{paneID: p}}
	a.layout.Tabs = []protocol.LayoutTab{tab}
	preview := &treePreview{pane: paneID, selected: "entry:3", rows: []treePreviewRow{{id: "entry:3", entryID: "3", entryParent: "2", conversation: "1", kind: "user"}}}
	a.tree = preview
	a.treeNavigate(preview, false)
	for range 2 {
		select {
		case fn := <-updates:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("restore timed out")
		}
	}
	node := a.layout.Tabs[0].Nodes[0]
	if p.draft != "editable message" || len(p.images) != 1 || node.TreePosition == nil || node.TreePosition.EntryId != "2" || node.TreePosition.RestoreEntryId != nil {
		t.Fatalf("message was not restored in memory at its parent: %q %+v", p.draft, node)
	}
}

func TestForkIntoTabPinsIndependentPaneWithoutChangingOriginal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		var layout protocol.Layout
		if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
			t.Error(err)
			return
		}
		layout.Revision++
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(layout)
	}))
	defer server.Close()
	data := t.TempDir()
	endpoint, _ := json.Marshal(map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "token": strings.Repeat("a", 64)})
	if err := os.WriteFile(filepath.Join(data, "endpoint.json"), endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 16)
	project, paneID, conversation := uuid.New(), uuid.New(), "1"
	tab := protocol.LayoutTab{Id: uuid.New(), ProjectId: &project}
	setPanes(&tab, []protocol.LayoutNode{{Id: paneID, ConversationId: &conversation}})
	p := &pane{conversation: conversation, connected: true, draft: "original draft"}
	a := &app{ctx: t.Context(), api: api, name: "default", loaded: true, update: func(fn func()) { updates <- fn }, panes: map[uuid.UUID]*pane{paneID: p}}
	a.layout.Tabs = []protocol.LayoutTab{tab}
	preview := &treePreview{pane: paneID, selected: "entry:2", rows: []treePreviewRow{{id: "entry:2", entryID: "2", conversation: "1", kind: "assistant", current: true}}}
	a.tree = preview
	a.treeNavigate(preview, true)
	select {
	case fn := <-updates:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("new tab was not saved")
	}
	if len(a.layout.Tabs) != 2 || a.layout.Tabs[0].Nodes[0].TreePosition != nil || p.draft != "original draft" {
		t.Fatal("original pane changed")
	}
	node := a.layout.Tabs[1].Nodes[0]
	if node.Id == paneID || node.ConversationId == nil || *node.ConversationId != conversation || node.TreePosition == nil || node.TreePosition.EntryId != "2" || node.PendingFork != nil {
		t.Fatalf("new tab is not independently pinned: %+v", node)
	}
}

func TestJumpToCurrentResumesAttachedConversation(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var layout protocol.Layout
		if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&layout) != nil {
			http.Error(w, "unexpected", 400)
			return
		}
		node := layout.Tabs[0].Nodes[0]
		if node.TreePosition != nil || node.ResumeEntryId == nil {
			requests <- "invalid"
		} else {
			requests <- *node.ResumeEntryId
		}
		for i := range layout.Tabs {
			for j := range layout.Tabs[i].Nodes {
				layout.Tabs[i].Nodes[j].ResumeEntryId = nil
			}
		}
		layout.Revision++
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(layout)
	}))
	defer server.Close()
	data := t.TempDir()
	endpoint, _ := json.Marshal(map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "token": strings.Repeat("a", 64)})
	if err := os.WriteFile(filepath.Join(data, "endpoint.json"), endpoint, 0600); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(data, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	paneID, conversation := uuid.New(), "1"
	tab := protocol.LayoutTab{Id: uuid.New()}
	setPanes(&tab, []protocol.LayoutNode{{Id: paneID, ConversationId: &conversation, TreePosition: &protocol.TreePosition{ConversationId: "1", EntryId: "2"}}})
	p := &pane{conversation: conversation, connected: true, draft: "preserved"}
	a := &app{ctx: t.Context(), api: api, name: "default", loaded: true, panes: map[uuid.UUID]*pane{paneID: p}, update: func(fn func()) { updates <- fn }}
	a.layout.Tabs = []protocol.LayoutTab{tab}
	preview := &treePreview{pane: paneID, selected: "entry:3", rows: []treePreviewRow{{id: "entry:3", entryID: "3", conversation: "1", kind: "assistant", current: true}}}
	a.tree = preview
	a.treeNavigate(preview, false)
	select {
	case entry := <-requests:
		if entry != "3" {
			t.Fatalf("missing tail check: %s", entry)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume was not saved")
	}
	select {
	case fn := <-updates:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not complete")
	}
	node := a.layout.Tabs[0].Nodes[0]
	if node.TreePosition != nil || node.ResumeEntryId != nil || p.draft != "preserved" || a.tree != nil {
		t.Fatalf("live tail or draft not restored: %+v / %q", node, p.draft)
	}
}
