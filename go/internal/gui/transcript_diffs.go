package gui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// transcriptDiffs holds the diffs of one tool entry. The stream carries only
// how many an entry has; their file contents are fetched when its row is built.
type transcriptDiffs struct {
	loading, failed bool
	patch           string
	files           []fileDiff
}

// Bound cached diffs, as transcript images are; a visible evicted row reloads.
const transcriptDiffLimit = 64

func (a *app) messageDiffs(p *pane, conversation, entry string) *transcriptDiffs {
	if p.transcriptDiffs == nil {
		p.transcriptDiffs = make(map[string]*transcriptDiffs)
	}
	if state := p.transcriptDiffs[entry]; state != nil {
		return state
	}
	if len(p.transcriptDiffs) >= transcriptDiffLimit {
		for key, old := range p.transcriptDiffs {
			if !old.loading {
				delete(p.transcriptDiffs, key)
				break
			}
		}
	}
	state := &transcriptDiffs{loading: true}
	p.transcriptDiffs[entry] = state
	if a.api == nil || a.update == nil || a.ctx == nil {
		state.loading, state.failed = false, true
		return state
	}
	go func() {
		diffs, err := a.api.EntryDiffs(a.ctx, conversation, entry)
		a.update(func() {
			if p.transcriptDiffs[entry] != state {
				return
			}
			state.loading = false
			if err != nil {
				state.failed = true
				return
			}
			state.patch = valueOrEmpty(diffs.Patch)
			for _, file := range diffs.Files {
				state.files = append(state.files, fileDiff{file.Path, file.Before, file.After, true})
			}
		})
	}()
	return state
}

func diffStatus(c *ui.Context, p *pane, entry string, count int, diffs *transcriptDiffs) {
	switch {
	case diffs.loading:
		text(c, fmt.Sprintf("Loading %d diff(s)…", count), 12, muted).Padding(0, paneGutter, 4, paneTextInset)
	case diffs.failed:
		ui.Row(c).Padding(0, paneGutter, 4, paneTextInset).Children(func() {
			if ui.ButtonBase(c).Label("Retry loading diffs").Children(func() { text(c, "Diff unavailable · Retry", 12, muted) }).Clicked() {
				delete(p.transcriptDiffs, entry)
			}
		})
	}
}
