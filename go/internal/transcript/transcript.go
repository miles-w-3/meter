// Package transcript projects conversation state into items for either frontend.
package transcript

import (
	"strconv"
	"strings"
	"unicode"

	"pi-workspace.local/workspace/internal/client"
	"pi-workspace.local/workspace/internal/protocol"
)

const PartialID = "partial"

type Kind uint8

const (
	Message Kind = iota
	Tool
	Notice
)

type Part struct {
	Index    int
	Thinking bool
	Text     string
}

type LiveTool struct {
	Status    string
	StartedAt *int
}

type Item struct {
	ID, Label, Text, Error, Target, Command string
	Kind                                    Kind
	Parts                                   []Part
	Entry                                   *protocol.Entry
	Live                                    *LiveTool
}

func Build(state *client.State, hideThinking bool, expanded map[string]bool) []Item {
	entries := state.Entries()
	items, completed, calls := BuildEntries(entries, hideThinking)
	return append(items, BuildLive(&state.Current, hideThinking, expanded, completed, calls)...)
}

// BuildEntries projects the stable transcript independently from streaming state.
func BuildEntries(entries []protocol.Entry, hideThinking bool) ([]Item, map[string]bool, map[string]string) {
	items := make([]Item, 0, len(entries))
	notices := cacheNotices(entries)
	completed := make(map[string]bool)
	calls := make(map[string]string)
	for i := range entries {
		entry := &entries[i]
		if notice := notices[entry.Id]; notice != "" {
			items = append(items, Item{ID: "cache:" + entry.Id, Kind: Notice, Text: notice})
		}
		item := Item{ID: entry.Id, Label: strings.TrimPrefix(entry.Kind, "pi."), Text: clean(entry.Text), Error: clean(deref(entry.Error)), Entry: entry}
		if entry.Kind == "meter.background" {
			item.Kind, item.Label = Notice, "background"
		}
		if entry.ToolName != nil {
			completed[entry.Id] = true
			if call := deref(entry.ToolCallId); call != "" {
				calls[call] = entry.Id
			}
			item.Kind, item.Label = Tool, clean(*entry.ToolName)
			item.Target, item.Command = clean(deref(entry.ToolTarget)), deref(entry.ToolCommand)
		} else if entry.Blocks != nil {
			for i, part := range *entry.Blocks {
				item.Parts = appendPart(item.Parts, i, part.Type == protocol.Thinking, part.Text, hideThinking)
			}
			if hasText(item.Parts) {
				item.Text = ""
			}
		}
		if item.Kind == Tool || item.Text != "" || item.Error != "" || len(item.Parts) > 0 || deref(entry.ImageCount) > 0 {
			items = append(items, item)
		}
	}

	return items, completed, calls
}

// BuildLive projects running tools and streaming content. It does not mutate the cached call map.
func BuildLive(view *protocol.ConversationView, hideThinking bool, expanded map[string]bool, completed map[string]bool, committedCalls map[string]string) []Item {
	items := make([]Item, 0, len(view.Tools)+2)
	calls := make(map[string]string, len(committedCalls))
	for k, v := range committedCalls {
		calls[k] = v
	}
	for i, tool := range view.Tools {
		callID, taskID, entryID := deref(tool.CallId), deref(tool.TaskId), deref(tool.EntryId)
		fallback := "live:" + deref(view.AbortTarget) + ":" + strconv.Itoa(i) + ":" + tool.Name
		key := fallback
		if taskID != "" {
			key = "task:" + taskID
		}
		// The call ID is also stored on the committed entry. Keep that key
		// when the daemon adds a task ID to an already-visible tool.
		if callID != "" {
			key = "call:" + callID
			if taskID != "" {
				moveExpansion(expanded, "task:"+taskID, key)
			}
			if entryID == "" && tool.Status == "done" {
				entryID = calls[callID]
			}
			delete(calls, callID)
		}
		moveExpansion(expanded, fallback, key)
		if entryID != "" {
			moveExpansion(expanded, key, entryID)
			key = entryID
			if completed[key] {
				continue
			}
		}
		items = append(items, Item{
			ID: key, Kind: Tool, Label: clean(tool.Name), Text: clean(deref(tool.Output)),
			Target: clean(deref(tool.Target)), Command: deref(tool.Command),
			Live: &LiveTool{Status: clean(tool.Status), StartedAt: tool.StartedAt},
		})
	}
	// A coalesced update can commit a tool and remove its live slot at once.
	for callID, entryID := range calls {
		moveExpansion(expanded, "call:"+callID, entryID)
	}

	partial := Item{ID: PartialID, Label: "assistant", Text: clean(deref(view.Partial))}
	if view.PartialBlocks != nil {
		for i, part := range *view.PartialBlocks {
			partial.Parts = appendPart(partial.Parts, i, part.Type == protocol.Thinking, part.Text, hideThinking)
		}
		if hasText(partial.Parts) {
			partial.Text = ""
		}
	}
	if partial.Text != "" || len(partial.Parts) > 0 {
		items = append(items, partial)
	}
	if failure := clean(deref(view.Failure)); view.Status == protocol.Failed && failure != "" {
		items = append(items, Item{ID: "failure", Kind: Notice, Text: failure})
	}
	return items
}

func appendPart(parts []Part, index int, thinking bool, text string, hideThinking bool) []Part {
	if text = clean(text); text != "" && (!thinking || !hideThinking) {
		return append(parts, Part{Index: index, Thinking: thinking, Text: text})
	}
	return parts
}

func hasText(parts []Part) bool {
	for _, part := range parts {
		if !part.Thinking {
			return true
		}
	}
	return false
}

func moveExpansion(expanded map[string]bool, from, to string) {
	if from != to && expanded[from] {
		expanded[to] = true
		delete(expanded, from)
	}
}

func deref[T any](ptr *T) (zero T) {
	if ptr != nil {
		return *ptr
	}
	return zero
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}
