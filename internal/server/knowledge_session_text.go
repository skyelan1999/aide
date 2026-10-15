package server

import "strings"

// A compact summary has no per-workspace provenance. Keep it in the chat,
// but do not use it (or unanchored legacy messages) as scoped map/model text.
func knowledgeScopedSessionText(s *Session, scope string) (string, bool) {
	known, duplicates := map[string]bool{}, map[string]bool{}
	for _, r := range s.Runs {
		if r == nil {
			continue
		}
		if _, exists := known[r.ID]; exists {
			duplicates[r.ID] = true
		}
		known[r.ID] = r.WorkspaceID == scope
	}
	start := max(0, len(s.Messages)-256)
	partial := start > 0 || s.Compact != ""
	parts := []string{}
	for i := len(s.Messages) - 1; i >= start; i-- {
		m := s.Messages[i]
		if m.SourceRunID == "" {
			if m.Content != "" {
				partial = true
			}
			continue
		}
		bound, exists := known[m.SourceRunID]
		if !exists || duplicates[m.SourceRunID] {
			partial = true
			continue
		}
		if !bound {
			continue
		}
		if len(parts) >= 6 {
			partial = true
			break
		}
		parts = append(parts, knowledgeClip(m.Content, 800))
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return knowledgeClip(strings.Join(parts, "\n"), 6000), partial
}
