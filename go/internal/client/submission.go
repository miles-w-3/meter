package client

import (
	"strings"

	"github.com/google/uuid"
	"pi-workspace.local/workspace/internal/protocol"
)

// Submission retains one user action while it is in flight or its outcome is unclear.
type Submission struct {
	input    *protocol.SubmitInput
	inFlight bool
	unknown  bool
}

func (s *Submission) Begin(mode protocol.SubmitInputMode, content string) bool {
	if s.input != nil || strings.TrimSpace(content) == "" {
		return false
	}
	s.input = &protocol.SubmitInput{RequestId: uuid.NewString(), Mode: mode, Content: content}
	return true
}

func (s *Submission) Take() (protocol.SubmitInput, bool) {
	if s.input == nil || s.inFlight {
		return protocol.SubmitInput{}, false
	}
	s.inFlight = true
	return *s.input, true
}

func (s *Submission) Resolve(err error) {
	s.inFlight = false
	s.unknown = err != nil && Uncertain(err)
	if err == nil {
		s.input = nil
	}
}

func (s *Submission) Uncertain() bool { return s.unknown }

// Restore returns a rejected input to the editor and clears the pending item.
// An ambiguous input keeps its request ID until it can be reconciled.
func (s *Submission) Restore() string {
	if s.input == nil || s.inFlight || s.unknown {
		return ""
	}
	content := s.input.Content
	s.input = nil
	return content
}
