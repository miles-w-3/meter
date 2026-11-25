package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const agentPathFile = "agent-path"

func loadAgentPath(data string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(data, agentPathFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read agent PATH: %w", err)
	}
	return string(bytes.TrimSuffix(raw, []byte("\n"))), nil
}

func validateAgentPath(path string) error {
	if strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("agent PATH must be a single line of directories separated by colons")
	}
	return nil
}

func saveAgentPath(data, path string) error {
	if err := validateAgentPath(path); err != nil {
		return err
	}
	if err := privateData(data); err != nil {
		return err
	}
	destination := filepath.Join(data, agentPathFile)
	if path == "" {
		err := os.Remove(destination)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	staged, err := stageFile(data, ".agent-path-*", strings.NewReader(path+"\n"), 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	return os.Rename(staged, destination)
}

func agentPath(ctx context.Context, data string, useLoginPath bool) (string, string, error) {
	return resolveAgentPath(ctx, data, useLoginPath, loginShell)
}

func resolveAgentPath(ctx context.Context, data string, useLoginPath bool, getShell func(context.Context) string) (string, string, error) {
	path, err := loadAgentPath(data)
	if err != nil {
		return "", "", err
	}
	if path != "" {
		return path, "Saved override", validateAgentPath(path)
	}
	if useLoginPath {
		shell := getShell(ctx)
		// Interactive startup picks up toolchains initialized in files like .zshrc.
		if path := shellPath(ctx, shell, "-ilc", 5*time.Second); path != "" {
			return path, "Interactive login shell", nil
		}
		if path := shellPath(ctx, shell, "-lc", 2*time.Second); path != "" {
			return path, "Login shell (interactive probe failed)", nil
		}
		return os.Getenv("PATH"), "Inherited PATH (shell discovery failed or timed out)", nil
	}
	return os.Getenv("PATH"), "Inherited PATH", nil
}

func loginShell(ctx context.Context) string {
	if runtime.GOOS == "darwin" {
		// Finder's environment can omit SHELL or still reflect an old login shell.
		queryCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		record, err := exec.CommandContext(queryCtx, "/usr/bin/dscacheutil",
			"-q", "user", "-a", "uid", strconv.Itoa(os.Getuid())).Output()
		if err == nil {
			if shell := accountShell(string(record)); shell != "" && isExecutable(shell) {
				return shell
			}
		}
	}
	if shell := os.Getenv("SHELL"); filepath.IsAbs(shell) && isExecutable(shell) {
		return shell
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// MyGo's terminal reads SHELL from the GUI environment. Respect an explicit
// choice, even when it differs from the account's configured login shell.
func setDefaultGUIShell(ctx context.Context, probe func(context.Context) string) error {
	if os.Getenv("SHELL") != "" {
		return nil
	}
	if err := os.Setenv("SHELL", probe(ctx)); err != nil {
		return fmt.Errorf("set GUI shell: %w", err)
	}
	return nil
}

func accountShell(record string) string {
	for line := range strings.SplitSeq(record, "\n") {
		if value, ok := strings.CutPrefix(line, "shell:"); ok {
			if shell := strings.TrimSpace(value); filepath.IsAbs(shell) {
				return shell
			}
		}
	}
	return ""
}

func shellPath(ctx context.Context, shell, flags string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Quoted PATH is colon-delimited in fish as well as in POSIX shells.
	cmd := exec.CommandContext(ctx, shell, flags, `printf '\000%s\000' "$PATH"`)
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Env = append(os.Environ(), "SHELL="+shell, "METER_RESOLVING_ENVIRONMENT=1", "DISABLE_AUTO_UPDATE=true", "ZSH_TMUX_AUTOSTARTED=true", "ZSH_TMUX_AUTOSTART=false")
	// Shell startup files may start subprocesses of their own.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Login scripts can write to stdout before or after the command.
	first := bytes.IndexByte(output, 0)
	last := bytes.LastIndexByte(output, 0)
	if first < 0 || first == last {
		return ""
	}
	path := string(output[first+1 : last])
	if validateAgentPath(path) != nil {
		return ""
	}
	return path
}
