package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppClaudeCommand(t *testing.T) {
	home := t.TempDir()
	command := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(home, "Meter.app", "Contents", "MacOS", "meter")
	if got := appClaudeCommand(app, home); got != "" {
		t.Fatalf("missing command: got %q", got)
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := appClaudeCommand(app, home); got != "" {
		t.Fatalf("non-executable command: got %q", got)
	}
	if err := os.Chmod(command, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := appClaudeCommand(app, home); got != command {
		t.Fatalf("got %q, want %q", got, command)
	}
	if got := appClaudeCommand(filepath.Join(home, "bin", "meter"), home); got != "" {
		t.Fatalf("non-app command: got %q", got)
	}
}
