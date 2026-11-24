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
	"regexp"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/gui"
	"pi-workspace.local/workspace/internal/mermaid"
	"pi-workspace.local/workspace/internal/protocol"
	"pi-workspace.local/workspace/internal/tui"
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
	useLoginPath := false
	if len(args) == 0 {
		// Finder launches the executable without arguments and normally with / as
		// its working directory. Start the GUI in the user's home instead.
		binary, err := os.Executable()
		if err == nil && isAppExecutable(binary) {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			useLoginPath = !term.IsTerminal(os.Stdin.Fd()) && !term.IsTerminal(os.Stdout.Fd())
			args = []string{"gui", "--cwd", home}
		} else {
			return errors.New("usage: meter gui [--workspace NAME] | meter tui [--cwd PATH | --conversation ID] [--workspace NAME] [--no-mouse] [--transparent] | meter server start|stop|status|logs | meter dictation")
		}
	}
	data := os.Getenv("METER_HOME")
	if data == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		data = filepath.Join(config, "meter")
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
			info, err := ensureServer(ctx, api, data, useLoginPath)
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
		if _, err := ensureServer(ctx, api, data, useLoginPath); err != nil {
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
		info, startErr := ensureServer(ctx, api, data, useLoginPath)
		release := bundledServer()
		return gui.Run(ctx, api, info, data, *name, cwd, gui.BackendControls{
			Revision: bundledRevision, Dirty: bundledDirty == "true", StartupError: startErr,
			CanRedeploy: release != "" && os.Getenv("PI_WORKSPACE_SERVER") == "",
			Restart: func(ctx context.Context, redeploy bool) (protocol.ServerInfo, error) {
				return restartBackend(ctx, api, data, release, redeploy, useLoginPath)
			},
			Download:      func(ctx context.Context) error { return downloadModel(ctx, data) },
			LoadAgentPath: func() (string, error) { return loadAgentPath(data) },
			SaveAgentPath: func(path string) error { return saveAgentPath(data, path) },
		})
	case "tui":
		flags := flag.NewFlagSet("tui", flag.ContinueOnError)
		cwd := flags.String("cwd", "", "start a conversation in the project for this directory")
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
		info, err := ensureServer(ctx, api, data, useLoginPath)
		if err != nil {
			return err
		}
		var start tui.Start
		switch {
		case *id != "":
			conversation, err := api.Conversation(ctx, *id)
			if err != nil {
				return err
			}
			start.Conversation = &conversation
		case *cwd != "":
			path, err := filepath.Abs(*cwd)
			if err != nil {
				return err
			}
			start.Cwd, start.Create = path, true
		default:
			start.Cwd, _ = os.Getwd()
		}
		tuiCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		app := tui.NewApp(tuiCtx, api, info, tui.Options{NoMouse: *noMouse, Transparent: *transparent, Workspace: *name}, start)
		_, err = tea.NewProgram(app, tea.WithContext(tuiCtx)).Run()
		if id := app.ConversationID(); id != "" {
			fmt.Println("Conversation", id)
		}
		return err
	}
	return errors.New("expected workspace gui, tui, server, or dictation")
}

func ensureServer(ctx context.Context, api *client.Client, data string, useLoginPath bool) (protocol.ServerInfo, error) {
	info, err := api.Info(ctx)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
		return info, err
	}
	if err := launch(ctx, data, useLoginPath); err != nil {
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

func launch(ctx context.Context, data string, useLoginPath bool) error {
	if err := privateData(data); err != nil {
		return err
	}
	path, pathSource, err := agentPath(ctx, data, useLoginPath)
	if err != nil {
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
	cmd.Env = append(os.Environ(), "METER_HOME="+data, "METER_AGENT_PATH_SOURCE="+pathSource)
	if path != "" {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}
	// Finder does not inherit the user's shell PATH. For the app bundle only,
	// fall back to Claude Code's standard user installation when PATH and
	// the explicit provider command do not resolve it.
	if _, configured := os.LookupEnv("PI_CLAUDE_CLI_COMMAND"); !configured && !pathHasClaude(path) {
		binary, binaryErr := os.Executable()
		home, homeErr := os.UserHomeDir()
		if binaryErr == nil && homeErr == nil {
			if command := appClaudeCommand(binary, home); command != "" {
				cmd.Env = append(cmd.Env, "PI_CLAUDE_CLI_COMMAND="+command)
			}
		}
	}
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

func pathHasClaude(path string) bool {
	for dir := range strings.SplitSeq(path, string(os.PathListSeparator)) {
		if isExecutable(filepath.Join(dir, "claude")) {
			return true
		}
	}
	return false
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
