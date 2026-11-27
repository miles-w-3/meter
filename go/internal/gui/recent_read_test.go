package gui

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestRecentReadHeadSurvivesReopen(t *testing.T) {
	data, server := t.TempDir(), uuid.New()
	open := func(server uuid.UUID, name string) *app {
		a := &app{clientData: data, name: name}
		a.loadReadHeads(server)
		return a
	}
	entries := []protocol.Entry{{Id: "first"}}
	a := open(server, "main")
	a.noteRecentHead("seen", entries)
	a.noteRecentHead("unseen", entries)
	a.markRecentRead("seen")
	a.flushReadHeads()
	b := open(server, "main")
	b.noteRecentHead("seen", entries)
	b.noteRecentHead("unseen", entries)
	if b.unreadConversations["seen"] || !b.unreadConversations["unseen"] {
		t.Fatalf("wrong unread state after reopen: %v", b.unreadConversations)
	}
	b.noteRecentHead("seen", append(entries, protocol.Entry{Id: "second"}))
	if !b.unreadConversations["seen"] {
		t.Fatal("new activity should mark the conversation unread")
	}
	if open(uuid.New(), "main").readHeads["seen"] != "" || open(server, "other").readHeads["seen"] != "" {
		t.Fatal("read receipt leaked across servers or workspaces")
	}
}

func TestRecentReadWritesAreCoalesced(t *testing.T) {
	var scheduled []func()
	var writes []map[string]string
	w := &readHeadsWriter{
		interval: time.Second,
		after: func(delay time.Duration, write func()) {
			if delay != time.Second {
				t.Errorf("debounce = %v", delay)
			}
			scheduled = append(scheduled, write)
		},
		save: func(_ string, heads map[string]string) error {
			writes = append(writes, maps.Clone(heads))
			return nil
		},
	}
	a := &app{readHeadsPath: filepath.Join(t.TempDir(), "read.json"), readHeadsSaver: w}
	for _, head := range []string{"first", "second", "third"} {
		a.noteRecentHead("chat", []protocol.Entry{{Id: head}})
		a.markRecentRead("chat")
	}
	if len(writes) != 0 || len(scheduled) != 1 {
		t.Fatalf("marks must not write from the frame: %d writes, %d scheduled", len(writes), len(scheduled))
	}
	scheduled[0]()
	if len(writes) != 1 || writes[0]["chat"] != "third" {
		t.Fatalf("want one write of the newest head, got %v", writes)
	}
	a.flushReadHeads()
	if len(writes) != 1 {
		t.Fatalf("flush rewrote unchanged state: %v", writes)
	}
	a.noteRecentHead("chat", []protocol.Entry{{Id: "fourth"}})
	a.markRecentRead("chat")
	if len(scheduled) != 2 {
		t.Fatalf("a mark after the write should schedule another: %d", len(scheduled))
	}
	a.flushReadHeads()
	scheduled[1]()
	if len(writes) != 2 || writes[1]["chat"] != "fourth" {
		t.Fatalf("shutdown flush should write pending state once: %v", writes)
	}
}

func TestRecentReadFlushWritesPrivateFile(t *testing.T) {
	data, server := t.TempDir(), uuid.New()
	w := newReadHeadsWriter()
	w.after = func(time.Duration, func()) {} // only the explicit flush writes
	a := &app{clientData: data, name: "main", readHeadsSaver: w}
	a.loadReadHeads(server)
	a.noteRecentHead("chat", []protocol.Entry{{Id: "head"}})
	a.markRecentRead("chat")
	if _, err := os.Stat(a.readHeadsPath); !os.IsNotExist(err) {
		t.Fatalf("read state written before flush: %v", err)
	}
	reopened := &app{clientData: data, name: "main", readHeadsSaver: a.readHeadsSaver}
	reopened.loadReadHeads(server)
	if reopened.readHeads["chat"] != "head" {
		t.Fatalf("queued receipt not visible before it is written: %v", reopened.readHeads)
	}
	a.flushReadHeads()
	info, err := os.Stat(a.readHeadsPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("read state permissions = %v", info.Mode().Perm())
	}
	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
