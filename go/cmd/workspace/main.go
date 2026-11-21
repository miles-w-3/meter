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
	"pi-workspace.local/workspace/internal/mermaid"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/tui"
	"regexp"
)

var bundledRevision, bundledDirty string

func main() {
	if len(os.Args) == 2 && os.Args[1] == "__mermaid_render" {
		if err := mermaid.Helper(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: workspace gui [--workspace NAME] | workspace tui [--cwd PATH | --conversation ID] [--workspace NAME] [--no-mouse] [--transparent] | workspace server start|stop|status|logs | workspace dictation")
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
	case "dictation":
		if len(args) != 1 {
			return errors.New("usage: workspace dictation < audio.pcm (16 kHz mono signed 16-bit little-endian)")
		}
		if _, err := ensureServer(ctx, api, data); err != nil {
			return err
		}
		text, err := api.Dictate(ctx, os.Stdin, nil)
		if err == nil {
			fmt.Println(text)
		}
		return err
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
		info, startErr := ensureServer(ctx, api, data)
		release := bundledServer()
		return gui.Run(ctx, api, info, data, *name, cwd, gui.BackendControls{
			Revision: bundledRevision, Dirty: bundledDirty == "true", StartupError: startErr,
			CanRedeploy: release != "" && os.Getenv("PI_WORKSPACE_SERVER") == "",
			Restart: func(ctx context.Context, redeploy bool) (protocol.ServerInfo, error) {
				return restartBackend(ctx, api, data, release, redeploy)
			},
			Download: func(ctx context.Context) error { return downloadModel(ctx, data) },
		})
	case "tui":
		flags := flag.NewFlagSet("tui", flag.ContinueOnError)
		cwd := flags.String("cwd", "", "execution-host project directory")
		id := flags.String("conversation", "", "existing conversation ID")
		name := flags.String("workspace", "default", "workspace layout for the conversation switcher")
		noMouse := flags.Bool("no-mouse", false, "leave mouse input to the terminal for selection")
		transparent := flags.Bool("transparent", false, "use the terminal background")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*id != "" && *cwd != "") {
			return errors.New("usage: workspace tui [--cwd PATH | --conversation ID] [--workspace NAME] [--no-mouse] [--transparent]")
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
		model := tui.New(tuiCtx, api, conversation, info, tui.Options{NoMouse: *noMouse, Transparent: *transparent, Workspace: *name})
		_, err = tea.NewProgram(model, tea.WithContext(tuiCtx)).Run()
		fmt.Println("Conversation", model.ConversationID())
		return err
	}
	return errors.New("expected workspace gui, tui, server, or dictation")
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

func privateData(data string) error {
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
	return nil
}

func launch(data string) error {
	if err := privateData(data); err != nil {
		return err
	}
	entry := os.Getenv("PI_WORKSPACE_SERVER")
	release := bundledServer()
	if entry == "" {
		managed := filepath.Join(data, "meter-server")
		if isExecutable(managed) {
			entry = managed
		} else if release != "" {
			entry = release
		} else {
			binary, err := os.Executable()
			if err != nil {
				return err
			}
			entry = filepath.Join(filepath.Dir(binary), "..", "server", "main.ts")
		}
	}
	var err error
	entry, err = filepath.Abs(entry)
	if err != nil {
		return err
	}
	entryInfo, err := os.Stat(entry)
	if err != nil {
		return fmt.Errorf("locate daemon entrypoint with PI_WORKSPACE_SERVER: %w", err)
	}
	logfile, err := os.OpenFile(filepath.Join(data, "server.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer logfile.Close()
	cmd := exec.Command(entry)
	switch filepath.Ext(entry) {
	case ".ts", ".mts", ".cts", ".js", ".mjs", ".cjs":
		cmd = exec.Command("node", entry)
	default:
		if entryInfo.Mode().Perm()&0o111 == 0 {
			cmd = exec.Command("node", entry)
		}
	}
	cmd.Env = append(os.Environ(), "PI_WORKSPACE_HOME="+data)
	if release != "" && os.Getenv("PI_DICTATION_DIR") == "" && os.Getenv("PI_WORKSPACE_SERVER") == "" {
		cmd.Env = append(cmd.Env, "PI_DICTATION_DIR="+filepath.Dir(release))
	}
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
