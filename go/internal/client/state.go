package client

import "pi-workspace.local/workspace/internal/protocol"

// State is an in-memory transcript with a separately pageable older prefix.
type State struct {
	Current protocol.ConversationView
	Before  *string
	older   []protocol.Entry
}

func (s *State) Apply(event Event) {
	if event.View == nil {
		return
	}
	next := *event.View
	nextIDs := make(map[string]bool, len(next.Entries))
	for _, entry := range next.Entries {
		nextIDs[entry.Id] = true
	}
	previous := s.Entries()
	firstOverlap := -1
	for i, entry := range previous {
		if nextIDs[entry.Id] {
			firstOverlap = i
			break
		}
	}
	switch {
	case firstOverlap >= 0:
		s.older = previous[:firstOverlap]
	case len(previous) == 0 || event.Reset:
		s.older, s.Before = nil, next.Before
	default:
		// An update can move the bounded live window past the entire old view.
		// The new cursor can fill the missing range without inventing order.
		s.older, s.Before = nil, next.Before
	}
	s.Current = next
}

func (s *State) Prepend(cursor *string, page protocol.HistoryPage) {
	if cursor == nil || s.Before == nil || *s.Before != *cursor {
		return
	}
	existing := make(map[string]bool)
	for _, entry := range s.Entries() {
		existing[entry.Id] = true
	}
	prefix := make([]protocol.Entry, 0, len(page.Entries)+len(s.older))
	for _, entry := range page.Entries {
		if !existing[entry.Id] {
			prefix = append(prefix, entry)
			existing[entry.Id] = true
		}
	}
	s.older, s.Before = append(prefix, s.older...), page.Before
}

func (s *State) Entries() []protocol.Entry {
	entries := make([]protocol.Entry, 0, len(s.older)+len(s.Current.Entries))
	entries = append(entries, s.older...)
	return append(entries, s.Current.Entries...)
}
