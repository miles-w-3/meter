package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundledRG(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "meter-server")
	rg := filepath.Join(dir, "rg")
	if bundledRG("") != "" || bundledRG(release) != "" {
		t.Fatal("missing bundled rg should not resolve")
	}
	if err := os.WriteFile(rg, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bundledRG(release) != "" {
		t.Fatal("non-executable bundled rg should not resolve")
	}
	if err := os.Chmod(rg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := bundledRG(release); got != rg {
		t.Fatalf("bundled rg = %q, want %q", got, rg)
	}
}

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
