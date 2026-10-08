package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistedApprovalNewFiles(t *testing.T) {
	for _, mode := range []string{"new", "existing", "changed", "disabled", "readonly"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			task := &Task{ID: "files-review", Prompt: "Create qa.md containing QA", Status: "awaiting_approval", AutoReview: true, WorkspaceID: a.wsID(), Files: []Change{{Path: "qa.md", Content: "QA"}}}
			s := &Session{ID: "files-owner", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
			a.sessions[s.ID] = s
			if mode == "existing" {
				os.WriteFile(filepath.Join(a.workPath, "qa.md"), []byte("original"), 0644)
			}
			if mode == "readonly" {
				a.settings.SandboxMode = "read-only"
			}
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages []Message
					Tools    []any
				}
				json.NewDecoder(r.Body).Decode(&payload)
				if len(payload.Tools) != 0 || !strings.Contains(payload.Messages[1].Content, "Create qa.md") {
					t.Error("missing authorization or reviewer tools")
				}
				a.mu.Lock()
				if mode == "changed" {
					task.Files[0].Content = "changed"
				}
				if mode == "disabled" {
					task.AutoReview = false
					task.approvalGeneration++
				}
				a.mu.Unlock()
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"authorized new file"}`}}}})
			}))
			defer model.Close()
			a.settings.BaseURL = model.URL
			a.settings.Model = "test"
			a.reviewPendingFiles(context.Background(), task)
			b, err := os.ReadFile(filepath.Join(a.workPath, "qa.md"))
			if mode == "new" {
				if err != nil || string(b) != "QA" || !task.Applied || task.Status != "completed" {
					t.Fatalf("not applied: %s %v %+v", b, err, task)
				}
			} else if mode == "existing" {
				if string(b) != "original" || task.Applied {
					t.Fatal("original overwritten")
				}
			} else if !os.IsNotExist(err) || task.Applied {
				t.Fatal("stale or forbidden proposal applied")
			}
		})
	}
}
