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

func TestAssistedApprovalLocalFiles(t *testing.T) {
	for _, mode := range []string{"new", "existing", "update", "changed", "disabled", "readonly", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			prompt := "Create qa.md containing QA"
			if mode == "update" {
				prompt = "Update qa.md from its existing text to QA"
			}
			task := &Task{ID: "files-review", Prompt: prompt, Status: "awaiting_approval", AutoReview: true, WorkspaceID: a.wsID(), Files: []Change{{Path: "qa.md", Content: "QA"}}}
			s := &Session{ID: "files-owner", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
			a.sessions[s.ID] = s
			if mode == "existing" || mode == "update" {
				if err := os.WriteFile(filepath.Join(a.workPath, "qa.md"), []byte("original"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "update" {
				// A legitimate existing-file proposal carries the exact read version.
				task.Files[0].Before = "original"
				task.Files[0].BaseHash = hash([]byte("original"))
			}
			if mode == "readonly" {
				a.settings.SandboxMode = "read-only"
			}
			if mode == "symlink" {
				if err := os.WriteFile(filepath.Join(a.workPath, "target.txt"), []byte("untouched"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target.txt", filepath.Join(a.workPath, "qa.md")); err != nil {
					t.Fatal(err)
				}
			}
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages []Message
					Tools    []any
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				request := payload.Messages[1].Content
				if len(payload.Tools) != 0 || !strings.Contains(request, prompt) {
					t.Error("missing authorization or reviewer tools")
				}
				if mode == "update" && (!strings.Contains(request, "original") || !strings.Contains(request, "baseHash")) {
					t.Error("reviewer did not receive the exact original and version")
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
				decision := `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"authorized exact local proposal"}`
				if mode == "existing" {
					decision = `{"decision":"manual","risk":"medium","withinWorkspace":true,"authorized":false,"reason":"the user asked to create, not replace the existing file"}`
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: decision}}}})
			}))
			defer model.Close()
			a.settings.BaseURL = model.URL
			a.settings.Model = "test"
			a.reviewPendingFiles(context.Background(), task)
			b, err := os.ReadFile(filepath.Join(a.workPath, "qa.md"))
			if mode == "new" || mode == "update" {
				if err != nil || string(b) != "QA" || !task.Applied || task.Status != "completed" {
					t.Fatalf("not applied: %s %v %+v", b, err, task)
				}
			} else if mode == "symlink" {
				if err != nil || string(b) != "untouched" || task.Applied {
					t.Fatalf("symlink proposal was applied: %s %v %+v", b, err, task)
				}
				info, statErr := os.Lstat(filepath.Join(a.workPath, "qa.md"))
				if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("symlink target was unexpectedly replaced")
				}
			} else if mode == "existing" {
				if string(b) != "original" || task.Applied {
					t.Fatal("unrequested replacement was applied")
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
