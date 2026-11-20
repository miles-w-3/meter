package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/gui"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/tui"
	"regexp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: workspace gui [--workspace NAME] | workspace tui [--cwd PATH | --conversation ID] | workspace server start|stop|status|logs")
	}
	data := os.Getenv("PI_WORKSPACE_HOME")
	if data == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		data = filepath.Join(config, "pi-workspace")
	}
	data, err := filepath.Abs(data)
	if err != nil {
		return err
	}
	api, err := client.New(data, os.Getenv("PI_TRANSPORT"))
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch args[0] {
	case "server":
		if len(args) != 2 {
			return errors.New("usage: workspace server start|stop|status|logs")
		}
		switch args[1] {
		case "start":
			info, err := ensureServer(ctx, api, data)
			if err == nil {
				fmt.Println("Workspace ready", info.Id, info.Provider)
			}
			return err
		case "stop":
			return api.Stop(ctx)
		case "status":
			info, err := api.Info(ctx)
			if err == nil {
				fmt.Println("Workspace ready", info.Id, info.Provider)
			}
			return err
		case "logs":
			return showLogs(data)
		}
	case "gui":
		flags := flag.NewFlagSet("gui", flag.ContinueOnError)
		name := flags.String("workspace", "default", "server-side workspace name")
		cwdFlag := flags.String("cwd", "", "initial execution-host project directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(*name) {
			return errors.New("usage: workspace gui [--workspace NAME] [--cwd PATH]")
		}
		cwd, err := filepath.Abs(*cwdFlag)
		if err != nil {
			return err
		}
		info, err := ensureServer(ctx, api, data)
		if err != nil {
			return err
		}
		return gui.Run(ctx, api, info, data, *name, cwd)
	case "tui":
		flags := flag.NewFlagSet("tui", flag.ContinueOnError)
		cwd := flags.String("cwd", "", "execution-host project directory")
		id := flags.String("conversation", "", "existing conversation ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*id != "" && *cwd != "") {
			return errors.New("usage: workspace tui [--cwd PATH | --conversation ID]")
		}
		info, err := ensureServer(ctx, api, data)
		if err != nil {
			return err
		}
		var conversation protocol.Conversation
		if *id != "" {
			conversation, err = api.Conversation(ctx, *id)
		} else {
			path, pathErr := filepath.Abs(*cwd)
			if pathErr != nil {
				return pathErr
			}
			conversation, err = api.Create(ctx, path)
			if err != nil && client.Uncertain(err) {
				return fmt.Errorf("create outcome unknown; check server logs before retrying: %w", err)
			}
		}
		if err != nil {
			return err
		}
		tuiCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		_, err = tea.NewProgram(tui.New(tuiCtx, api, conversation, info)).Run()
		fmt.Println("Conversation", conversation.Id)
		return err
	}
	return errors.New("expected workspace gui, workspace tui, or workspace server start|stop|status|logs")
}

func ensureServer(ctx context.Context, api *client.Client, data string) (protocol.ServerInfo, error) {
	info, err := api.Info(ctx)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
		return info, err
	}
	if err := launch(data); err != nil {
		return info, err
	}
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		info, err = api.Info(deadline)
		if err == nil {
			return info, nil
		}
		if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
			return info, err
		}
		select {
		case <-deadline.Done():
			return info, errors.New("daemon did not become ready; run workspace server logs")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func launch(data string) error {
	if err := os.MkdirAll(data, 0o700); err != nil {
		return err
	}
	dir, err := os.Lstat(data)
	if err != nil {
		return err
	}
	if !dir.IsDir() || dir.Mode().Perm()&0o077 != 0 || dir.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("workspace data directory must be private and owned by the current user")
	}
	entry := os.Getenv("PI_WORKSPACE_SERVER")
	if entry == "" {
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		entry = filepath.Join(filepath.Dir(binary), "..", "server", "main.ts")
	}
	entry, err = filepath.Abs(entry)
	if err != nil {
		return err
	}
	if _, err := os.Stat(entry); err != nil {
		return fmt.Errorf("locate daemon entrypoint with PI_WORKSPACE_SERVER: %w", err)
	}
	logfile, err := os.OpenFile(filepath.Join(data, "server.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer logfile.Close()
	cmd := exec.Command("node", entry)
	cmd.Env = append(os.Environ(), "PI_WORKSPACE_HOME="+data)
	cmd.Stdout, cmd.Stderr = logfile, logfile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap a startup loser while this client is still running.
	go func() { _ = cmd.Wait() }()
	return nil
}

func showLogs(data string) error {
	file, err := os.Open(filepath.Join(data, "server.log"))
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if _, err := file.Seek(max(0, stat.Size()-64*1024), io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(os.Stdout, file)
	return err
}
