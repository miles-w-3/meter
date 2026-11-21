package gui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Cyenoch/mygoui"
	uitheme "github.com/Cyenoch/mygoui/theme"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
)

type dictationRecording struct {
	pane     *pane
	release  chan struct{}
	cancel   context.CancelFunc
	released bool
	preview  string
}

func (a *app) dictationButton(c *ui.Context, id uuid.UUID, p *pane, disabled bool) {
	if !a.mayDictate || !canCaptureAudio {
		return
	}
	current := a.dictation
	onPane := current != nil && current.pane == p
	a.dictationVisible = a.dictationVisible || onPane
	button := mygoui.Button(c, mygoui.ButtonOptions{
		Label: "Hold to dictate", Size: uitheme.ButtonExtraSmall, Variant: mygoui.ButtonSecondary,
		Disabled: (disabled && !onPane) || (current != nil && !onPane),
	})
	held := button.Pressed()
	switch {
	case current == nil && held:
		a.dictationVisible = true
		a.startDictation(id, p)
	case onPane && button.PressCanceled():
		a.cancelDictation()
	case onPane && !held && !current.released:
		current.released = true
		close(current.release)
	}
}

func (a *app) dictationPreview(c *ui.Context, p *pane) {
	current := a.dictation
	if current == nil || current.pane != p {
		return
	}
	message := current.preview
	if message == "" {
		message = "Listening…"
	}
	if current.released {
		message = "Finishing dictation…"
	}
	text(c, clean(message), 12, muted)
}

func (a *app) cancelDictation() {
	if a.dictation != nil {
		a.dictation.cancel()
		a.dictation = nil
	}
}

func (a *app) startDictation(id uuid.UUID, p *pane) {
	ctx, cancel := context.WithCancel(a.ctx)
	recording := &dictationRecording{pane: p, release: make(chan struct{}), cancel: cancel}
	a.dictation = recording
	captured := make(chan []byte, 100)
	micErr := make(chan error, 1)
	go func() {
		defer close(captured)
		micErr <- captureMicrophone(ctx, recording.release, captured)
	}()
	go func() {
		defer cancel()
		result, err := a.api.Dictate(ctx, &pcmReader{ctx: ctx, samples: captured}, func(preview string) {
			a.update(func() {
				if a.dictation == recording {
					recording.preview = preview
				}
			})
		})
		captureErr := func() error {
			select {
			case err := <-micErr:
				return err
			default:
				return nil
			}
		}()
		a.update(func() {
			if a.dictation != recording || a.panes[id] != p {
				return
			}
			a.dictation = nil
			if captureErr != nil {
				p.notice = captureErr.Error()
				return
			}
			if err != nil {
				p.notice = fmt.Sprintf("Dictation failed: %v", err)
				return
			}
			if result = strings.TrimSpace(result); result != "" {
				if p.draft != "" && !strings.HasSuffix(p.draft, " ") && !strings.HasSuffix(p.draft, "\n") {
					p.draft += " "
				}
				p.draft += result
			}
		})
	}()
}

type pcmReader struct {
	ctx     context.Context
	samples <-chan []byte
	pending []byte
}

func (r *pcmReader) Read(dst []byte) (int, error) {
	if len(r.pending) == 0 {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case block, ok := <-r.samples:
			if !ok {
				return 0, io.EOF
			}
			r.pending = block
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
