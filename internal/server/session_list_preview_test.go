package server

import (
	"strings"
	"testing"
)

func TestSessionListPreviewUsesLatestUserInput(t *testing.T) {
	s := &Session{ID: "preview", Runs: []*Task{
		{Prompt: "first request"},
		{Prompt: "  latest   request\nwith a second line  "},
	}}
	if got := sessionListPreview(s); got != "latest request with a second line" {
		t.Fatalf("unexpected preview: %q", got)
	}
	s.Runs = nil
	s.Messages = []Message{{Role: "assistant", Content: "answer"}, {Role: "user", Content: "message fallback"}}
	if got := sessionListPreview(s); got != "message fallback" {
		t.Fatalf("unexpected message fallback: %q", got)
	}
	s.Messages = []Message{{Role: "user", Content: strings.Repeat("长", 300)}}
	if got := []rune(sessionListPreview(s)); len(got) != 161 || got[len(got)-1] != '…' {
		t.Fatalf("preview is not bounded to 160 runes plus an ellipsis: %d", len(got))
	}
}

func TestSessionListPreviewDoesNotExposeAssistantPrivateMessages(t *testing.T) {
	s := &Session{Kind: assistantSessionKind, Messages: []Message{{Role: "user", Content: "private question"}}}
	if got := sessionListPreview(s); got != "" {
		t.Fatalf("assistant private content escaped into list preview: %q", got)
	}
}
