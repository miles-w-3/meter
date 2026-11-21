package gui

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"io"
	"strings"
	"time"
)

type dictationRecording struct {
	pane     *pane
	release  chan struct{}
	cancel   context.CancelFunc
	released bool
	live     bool
	preview  string
	base     string
	paneOnly bool
}

const dictationHold = 350 * time.Millisecond

func dictationStatus(recording *dictationRecording) (string, ui.Color) {
	if recording.released {
		return "Transcribing…", ui.Transparent
	}
	if recording.live {
		return "Listening", ui.RGBA(218, 106, 104, .65)
	}
	return "Starting…", ui.RGBA(210, 168, 95, .65)
}

type spaceHold struct {
	pane       *pane
	at         time.Time
	before     string
	start, end int
	active     bool
	restore    bool
	paneOnly   bool
}

func (a *app) dictationInput(c *ui.Context, input ui.Element, p *pane, disabled bool, begin func()) {
	start, end := input.TextSelection()
	input.ObserveInput(func(ev ui.InputEvent) {
		hold := a.spaceHold
		if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeySpace && ev.Mods == 0 && !ev.Repeat && hold == nil && a.dictation == nil && a.mayDictate && canCaptureAudio && !disabled {
			a.spaceHold = &spaceHold{pane: p, at: time.Now(), before: p.draft, start: start, end: end}
		} else if hold != nil && hold.pane == p && ev.Kind == ui.InputKeyUp && ev.Key == ui.KeySpace {
			if hold.active && a.dictation != nil && a.dictation.pane == p && !a.dictation.released {
				a.dictation.released = true
				close(a.dictation.release)
			}
			a.spaceHold = nil
		} else if hold != nil && hold.pane == p && !hold.active && ev.Kind == ui.InputKeyDown && ev.Key != ui.KeySpace {
			a.spaceHold = nil
		}
	})
	hold := a.spaceHold
	if hold == nil || hold.pane != p || hold.paneOnly {
		return
	}
	if !input.Focused() || disabled {
		a.spaceHold = nil
		if hold.active {
			a.cancelDictation()
		}
		return
	}
	if hold.restore {
		input.SetTextSelection(hold.start, hold.end)
		hold.restore = false
	}
	if !hold.active {
		remaining := dictationHold - time.Since(hold.at)
		if remaining > 0 {
			c.After(remaining)
			return
		}
		hold.active = true
		hold.restore = true
		p.draft = hold.before
		begin()
		c.After(time.Millisecond)
	}
}

// Observe at the workspace root so the selected pane remains the dictation
// target even when focus is elsewhere. Text fields keep their own Space input.
func (a *app) observePaneDictation(root ui.Element) {
	root.ObserveInput(func(ev ui.InputEvent) {
		p := a.panes[a.selectedPane]
		hold := a.spaceHold
		if hold != nil && hold.paneOnly && ev.Kind == ui.InputKeyUp && ev.Key == ui.KeySpace {
			if hold.active && a.dictation != nil && a.dictation.pane == hold.pane && !a.dictation.released {
				a.dictation.released = true
				close(a.dictation.release)
			}
			a.spaceHold = nil
			return
		}
		if a.dictationTextFocused || p == nil || p.starting || p.pendingFirst {
			return
		}
		if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeySpace && ev.Mods == 0 && !ev.Repeat && hold == nil && a.dictation == nil && a.mayDictate && canCaptureAudio {
			a.spaceHold = &spaceHold{pane: p, at: time.Now(), before: p.draft, paneOnly: true}
		} else if hold != nil && hold.paneOnly && !hold.active && ev.Kind == ui.InputKeyDown && ev.Key != ui.KeySpace {
			a.spaceHold = nil
		}
	})
}

func (a *app) tickPaneDictation(c *ui.Context, focusWithin bool, begin func(uuid.UUID, *pane)) {
	hold := a.spaceHold
	if hold == nil || !hold.paneOnly {
		return
	}
	p := a.panes[a.selectedPane]
	if !focusWithin || p == nil || hold.pane != p || a.dictationTextFocused || p.starting || p.pendingFirst {
		a.cancelDictation()
		return
	}
	if !hold.active {
		remaining := dictationHold - time.Since(hold.at)
		if remaining > 0 {
			c.After(remaining)
			return
		}
		hold.active = true
		begin(a.selectedPane, p)
		c.After(time.Millisecond)
	}
}

func (a *app) dictationPreview(c *ui.Context, p *pane) {
	current := a.dictation
	if current == nil || current.pane != p {
		return
	}
	a.dictationVisible = true
}

func (a *app) cancelDictation() {
	a.spaceHold = nil
	if a.dictation != nil {
		a.dictation.pane.draft = a.dictation.base
		a.dictation.cancel()
		a.dictation = nil
	}
}

func (a *app) startDictation(id uuid.UUID, p *pane) {
	ctx, cancel := context.WithCancel(a.ctx)
	recording := &dictationRecording{pane: p, base: p.draft, release: make(chan struct{}), cancel: cancel, paneOnly: a.spaceHold != nil && a.spaceHold.paneOnly}
	a.dictation = recording
	captured := make(chan []byte, 100)
	micErr := make(chan error, 1)
	micReady := make(chan struct{}, 1)
	go func() {
		defer close(captured)
		err := captureMicrophone(ctx, recording.release, captured, micReady)
		micErr <- err
		if err != nil && ctx.Err() == nil {
			a.update(func() {
				if a.dictation == recording {
					p.draft, p.notice = recording.base, err.Error()
					a.dictation = nil
					cancel()
				}
			})
		}
	}()
	go func() {
		select {
		case <-micReady:
			a.update(func() {
				if a.dictation == recording && !recording.released {
					recording.live = true
				}
			})
		case <-ctx.Done():
		}
	}()
	go func() {
		defer cancel()
		result, err := a.api.Dictate(ctx, &pcmReader{ctx: ctx, samples: captured}, func(preview string) {
			a.update(func() {
				if a.dictation == recording {
					recording.preview = preview
					p.draft = dictationDraft(recording.base, preview)
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
			a.finishDictation(id, recording, result, err, captureErr)
		})
	}()
}

func (a *app) finishDictation(id uuid.UUID, recording *dictationRecording, result string, err, captureErr error) {
	p := recording.pane
	if a.dictation != recording || a.panes[id] != p {
		return
	}
	a.dictation = nil
	if captureErr != nil {
		p.draft = recording.base
		p.notice = captureErr.Error()
		return
	}
	if err != nil {
		p.draft = recording.base
		p.notice = fmt.Sprintf("Dictation failed: %v", err)
		return
	}
	p.draft = dictationDraft(recording.base, result)
	// Only move focus for pane-initiated dictation, and don't steal it from
	// another text field the user chose while transcription was in flight.
	if recording.paneOnly && strings.TrimSpace(result) != "" && a.selectedPane == id && !a.dictationTextFocused {
		p.focusDictatedDraft = true
	}
}

func dictationDraft(base, spoken string) string {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return base
	}
	if base != "" && !strings.HasSuffix(base, " ") && !strings.HasSuffix(base, "\n") {
		base += " "
	}
	return base + spoken
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
