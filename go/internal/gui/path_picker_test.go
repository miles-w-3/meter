package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryMatches(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"projects/meter", "projects/other", ".hidden/secret"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	got := directoryMatches(filepath.Join(root, "pmt"))
	if len(got) != 1 || got[0] != filepath.Join(root, "projects/meter") {
		t.Fatalf("fuzzy matches: %v", got)
	}
	got = directoryMatches(root + string(os.PathSeparator))
	for _, path := range got {
		if path == filepath.Join(root, ".hidden") {
			t.Fatal("unexpected hidden directory")
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := expandHome("~/projects"); got != filepath.Join(home, "projects") {
		t.Fatalf("got %q", got)
	}
	if got := expandHome("~someone/projects"); got != "~someone/projects" {
		t.Fatalf("got %q", got)
	}
}
