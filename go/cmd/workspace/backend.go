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
	"syscall"
	"time"

	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
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

func restartBackend(ctx context.Context, api *client.Client, data, release string, redeploy bool) (protocol.ServerInfo, error) {
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

	info, err = api.Info(ctx)
	if err == nil {
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
		return ensureServer(ctx, api, data)
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
	info, err = ensureServer(ctx, api, data)
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
	restored, restartErr := ensureServer(ctx, api, data)
	return restored, errors.Join(fmt.Errorf("redeployed backend did not start; previous executable restored: %w", err), restartErr)
}

func backendDown(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
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
