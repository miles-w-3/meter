//go:build cgo && (linux || darwin)

package gui

import (
	"context"
	"pi-workspace.local/workspace/internal/microphone"
)

const canCaptureAudio = microphone.Available

func captureMicrophone(ctx context.Context, release <-chan struct{}, samples chan<- []byte, ready chan<- struct{}) error {
	return microphone.Capture(ctx, release, samples, ready)
}
