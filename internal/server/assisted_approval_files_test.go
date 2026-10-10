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

func TestReviewedFileResult(t *testing.T) {
	for _, mode := range []string{"pending", "applied", "changed", "switched"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			task := &Task{ID: "receipt", AutoReview: true, Status: "completed", WorkspaceID: a.wsID(), Files: []Change{{Path: "qa.md", Content: "QA"}}}
			s := &Session{ID: "receipt-owner", Runs: []*Task{task}, Messages: []Message{{Role: "assistant", Content: "Earlier model answer"}}}
			a.sessions[s.ID] = s
			if mode != "pending" {
				task.Applied, task.Files[0].Applied = true, true
				content := "QA"
				if mode == "changed" {
					content = "changed"
				}
				if err := os.WriteFile(filepath.Join(a.workPath, "qa.md"), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "switched" {
				task.WorkspaceID = "other-workspace"
			}
			pending := a.pendingNewFileRead(task, "./qa.md")
			if (mode == "pending") != strings.Contains(pending, "不是读取失败") {
				t.Fatalf("pending read: %q", pending)
			}
			if got := a.pendingNewFileRead(task, "unrelated.md"); got != "" {
				t.Fatal("unrelated reads intercepted")
			}
			a.recordReviewedFileResult(task)
			a.recordReviewedFileResult(task)
			if len(task.Steps) != 1 || len(s.Messages) != 2 || s.Messages[0].Content != "Earlier model answer" {
				t.Fatal("receipt duplicated or original answer rewritten")
			}
			content := task.Steps[0].Content
			switch mode {
			case "pending":
				if !strings.Contains(content, "尚未全部应用") || strings.Contains(content, "读回一致") {
					t.Fatal(content)
				}
			case "applied":
				if !strings.Contains(content, "读回一致，2 字节，SHA-256") {
					t.Fatal(content)
				}
			case "changed":
				if task.Steps[0].Status != "failed" || !strings.Contains(content, "内容已变化") {
					t.Fatal(content)
				}
			case "switched":
				if !strings.Contains(content, "未进行本地字节读回") || strings.Contains(content, "读回一致") {
					t.Fatal(content)
				}
			}
		})
	}
}
