//go:build darwin || linux

package gui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/google/uuid"
)

// Opt in: loads the native terminal library and starts a real local process.
func TestTerminalShutdownRealPTY(t *testing.T) {
	if os.Getenv("PI_TERMINAL_SMOKE") != "1" {
		t.Skip("set PI_TERMINAL_SMOKE=1 for real PTY smoke test")
	}
	file := filepath.Join(t.TempDir(), "pid")
	term, err := terminal.New(terminal.Options{Command: []string{"sh", "-c", "trap '' HUP; echo $$ > \"$1\"; while :; do sleep 1; done", "sh", file}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(file); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("terminal process did not start")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	a := &app{terminals: map[uuid.UUID]terminalSession{uuid.New(): term}}
	a.shutdownTerminals()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d survived shutdown", pid)
}
