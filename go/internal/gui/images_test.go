package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Cyenoch/mygoui/form"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

func TestComposerPasteAndRemoveImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	p := &pane{}
	submitted := 0
	driver := ui.NewTester(func(c *ui.Context) {
		(&app{}).composer(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", SubmitLabel: "Send", OnSubmit: func(string) { submitted++ }})
	}, 500, 260)
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, encoded.Bytes()))))
	driver.Key(ui.Cmd, ui.KeyV)
	driver.Frame()
	if len(p.images) != 1 || !driver.HasText("×") {
		t.Fatalf("pasted image or remove control missing: %v", driver.Texts())
	}
	for range 2 {
		if err := driver.Click("Message"); err != nil {
			t.Fatal(err)
		}
		driver.Key(ui.Cmd, ui.KeyV)
		driver.Frame()
	}
	if len(p.images) != 3 {
		t.Fatalf("expected three attachments, got %d", len(p.images))
	}
	if err := driver.Click("Remove image 2"); err != nil || len(p.images) != 2 {
		t.Fatalf("middle image was not removed: %v", err)
	}
	if err := driver.Click("Send"); err != nil || submitted != 1 {
		t.Fatalf("image-only submission: %v, %d", err, submitted)
	}
	if err := driver.Click("Remove image 1"); err != nil || len(p.images) != 1 {
		t.Fatalf("first image was not removed: %v", err)
	}
	if err := driver.Click("Remove image 1"); err != nil || len(p.images) != 0 {
		t.Fatalf("last image was not removed: %v", err)
	}
	driver.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.Text, []byte("hello")))))
	if err := driver.Click("Message"); err != nil {
		t.Fatal(err)
	}
	driver.Key(ui.Cmd, ui.KeyV)
	if p.draft != "hello" {
		t.Fatalf("text paste changed: %q", p.draft)
	}
}

func TestTranscriptImageOnlyEntryRemainsVisible(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 500, 200))); err != nil {
		t.Fatal(err)
	}
	preview, err := decodeTranscriptThumbnail(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	w, h := ui.ImageSize(preview)
	if w > 192 || h > 192 {
		t.Fatalf("thumbnail too large: %v x %v", w, h)
	}
	count := 1
	p := &pane{transcriptImages: map[string]*transcriptImageState{"123": {images: []ui.ImageSource{preview}}}}
	p.state.Current.Entries = []protocol.Entry{{Id: "123", Kind: "pi.user", ImageCount: &count}}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) { a.transcript(c, p, "1") }, 500, 300)
	driver.Frame()
	if _, ok := driver.Find("Sent image"); !ok {
		t.Fatalf("image-only message not rendered: %v", driver.Texts())
	}
}

func TestComposerStartsAtTopAndButtonStaysAtBottom(t *testing.T) {
	p := &pane{}
	a := &app{}
	driver := ui.NewTester(func(c *ui.Context) {
		a.composer(c, uuid.Nil, p, form.ComposerOptions{Label: "Message", Placeholder: "Message…", SubmitLabel: "Send", OnSubmit: func(string) {}})
	}, 500, 200)
	driver.Frame()
	input, ok := driver.Find("Message")
	if !ok {
		t.Fatal("composer missing")
	}
	button, ok := driver.Find("Send")
	if !ok {
		t.Fatal("send button missing")
	}
	if input.Y > button.Y+1 {
		t.Fatalf("input is not top aligned: input=%+v button=%+v", input, button)
	}
	p.draft = "first line\nsecond line\nthird line"
	driver.Frame()
	input, _ = driver.Find("Message")
	button, _ = driver.Find("Send")
	if input.Y+input.H > button.Y+button.H+1 {
		t.Fatalf("button is not bottom aligned with multiline input: input=%+v button=%+v", input, button)
	}
}
