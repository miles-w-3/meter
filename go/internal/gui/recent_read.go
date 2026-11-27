package gui

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// readHeadsInterval bounds how often read receipts reach the disk.
const readHeadsInterval = time.Second

// readHeadsWriter persists read receipts away from the frame. The view hands
// it snapshots; one scheduled write per interval stores the newest snapshot
// for each file. It never touches app state.
type readHeadsWriter struct {
	interval time.Duration
	after    func(time.Duration, func())
	save     func(string, map[string]string) error

	mu        sync.Mutex // guards pending and scheduled
	pending   map[string]map[string]string
	scheduled bool
	writing   sync.Mutex // serializes writes so a newer snapshot lands last
}

func newReadHeadsWriter() *readHeadsWriter {
	return &readHeadsWriter{
		interval: readHeadsInterval,
		after:    func(delay time.Duration, write func()) { time.AfterFunc(delay, write) },
		save:     saveReadHeads,
	}
}

// store queues a private copy of heads and schedules a write if none is due.
func (w *readHeadsWriter) store(path string, heads map[string]string) {
	snapshot := maps.Clone(heads)
	w.mu.Lock()
	if w.pending == nil {
		w.pending = make(map[string]map[string]string)
	}
	w.pending[path] = snapshot
	schedule := !w.scheduled
	w.scheduled = true
	w.mu.Unlock()
	if schedule {
		w.after(w.interval, w.flush)
	}
}

// queued returns the snapshot waiting to be written to path, if any.
func (w *readHeadsWriter) queued(path string) (map[string]string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	heads, ok := w.pending[path]
	return maps.Clone(heads), ok
}

// flush writes every queued snapshot now. It is safe to call concurrently
// with a scheduled write.
func (w *readHeadsWriter) flush() {
	w.writing.Lock()
	defer w.writing.Unlock()
	w.mu.Lock()
	pending := w.pending
	w.pending, w.scheduled = nil, false
	w.mu.Unlock()
	for path, heads := range pending {
		if err := w.save(path, heads); err != nil {
			log.Printf("save Recents read state: %v", err)
		}
	}
}

func (a *app) readHeadsQueue() *readHeadsWriter {
	if a.readHeadsSaver == nil {
		a.readHeadsSaver = newReadHeadsWriter()
	}
	return a.readHeadsSaver
}

// flushReadHeads persists queued read receipts synchronously, for shutdown.
func (a *app) flushReadHeads() {
	if a.readHeadsSaver != nil {
		a.readHeadsSaver.flush()
	}
}

func (a *app) loadReadHeads(server uuid.UUID) {
	if a.clientData == "" || server == uuid.Nil {
		return
	}
	// Scope local receipts to this workspace and the stable server ID, not
	// the generation (which changes when the server restarts).
	key := sha256.Sum256([]byte(server.String() + ":" + a.name))
	path := filepath.Join(a.clientData, fmt.Sprintf("desktop-read-%x.json", key[:16]))
	if path == a.readHeadsPath {
		return
	}
	a.readHeadsPath = path
	a.readHeads, a.recentHeads, a.unreadConversations = nil, nil, nil
	if heads, ok := a.readHeadsQueue().queued(path); ok {
		// A write for this file is still queued and newer than the disk.
		a.readHeads = heads
		return
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var heads map[string]string
		if err = json.Unmarshal(data, &heads); err == nil {
			a.readHeads = heads
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("load Recents read state: %v", err)
	}
}

func (a *app) markRecentRead(conversation string) {
	delete(a.unreadConversations, conversation)
	head := a.recentHeads[conversation]
	if head == "" || head == a.readHeads[conversation] {
		return
	}
	if a.readHeads == nil {
		a.readHeads = make(map[string]string)
	}
	a.readHeads[conversation] = head
	if a.readHeadsPath != "" {
		// Called from the frame: queue the write instead of doing disk I/O.
		a.readHeadsQueue().store(a.readHeadsPath, a.readHeads)
	}
}

func saveReadHeads(path string, heads map[string]string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".desktop-read-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := errors.Join(json.NewEncoder(file).Encode(heads), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
