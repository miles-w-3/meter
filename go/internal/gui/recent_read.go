package gui

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

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
		if err := saveReadHeads(a.readHeadsPath, a.readHeads); err != nil {
			log.Printf("save Recents read state: %v", err)
		}
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
