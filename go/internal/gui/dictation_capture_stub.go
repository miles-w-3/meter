//go:build !cgo || (!linux && !darwin)

package gui

import (
	"context"
	"errors"
)

const canCaptureAudio = false

func captureMicrophone(context.Context, <-chan struct{}, chan<- []byte) error {
	return errors.New("microphone capture is unavailable in this build")
}
