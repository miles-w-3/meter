package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func isAppExecutable(binary string) bool {
	dir := filepath.Dir(binary)
	contents := filepath.Dir(dir)
	return filepath.Base(dir) == "MacOS" && filepath.Base(contents) == "Contents" && filepath.Ext(filepath.Dir(contents)) == ".app"
}

func appClaudeCommand(binary, home string) string {
	if !isAppExecutable(binary) {
		return ""
	}
	command := filepath.Join(home, ".local", "bin", "claude")
	if isExecutable(command) {
		return command
	}
	return ""
}

// bundledRG resolves relative to the app's server, not a redeployed managed
// server executable, which may live in the user's data directory.
func bundledRG(release string) string {
	if release == "" {
		return ""
	}
	rg := filepath.Join(filepath.Dir(release), "rg")
	if isExecutable(rg) {
		return rg
	}
	return ""
}

func bundledServer() string {
	binary, err := os.Executable()
	if err != nil {
		return ""
	}
	entry := filepath.Join(filepath.Dir(binary), "meter-server")
	if isExecutable(entry) {
		return entry
	}
	return ""
}

func restartBackend(ctx context.Context, api *client.Client, data, release string, redeploy, useLoginPath bool) (protocol.ServerInfo, error) {
	var info protocol.ServerInfo
	if err := privateData(data); err != nil {
		return info, err
	}
	file, err := os.OpenFile(filepath.Join(data, "backend-operation.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return info, err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return info, fmt.Errorf("backend operation lock: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var staged string
	if redeploy {
		if release == "" || os.Getenv("PI_WORKSPACE_SERVER") != "" {
			return info, errors.New("redeploy requires an adjacent bundled backend")
		}
		source, err := os.Open(release)
		if err != nil {
			return info, err
		}
		staged, err = stageFile(data, "backend-*", source, 0o700)
		source.Close()
		if err != nil {
			return info, err
		}
		defer os.Remove(staged)
	}

	// Hold the launch lock until the replacement is verified, so another
	// client's ensureServer cannot start a backend in the gap after Stop.
	unlock, err := lockLaunch(ctx, data)
	if err != nil {
		return info, err
	}
	defer unlock()

	// previous identifies the server being replaced. The restart succeeds
	// only once a server with a different generation answers, so the old
	// process (or a stale answer) cannot pass for the new one.
	previous := ""
	info, err = api.Info(ctx)
	if err == nil {
		previous = generationOf(info)
		if err := api.Stop(ctx); err != nil {
			return info, err
		}
		for {
			current, err := api.Info(ctx)
			if backendDown(err) {
				break
			}
			if err != nil {
				return current, err
			}
			if info.Generation != nil && current.Generation != nil && *info.Generation != *current.Generation {
				return current, errors.New("another client restarted the backend")
			}
			select {
			case <-ctx.Done():
				return info, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	} else if !backendDown(err) {
		return info, err
	}

	if !redeploy {
		_, info, err = startServer(ctx, api, data, useLoginPath, previous)
		return info, err
	}
	installed := filepath.Join(data, "meter-server")
	backup := staged + ".previous"
	if err := os.Link(installed, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.Remove(backup)
		}
	}()
	if err := os.Rename(staged, installed); err != nil {
		return info, err
	}
	child, info, err := startServer(ctx, api, data, useLoginPath, previous)
	if err == nil {
		return info, nil
	}
	restoreErr := os.Rename(backup, installed)
	if errors.Is(restoreErr, os.ErrNotExist) {
		restoreErr = os.Remove(installed)
	}
	if restoreErr != nil {
		// Leave the previous binary available for manual recovery.
		keepBackup = true
		return info, errors.Join(err, fmt.Errorf("restore previous backend from %s: %w", backup, restoreErr))
	}
	err = fmt.Errorf("redeployed backend did not start; previous executable restored: %w", err)
	// A slow new backend may still be starting after missing the deadline.
	// Stop it first: otherwise it could answer the rollback's probe and pass
	// for the restored backend, and it would keep the writer lock.
	if stopErr := child.stop(backendStopTimeout); stopErr != nil {
		return info, errors.Join(err, stopErr)
	}
	// The rollback uses the same verified wait. It gets a context without the
	// restart deadline, which a slow failure may have used up, so the user is
	// not left without a backend; waitForBackend bounds the wait itself.
	_, restored, restartErr := startServer(context.WithoutCancel(ctx), api, data, useLoginPath, previous)
	return restored, errors.Join(err, restartErr)
}

func backendDown(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// Backend startup supervision.
//
// The backend is a daemon shared by every client, so it is started detached
// and must outlive the client that launched it. While that client waits for
// it to come up, though, it keeps the child's handle: a backend that crashes
// during startup is reported at once with its exit status instead of after a
// blind timeout, and a restart succeeds only when a new process answers.

// Holders of the launch lock bound their own work (see restartBackend), so
// backendLockTimeout only stops a client from waiting forever on a wedged one.
const (
	backendStartTimeout = 10 * time.Second
	backendPollInterval = 100 * time.Millisecond
	backendStopTimeout  = 5 * time.Second
	backendLockTimeout  = time.Minute
)

var (
	errBackendExited  = errors.New("backend exited during startup")
	errBackendTimeout = errors.New("daemon did not become ready")
)

// backendChild is a backend process started by this client. done is closed
// once the process has exited and been reaped; err then holds cmd.Wait's
// result. logPath and logStart locate the output this child wrote.
type backendChild struct {
	process  *os.Process
	done     chan struct{}
	err      error
	logPath  string
	logStart int64
}

// exitStatus reports whether the child has exited and, if so, how. A nil
// child (one this client did not start) never reports an exit.
func (c *backendChild) exitStatus() (string, bool) {
	if c == nil {
		return "", false
	}
	select {
	case <-c.done:
		if c.err == nil {
			return "exit status 0", true
		}
		return c.err.Error(), true
	default:
		return "", false
	}
}

func (c *backendChild) exitError() error {
	status, _ := c.exitStatus()
	return fmt.Errorf("%w (%s)", errBackendExited, status)
}

// stop terminates a child that failed to become ready and waits until it has
// been reaped, so it cannot answer the readiness probe of a later backend. It
// is only used on failure: a backend that started is never killed here.
func (c *backendChild) stop(timeout time.Duration) error {
	if _, gone := c.exitStatus(); c == nil || gone {
		return nil
	}
	// SIGTERM lets the backend shut down its own helpers; escalate to the
	// whole process group (the child leads its own session) if it hangs.
	_ = c.process.Signal(syscall.SIGTERM)
	select {
	case <-c.done:
		return nil
	case <-time.After(timeout):
	}
	_ = syscall.Kill(-c.process.Pid, syscall.SIGKILL)
	select {
	case <-c.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("failed backend process %d did not exit", c.process.Pid)
	}
}

// logTail returns the last lines this child wrote to server.log, formatted
// to append to an error, or "" when there are none.
func (c *backendChild) logTail() string {
	if c == nil || c.logPath == "" {
		return ""
	}
	file, err := os.Open(c.logPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return ""
	}
	start := max(c.logStart, stat.Size()-2048)
	if start >= stat.Size() {
		return ""
	}
	buffer := make([]byte, stat.Size()-start)
	n, _ := file.ReadAt(buffer, start)
	lines := strings.Split(strings.TrimSpace(string(buffer[:n])), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	tail := strings.TrimSpace(strings.Join(lines, "\n"))
	if tail == "" {
		return ""
	}
	return "\nserver.log:\n" + tail
}

func generationOf(info protocol.ServerInfo) string {
	if info.Generation == nil {
		return ""
	}
	return info.Generation.String()
}

// waitForBackend waits until probe reports a ready server, the child exits,
// or timeout passes, whichever comes first. probe returns the answering
// server's generation. When previous is set, an answer with that generation
// is the server being replaced and does not count. While child (if known)
// has exited, no answer counts either: it came from some other process.
// Dependencies are injected so the logic can be tested without processes.
func waitForBackend(ctx context.Context, probe func(context.Context) (string, error), child *backendChild, previous string, timeout, tick time.Duration) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var exited <-chan struct{} // nil, and so never ready, without a child
	if child != nil {
		exited = child.done
	}
	for {
		generation, err := probe(deadline)
		// Check the child after probing so an answer never outruns its exit.
		if _, gone := child.exitStatus(); gone {
			return "", child.exitError()
		}
		if err == nil && (previous == "" || generation != previous) {
			return generation, nil
		}
		if deadline.Err() != nil {
			return "", timeoutError(previous, err == nil)
		}
		if err != nil && !backendDown(err) {
			return "", err
		}
		stale := err == nil
		select {
		case <-exited:
			return "", child.exitError()
		case <-deadline.Done():
			return "", timeoutError(previous, stale)
		case <-ticker.C:
		}
	}
}

func timeoutError(previous string, stale bool) error {
	if stale {
		return fmt.Errorf("%w: the previous backend (generation %s) is still answering", errBackendTimeout, previous)
	}
	return errBackendTimeout
}

// startServer launches a backend and waits until it is verifiably serving.
// previous is the generation of the server being replaced, or "" if none. The
// child is returned even on failure so that a caller can stop it.
func startServer(ctx context.Context, api *client.Client, data string, useLoginPath bool, previous string) (*backendChild, protocol.ServerInfo, error) {
	child, err := launch(ctx, data, useLoginPath)
	if err != nil {
		return nil, protocol.ServerInfo{}, err
	}
	var info protocol.ServerInfo
	probe := func(ctx context.Context) (string, error) {
		current, err := api.Info(ctx)
		if err != nil {
			return "", err
		}
		info = current
		return generationOf(current), nil
	}
	if _, err := waitForBackend(ctx, probe, child, previous, backendStartTimeout, backendPollInterval); err != nil {
		if errors.Is(err, errBackendExited) || errors.Is(err, errBackendTimeout) {
			err = fmt.Errorf("%w; run workspace server logs%s", err, child.logTail())
		}
		return child, protocol.ServerInfo{}, err
	}
	return child, info, nil
}

// lockLaunch serializes backend launches between clients, and with restarts,
// using a flock in the data directory. It waits while another client holds
// it. The descriptor is close-on-exec, so the backend never inherits the lock.
func lockLaunch(ctx context.Context, data string) (func(), error) {
	if err := privateData(data); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(data, "backend-launch.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, backendLockTimeout)
	defer cancel()
	ticker := time.NewTicker(backendPollInterval)
	defer ticker.Stop()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("backend launch lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("another client is still starting the backend: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func stageFile(directory, pattern string, source io.Reader, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err := io.Copy(file, source); err != nil {
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	return name, file.Close()
}

const (
	modelName = "parakeet-eou-q8.gguf"
	modelURL  = "https://huggingface.co/mudler/parakeet-cpp-gguf/resolve/07a08a3a405d20c5434b9a1442f6e6cf571f57ad/realtime_eou_120m-v1-q8_0.gguf?download=true"
	modelSHA  = "62616b914d6f5a683a5dea672df055b57de5c49dddf871b8b44b9c814dc3d896"
	modelSize = 176001472
)

func downloadModel(ctx context.Context, data string) error {
	if err := privateData(data); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return err
	}
	httpClient := http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("unexpected model download redirect")
		}
		return nil
	}}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Hugging Face returned HTTP %d", response.StatusCode)
	}
	digest := sha256.New()
	staged, err := stageFile(data, "model-*", io.TeeReader(io.LimitReader(response.Body, modelSize+1), digest), 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if hex.EncodeToString(digest.Sum(nil)) != modelSHA {
		return errors.New("downloaded model did not match the expected SHA-256")
	}
	return os.Rename(staged, filepath.Join(data, modelName))
}
