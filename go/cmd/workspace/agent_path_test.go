package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAgentPathResolution(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(data, "test-shell")
	if err := os.WriteFile(shell, []byte(`#!/bin/sh
[ "$METER_RESOLVING_ENVIRONMENT" = 1 ] || exit 1
printf 'startup banner\n'
case "$1" in
  -ilc)
    if [ "$METER_TEST_INTERACTIVE_FAILURE" = true ]; then exit 1; fi
    PATH='/interactive/bin:/another directory'
    ;;
  -lc) PATH='/login/bin:/another directory' ;;
  *) exit 1 ;;
esac
export PATH
/bin/sh -c "$2"
printf 'logout banner\n'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/inherited/bin:/usr/bin:/bin")
	check := func(useLogin bool, want string) {
		t.Helper()
		if got, _, err := resolveAgentPath(t.Context(), data, useLogin, func(context.Context) string { return os.Getenv("SHELL") }); err != nil || got != want {
			t.Fatalf("agentPath(%t) = %q, %v, want %q", useLogin, got, err, want)
		}
	}

	check(false, "/inherited/bin:/usr/bin:/bin")
	check(true, "/interactive/bin:/another directory")

	const override = "/custom location:/usr/bin:/bin"
	if err := saveAgentPath(data, override); err != nil {
		t.Fatal(err)
	}
	check(false, override)
	check(true, override)
	if err := saveAgentPath(data, "/invalid\n/path"); err == nil {
		t.Fatal("accepted a multiline override")
	}
	check(true, override)
	if err := saveAgentPath(data, ""); err != nil {
		t.Fatal(err)
	}
	check(true, "/interactive/bin:/another directory")

	t.Setenv("METER_TEST_INTERACTIVE_FAILURE", "true")
	check(true, "/login/bin:/another directory")
	t.Setenv("SHELL", filepath.Join(data, "missing-shell"))
	check(true, "/inherited/bin:/usr/bin:/bin")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := shellPath(ctx, shell, "-ilc", time.Second); got != "" {
		t.Fatalf("canceled shell returned PATH %q", got)
	}
}

func TestInteractivePathProbeDoesNotWaitForStartupChildren(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "slow-shell")
	if err := os.WriteFile(shell, []byte(`#!/bin/sh
/bin/sleep 15 &
wait
`), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if path := shellPath(t.Context(), shell, "-ilc", 100*time.Millisecond); path != "" {
		t.Fatalf("timed-out shell returned PATH %q", path)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("timed-out shell kept the probe open for %s", elapsed)
	}
}

func TestAccountShell(t *testing.T) {
	const record = "name: fixture\nuid: 501\ndir: /Users/fixture\nshell: /opt/homebrew/bin/fish\n"
	if got := accountShell(record); got != "/opt/homebrew/bin/fish" {
		t.Fatalf("account shell = %q", got)
	}
}

func TestInteractiveFishPath(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is unavailable")
	}
	config := filepath.Join(t.TempDir(), "fish")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.fish"), []byte(`
if status is-interactive; and status is-login
    echo "startup banner"
    set -gx PATH /fixture/bin "/another directory/bin" /usr/bin /bin
    set -gx METER_FISH_PROBE_ONLY fish-only-variable
end
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(config))
	t.Setenv("METER_FISH_PROBE_ONLY", "parent")
	if got := shellPath(t.Context(), fish, "-ilc", 5*time.Second); got != "/fixture/bin:/another directory/bin:/usr/bin:/bin" {
		t.Fatalf("fish PATH = %q", got)
	}
	if got := os.Getenv("METER_FISH_PROBE_ONLY"); got != "parent" {
		t.Fatalf("probe changed parent environment: %q", got)
	}
}

func TestDefaultGUIShell(t *testing.T) {
	t.Run("preserves explicit shell without probing", func(t *testing.T) {
		t.Setenv("SHELL", "/custom/shell")
		called := false
		err := setDefaultGUIShell(t.Context(), func(context.Context) string { called = true; return "/bin/sh" })
		if err != nil || called || os.Getenv("SHELL") != "/custom/shell" {
			t.Fatalf("explicit shell changed or probed: shell=%q called=%v err=%v", os.Getenv("SHELL"), called, err)
		}
	})
	t.Run("probes when unset", func(t *testing.T) {
		t.Setenv("SHELL", "")
		if err := os.Unsetenv("SHELL"); err != nil {
			t.Fatal(err)
		}
		called := false
		err := setDefaultGUIShell(t.Context(), func(context.Context) string { called = true; return "/bin/sh" })
		if err != nil || !called || os.Getenv("SHELL") != "/bin/sh" {
			t.Fatalf("shell was not probed: shell=%q called=%v err=%v", os.Getenv("SHELL"), called, err)
		}
	})
	t.Run("probes when empty", func(t *testing.T) {
		t.Setenv("SHELL", "")
		called := false
		err := setDefaultGUIShell(t.Context(), func(context.Context) string { called = true; return "/bin/sh" })
		if err != nil || !called || os.Getenv("SHELL") != "/bin/sh" {
			t.Fatalf("shell was not probed: shell=%q called=%v err=%v", os.Getenv("SHELL"), called, err)
		}
	})
}

func TestLoginShellFallsBackToExecutableShell(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Skip the account lookup on macOS.
	t.Setenv("SHELL", "/bin/sh")
	if got := loginShell(ctx); got != "/bin/sh" {
		t.Fatalf("inherited shell = %q, want /bin/sh", got)
	}
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	want := "/bin/sh"
	if runtime.GOOS == "darwin" {
		want = "/bin/zsh"
	}
	if got := loginShell(ctx); got != want {
		t.Fatalf("fallback shell = %q, want %q", got, want)
	}
}
