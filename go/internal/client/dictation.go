package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gorilla/websocket"
)

type dictationEvent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

// Dictate streams signed 16-bit little-endian mono PCM at 16 kHz. EOF flushes the recording.
func (c *Client) Dictate(ctx context.Context, pcm io.Reader, partial func(string)) (string, error) {
	ws, err := c.dialStream(ctx, "/v1/dictation")
	if err != nil {
		return "", err
	}
	defer ws.Close()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ws.SetReadLimit(256 * 1024)
	if err := ws.SetReadDeadline(time.Now().Add(22 * time.Second)); err != nil {
		return "", err
	}
	var event dictationEvent
	if err := ws.ReadJSON(&event); err != nil {
		return "", err
	}
	if event.Type == "error" {
		return "", errors.New(event.Message)
	}
	if event.Type != "ready" {
		return "", errors.New("dictation session did not become ready")
	}
	if err := ws.SetReadDeadline(time.Now().Add(137 * time.Second)); err != nil {
		return "", err
	}

	writes := make(chan error, 1)
	go func() {
		if err := streamAudio(ctx, ws, pcm); err != nil {
			writes <- err
			_ = ws.Close()
		}
	}()
	for {
		event = dictationEvent{}
		if err := ws.ReadJSON(&event); err != nil {
			select {
			case writeErr := <-writes:
				return "", writeErr
			default:
				return "", err
			}
		}
		switch event.Type {
		case "partial":
			if partial != nil {
				partial(event.Text)
			}
		case "final":
			return event.Text, nil
		case "error":
			return "", errors.New(event.Message)
		default:
			return "", fmt.Errorf("unexpected dictation event %q", event.Type)
		}
	}
}

func streamAudio(ctx context.Context, ws *websocket.Conn, pcm io.Reader) error {
	start := time.Now()
	sent := 0
	payload := make([]byte, 640)
	for {
		n, err := io.ReadFull(pcm, payload)
		if n%2 != 0 {
			return errors.New("incomplete PCM sample")
		}
		if n != 0 {
			if wait := time.Until(start.Add(time.Duration(sent) * time.Second / 32_000)); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			sent += n
			if err := ws.SetWriteDeadline(time.Now().Add(4 * time.Second)); err != nil {
				return err
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, payload[:n]); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if err := ws.SetWriteDeadline(time.Now().Add(4 * time.Second)); err != nil {
				return err
			}
			return ws.WriteMessage(websocket.TextMessage, []byte("finish"))
		}
		if err != nil {
			return err
		}
	}
}
