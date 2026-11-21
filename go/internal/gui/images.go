package gui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"pi-workspace.local/workspace/internal/protocol"
)

type draftImage struct {
	attachment protocol.ImageAttachment
	preview    ui.ImageSource
}

func (p *pane) pasteImage(services ui.Services, text string) bool {
	items := services.ReadClipboardData(transfer.PNG).Items()
	if len(items) == 0 {
		return false
	}
	if p.queuePending != "" {
		p.notice = "Wait for the queued message to return."
		return true
	}
	if len(p.images) >= 4 {
		p.notice = "A message can contain up to four images."
		return true
	}
	data, err := items[0].Read(transfer.PNG)
	if err != nil || len(data) == 0 || len(data) > 2*1024*1024 {
		p.notice = "The image must be a PNG under 2 MiB."
		return true
	}
	preview, err := ui.DecodeImage(data)
	if err != nil {
		p.notice = "Unable to read the pasted image."
		return true
	}
	p.images = append(p.images, draftImage{
		attachment: protocol.ImageAttachment{Data: base64.StdEncoding.EncodeToString(data), MimeType: "image/png"},
		preview:    preview,
	})
	p.notice = ""
	return true
}

func imagePreviews(c *ui.Context, p *pane) {
	if len(p.images) == 0 {
		return
	}
	ui.Row(c).Padding(8, 12, 0).Gap(8).Children(func() {
		for i, image := range p.images {
			ui.Box(c.Key(i)).Size(72, 72).Children(func() {
				ui.Image(c, image.preview).Size(72, 72)
				if ui.ButtonBase(c).Absolute().Right(2).Top(2).Size(20, 20).Radius(10).
					Background(background).Center().Label(fmt.Sprintf("Remove image %d", i+1)).
					Tooltip(fmt.Sprintf("Remove image %d", i+1)).Children(func() {
					text(c, "×", 16, ink)
				}).Clicked() {
					p.images = append(p.images[:i], p.images[i+1:]...)
				}
			})
		}
	})
}

func (p *pane) restoreSubmission() {
	input := p.submission.RestoreInput()
	p.restoreDraft(input.Content, input.Images)
}

func (p *pane) restoreDraft(content string, images *[]protocol.ImageAttachment) {
	input := protocol.SubmitInput{Content: content, Images: images}
	if input.Content != "" {
		if p.draft != "" {
			p.draft = input.Content + "\n" + p.draft
		} else {
			p.draft = input.Content
		}
	}
	if input.Images != nil {
		restored := make([]draftImage, 0, len(*input.Images))
		for _, image := range *input.Images {
			data, err := base64.StdEncoding.DecodeString(image.Data)
			if err != nil {
				continue
			}
			preview, err := ui.DecodeImage(data)
			if err == nil {
				restored = append(restored, draftImage{attachment: image, preview: preview})
			}
		}
		p.images = append(restored, p.images...)
	}
}

type transcriptImageState struct {
	loading bool
	failed  bool
	images  []ui.ImageSource
}

// Load only image entries that the virtualized transcript actually builds.
func (a *app) messageImages(c *ui.Context, p *pane, conversation, entry string, count int) {
	if p.transcriptImages == nil {
		p.transcriptImages = make(map[string]*transcriptImageState)
	}
	state := p.transcriptImages[entry]
	if state == nil {
		// Bound cached decoded images; a visible evicted row reloads when revisited.
		if len(p.transcriptImages) >= 64 {
			for key, old := range p.transcriptImages {
				if !old.loading {
					delete(p.transcriptImages, key)
					break
				}
			}
		}
		state = &transcriptImageState{loading: true}
		p.transcriptImages[entry] = state
		if a.api != nil && a.update != nil && a.ctx != nil {
			go func(state *transcriptImageState) {
				images, err := a.api.EntryImages(a.ctx, conversation, entry)
				var previews []ui.ImageSource
				if err == nil {
					for _, image := range images {
						data, decodeErr := base64.StdEncoding.DecodeString(image.Data)
						if decodeErr == nil {
							if preview, decodeErr := decodeTranscriptThumbnail(data); decodeErr == nil {
								previews = append(previews, preview)
							}
						}
					}
				}
				a.update(func() {
					if p.transcriptImages[entry] != state {
						return
					}
					state.loading = false
					if err != nil {
						state.failed = true
						return
					}
					state.images = previews
					state.failed = len(state.images) == 0
				})
			}(state)
		} else {
			state.loading = false
			state.failed = true
		}
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).Wrap().Padding(0, 0, 6).Children(func() {
		switch {
		case state.loading:
			text(c, fmt.Sprintf("Loading %d image(s)…", count), 12, muted)
		case state.failed:
			if ui.ButtonBase(c).Label("Retry loading images").Children(func() { text(c, "Image unavailable · Retry", 12, muted) }).Clicked() {
				delete(p.transcriptImages, entry)
			}
		default:
			for i, image := range state.images {
				ui.Image(c.Key(i), image).Size(96, 96).Fit(ui.Contain).Shrink(0).Label("Sent image").Tooltip("Sent image")
			}
		}
	})
}

// Keep decoded previews small and reject images with extreme dimensions before decoding.
func decodeTranscriptThumbnail(data []byte) (ui.ImageSource, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return nil, fmt.Errorf("image dimensions exceed preview limit")
	}
	return ui.DecodeBitmapScaled(data, 192)
}
