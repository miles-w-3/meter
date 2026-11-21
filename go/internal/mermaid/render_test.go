package mermaid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__mermaid_render" {
		source, _ := io.ReadAll(os.Stdin)
		if string(source) == "test-only-hang" {
			for {
				time.Sleep(time.Second)
			}
		}
		if Helper(bytes.NewReader(source), os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRenderHelper(t *testing.T) {
	r, err := Render(context.Background(), "flowchart LR\n A[Start] --> B[Done]")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SVG, "<svg") || r.Width <= 0 {
		t.Fatal("missing SVG or dimensions")
	}
	if _, err = Render(context.Background(), strings.Repeat("x", MaxSource+1)); err == nil {
		t.Fatal("oversize source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Render(ctx, "flowchart LR\n A --> B"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestHelperRejectsLinks(t *testing.T) {
	var out bytes.Buffer
	if err := Helper(strings.NewReader("flowchart LR\n A --> B\n click A href \"https://example.invalid\""), &out); err != nil {
		t.Fatal(err)
	}
	var result response
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Result == nil {
		t.Fatalf("render failed: %s", result.Error)
	}
	if strings.Contains(result.Result.SVG, "https://example.invalid") {
		t.Fatal("link not removed")
	}
}

func TestValidateRejectsUnsafeSVG(t *testing.T) {
	wrap := func(s string) Result {
		return Result{SVG: `<svg xmlns="http://www.w3.org/2000/svg">` + s + `</svg>`, Width: 10, Height: 10}
	}
	for _, s := range []string{`<script/>`, `<image href="https://example.invalid"/>`, `<a href="https://example.invalid"/>`, `<rect onclick="evil()"/>`, `<rect fill="url(https://example.invalid/a)"/>`, `<foreignObject/>`, `<style>@import 'https://example.invalid';</style>`} {
		if validate(wrap(s)) == nil {
			t.Errorf("accepted %s", s)
		}
	}
	if err := validate(wrap(`<rect fill="url(#gradient)"/>`)); err != nil {
		t.Fatal(err)
	}
}

func TestHelperCatalogExamples(t *testing.T) {
	data, err := os.ReadFile("../../../server/tools/mermaid-catalog-data.ts")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "["), strings.LastIndex(text, "]")
	if start < 0 || end < start {
		t.Fatal("bad catalog data")
	}
	var entries []struct{ Kind, Name, Example string }
	if err := json.Unmarshal([]byte(text[start:end+1]), &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Kind == "icon" {
			continue
		}
		t.Run(e.Name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Helper(strings.NewReader(e.Example), &out); err != nil {
				t.Fatal(err)
			}
			var reply response
			if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Result == nil {
				t.Fatalf("rejected: %s", reply.Error)
			}
		})
	}
}

func TestRenderTerminatesStuckHelper(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Render(ctx, "test-only-hang")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("helper did not exit promptly")
	}
}
