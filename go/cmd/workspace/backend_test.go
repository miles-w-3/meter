package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

type probeAnswer struct {
	generation string
	err        error
}

var backendIsDown = probeAnswer{err: syscall.ECONNREFUSED}

// replay returns a probe that gives answers in order and then repeats the
// last one, and a counter of how often it was called.
func replay(answers ...probeAnswer) (func(context.Context) (string, error), *int) {
	calls := 0
	return func(context.Context) (string, error) {
		answer := answers[min(calls, len(answers)-1)]
		calls++
		return answer.generation, answer.err
	}, &calls
}

func runningChild() *backendChild {
	return &backendChild{done: make(chan struct{})}
}

func exitedChild(err error) *backendChild {
	child := runningChild()
	child.err = err
	close(child.done)
	return child
}

func TestWaitForBackendAcceptsNewGeneration(t *testing.T) {
	probe, calls := replay(backendIsDown, backendIsDown, probeAnswer{generation: "old"}, probeAnswer{generation: "new"})
	generation, err := waitForBackend(context.Background(), probe, runningChild(), "old", time.Second, time.Millisecond)
	if err != nil || generation != "new" {
		t.Fatalf("got %q, %v; want new generation", generation, err)
	}
	if *calls != 4 {
		t.Fatalf("probed %d times, want 4", *calls)
	}
}

func TestWaitForBackendFirstStartAcceptsAnyServer(t *testing.T) {
	probe, _ := replay(backendIsDown, probeAnswer{generation: "first"})
	generation, err := waitForBackend(context.Background(), probe, nil, "", time.Second, time.Millisecond)
	if err != nil || generation != "first" {
		t.Fatalf("got %q, %v", generation, err)
	}
}

func TestWaitForBackendOldGenerationTimesOut(t *testing.T) {
	probe, _ := replay(probeAnswer{generation: "old"})
	generation, err := waitForBackend(context.Background(), probe, runningChild(), "old", 20*time.Millisecond, time.Millisecond)
	if !errors.Is(err, errBackendTimeout) || !strings.Contains(err.Error(), "still answering") {
		t.Fatalf("got %q, %v; want a timeout naming the old backend", generation, err)
	}
}

func TestWaitForBackendChildExitsEarly(t *testing.T) {
	probe, _ := replay(backendIsDown)
	started := time.Now()
	_, err := waitForBackend(context.Background(), probe, exitedChild(errors.New("exit status 3")), "", 10*time.Second, time.Millisecond)
	if !errors.Is(err, errBackendExited) || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("got %v; want the child's exit", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("exit took %v to notice", elapsed)
	}
}

func TestWaitForBackendWakesOnExit(t *testing.T) {
	// With an hour-long tick only the exit channel can end the wait in time.
	child := runningChild()
	probe, _ := replay(backendIsDown)
	go func() { close(child.done) }()
	_, err := waitForBackend(context.Background(), probe, child, "", 10*time.Second, time.Hour)
	if !errors.Is(err, errBackendExited) || !strings.Contains(err.Error(), "exit status 0") {
		t.Fatalf("got %v; want a clean exit to be reported", err)
	}
}

func TestWaitForBackendIgnoresAnswerAfterChildExit(t *testing.T) {
	// A server that answers after our child died is some other process.
	probe, _ := replay(probeAnswer{generation: "other"})
	_, err := waitForBackend(context.Background(), probe, exitedChild(errors.New("exit status 1")), "old", time.Second, time.Millisecond)
	if !errors.Is(err, errBackendExited) {
		t.Fatalf("got %v; want the child's exit", err)
	}
}

func TestWaitForBackendReturnsProbeError(t *testing.T) {
	incompatible := errors.New("incompatible or unready daemon protocol")
	probe, calls := replay(probeAnswer{err: incompatible})
	_, err := waitForBackend(context.Background(), probe, runningChild(), "", time.Second, time.Millisecond)
	if !errors.Is(err, incompatible) || *calls != 1 {
		t.Fatalf("got %v after %d probes", err, *calls)
	}
}

func TestBackendLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	earlier := "earlier backend output\n"
	if err := os.WriteFile(path, []byte(earlier+"Error: database is locked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := &backendChild{logPath: path, logStart: int64(len(earlier))}
	if got := child.logTail(); got != "\nserver.log:\nError: database is locked" {
		t.Fatalf("tail = %q", got)
	}
	child.logStart += int64(len("Error: database is locked\n"))
	if got := child.logTail(); got != "" {
		t.Fatalf("tail without new output = %q", got)
	}
}

func TestLockLaunchSerializes(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	unlock, err := lockLaunch(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lockLaunch(ctx, data); err == nil {
		t.Fatal("second launcher acquired a held lock")
	}
	unlock()
	unlock, err = lockLaunch(context.Background(), data)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	unlock()
}
