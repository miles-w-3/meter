//go:build !cgo || (!linux && !darwin)

package microphone

import (
	"context"
	"errors"
)

const Available = false

func Capture(context.Context, <-chan struct{}, chan<- []byte, chan<- struct{}) error {
	return errors.New("microphone capture is unavailable in this build")
}
