package gui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"image"
	"image/color"
	"image/png"
	"pi-workspace.local/workspace/internal/client"
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

func TestTranscriptImagesLoadAsync(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/conversations/1/entries/123/images" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"images": []protocol.ImageAttachment{{Data: base64.StdEncoding.EncodeToString(encoded.Bytes()), MimeType: "image/png"}}})
	}))
	defer server.Close()
	addr, _ := url.Parse(server.URL)
	_, port, _ := net.SplitHostPort(addr.Host)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "endpoint.json"), []byte(fmt.Sprintf(`{"port":%s,"token":%q}`, port, strings.Repeat("a", 64))), 0600)
	api, err := client.New(dir, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 1)
	a := &app{api: api, ctx: context.Background(), update: func(f func()) { updates <- f }}
	count := 1
	p := &pane{}
	p.state.Current.Entries = []protocol.Entry{{Id: "123", Kind: "pi.user", ImageCount: &count}}
	driver := ui.NewTester(func(c *ui.Context) { a.transcript(c, p, "1") }, 500, 300)
	driver.Frame()
	select {
	case f := <-updates:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("image load timed out")
	}
	driver.Frame()
	if _, ok := driver.Find("Sent image"); !ok {
		t.Fatalf("image missing after async load: texts=%v state=%+v", driver.Texts(), p.transcriptImages["123"])
	}
}
