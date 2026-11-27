package gui

import (
	"strings"
	"testing"
	"time"
)

func TestBackgroundOutputPreview(t *testing.T) {
	if got := lastBackgroundLine("first\nlast\n"); got != "last" {
		t.Fatalf("last line = %q", got)
	}
	if got := lastBackgroundLine(""); got != "No output yet" {
		t.Fatalf("empty output = %q", got)
	}
	if got := lastBackgroundLine("ok\x00\n"); strings.ContainsRune(got, '\x00') {
		t.Fatalf("control character in %q", got)
	}
}

func TestBackgroundDuration(t *testing.T) {
	start := time.Now().UnixMilli() - 12_000
	end := start + 65_000
	if got := backgroundDuration(start, &end); got != "1m 05s" {
		t.Fatalf("duration = %q", got)
	}
	end = start - 1000
	if got := backgroundDuration(start, &end); got != "0m 00s" {
		t.Fatalf("negative duration = %q", got)
	}
}
