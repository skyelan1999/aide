package server

import (
	"testing"
	"time"
)

func TestIssue87OnlyExplicitProjectReferencesSelectRuns(t *testing.T) {
	a := testApp(t)
	start := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	prompts := map[string]string{
		"incidental":      "Explain an AI model and email maintenance",
		"substring":       "Please fix the aide display",
		"id-substring":    "asset-prefix-project12345-suffix is unrelated",
		"name-reference":  "Report [project-name:AI] progress",
		"other-reference": "Report [project-name:AIDE] progress",
		"id-reference":    "Report [project-id:project12345] progress",
		"file-reference":  "Attached project file",
	}
	s := &Session{ID: "scope", Number: 7}
	for id, prompt := range prompts {
		task := &Task{ID: id, Prompt: prompt, WorkspaceID: a.wsID(), Created: start.Format(time.RFC3339)}
		if id == "file-reference" {
			task.Attachments = []Attachment{{Root: "workspace", Path: "selected.pp"}}
		}
		s.Runs = append(s.Runs, task)
		s.Messages = append(s.Messages, Message{Role: "user", SourceRunID: id, Content: "content-" + id, CreatedAt: start.Format(time.RFC3339)})
	}
	a.sessions = map[string]*Session{s.ID: s}
	rows, _ := a.projectMessagesLocked("AI", "project12345", "selected.pp", start, start.AddDate(0, 0, 1))
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.RunID] = true
	}
	for _, id := range []string{"incidental", "substring", "id-substring", "other-reference"} {
		if seen[id] {
			t.Errorf("unrelated run selected by incidental match: %s", id)
		}
	}
	for _, id := range []string{"name-reference", "id-reference", "file-reference"} {
		if !seen[id] {
			t.Errorf("explicit project reference not selected: %s", id)
		}
	}
}

func TestIssue87ReferenceMarkersRequireWholeIdentity(t *testing.T) {
	for _, tc := range []struct{ prompt, name, id, want string }{
		{"Discuss AI", "AI", "", ""},
		{"[project-name:ai]", "AI", "", "projectName"},
		{"[project-name:AIDE]", "AI", "", ""},
		{"[project-id:project12345-other]", "Other", "project12345", ""},
		{"[project-id:project12345]", "Other", "project12345", "projectId"},
		{"[project-name:\nAI]", "AI", "", ""},
		{"[project-id:short]", "Other", "short", ""},
		{"[project-name:AI] and [project-id:project12345]", "AI", "project12345", "projectId"},
	} {
		if got := explicitProjectReference(tc.prompt, tc.name, tc.id); got != tc.want {
			t.Errorf("reference %q returned %q want%q", tc.prompt, got, tc.want)
		}
	}
}
