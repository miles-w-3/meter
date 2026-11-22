package gui

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + strings.TrimPrefix(path, "~")
		}
	}
	return path
}

// directoryMatches bounds traversal so typing never blocks the UI or walks an entire disk.
func directoryMatches(query string) []string {
	return findDirectories(context.Background(), query)
}

func findDirectories(ctx context.Context, query string) []string {
	expanded := expandHome(query)
	if !filepath.IsAbs(expanded) {
		return nil
	}
	base, needle := filepath.Dir(expanded), filepath.Base(expanded)
	if strings.HasSuffix(expanded, string(os.PathSeparator)) {
		base, needle = expanded, ""
	}
	needle = strings.ToLower(needle)
	var matches []string
	visited := 0
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		visited++
		if visited > 2500 {
			return fs.SkipAll
		}
		if path == base {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(needle, ".") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(base, path)
		if strings.Count(rel, string(os.PathSeparator)) > 3 {
			return filepath.SkipDir
		}
		if fuzzyMatch(strings.ToLower(rel), needle) {
			matches = append(matches, path)
		}
		return nil
	})
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i]) != len(matches[j]) {
			return len(matches[i]) < len(matches[j])
		}
		return matches[i] < matches[j]
	})
	if len(matches) > 12 {
		matches = matches[:12]
	}
	return matches
}

func fuzzyMatch(value, query string) bool {
	for _, ch := range query {
		i := strings.IndexRune(value, ch)
		if i < 0 {
			return false
		}
		value = value[i+len(string(ch)):]
	}
	return true
}

func (a *app) pathPicker(c *ui.Context, value *string) bool {
	input := ui.TextInput(c, value).Label("Directory").Placeholder("~/projects/…").AutoFocus()
	if a.pathQuery != *value {
		query := *value
		a.pathQuery = query
		a.pathError = ""
		if a.pathCancel != nil {
			a.pathCancel()
		}
		ctx, cancel := context.WithCancel(a.ctx)
		a.pathCancel = cancel
		go func() {
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			matches := findDirectories(ctx, query)
			if ctx.Err() != nil {
				return
			}
			a.update(func() {
				if ctx.Err() == nil && a.pathQuery == query {
					a.pathResults, a.pathResultsQuery = matches, query
				}
			})
		}()
	}
	ui.Scroll(c).Height(220).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			if a.pathResultsQuery != *value {
				return
			}
			for _, path := range a.pathResults {
				row := sidebarRow(c.Key(path), path, false)
				row.Children(func() { text(c, path, 12, ink).SingleLine() })
				if row.Clicked() {
					*value = path + string(os.PathSeparator)
				}
			}
		})
	})
	if a.pathError != "" {
		text(c, a.pathError, 12, warning)
	}
	return input.Submitted()
}
