//go:build cgo && (linux || darwin)

package gui

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/gen2brain/malgo"
)

const canCaptureAudio = true

func captureMicrophone(ctx context.Context, release <-chan struct{}, samples chan<- []byte) error {
	backends := []malgo.Backend{malgo.BackendPulseaudio, malgo.BackendAlsa}
	if runtime.GOOS == "darwin" {
		backends = []malgo.Backend{malgo.BackendCoreaudio}
	}
	audio, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("Open microphone: %w", err)
	}
	defer func() { _ = audio.Uninit(); audio.Free() }()
	config := malgo.DefaultDeviceConfig(malgo.Capture)
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = 1
	config.SampleRate = 16_000
	config.PeriodSizeInMilliseconds = 20
	overflow := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	mic, err := malgo.InitDevice(audio.Context, config, malgo.DeviceCallbacks{
		Data: func(_ []byte, capture []byte, _ uint32) {
			if len(capture) == 0 {
				return
			}
			block := append([]byte(nil), capture...)
			select {
			case samples <- block:
			default:
				signalCapture(overflow)
			}
		},
		Stop: func() { signalCapture(stopped) },
	})
	if err != nil {
		return fmt.Errorf("Open microphone: %w", err)
	}
	defer mic.Uninit()
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := mic.Start(); err != nil {
		return fmt.Errorf("Start microphone: %w", err)
	}
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-overflow:
		return errors.New("Microphone audio could not be processed in real time")
	case <-stopped:
		return errors.New("Microphone stopped during dictation")
	}
}

func signalCapture(signal chan<- struct{}) {
	select {
	case signal <- struct{}{}:
	default:
	}
}
