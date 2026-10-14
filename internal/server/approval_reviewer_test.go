package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestApprovalReviewerBoundedRecovery(t *testing.T) {
	approve := `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"任务内常规操作"}`
	for _, tc := range []struct {
		name, first, second string
		want                bool
		calls               int
	}{
		{"empty", "", approve, true, 2}, {"invalid", "not-json", approve, true, 2}, {"manual", `{"decision":"manual","reason":"需要确认"}`, approve, false, 1},
		{"manual-no-reason", `{"decision":"manual"}`, approve, false, 1}, {"invalid-twice", "bad", "bad", false, 2},
		{"unauthorized", `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":false,"reason":"未授权"}`, approve, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := n.Add(1)
				var body struct {
					MaxTokens int `json:"max_tokens"`
					Tools     []any
					Effort    string `json:"reasoning_effort"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				expected := 512
				if call == 2 {
					expected = 2048
				}
				if body.MaxTokens != expected || len(body.Tools) != 0 || body.Effort != "high" {
					t.Errorf("unexpected review request: %+v", body)
				}
				text := tc.first
				if call > 1 {
					text = tc.second
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": Message{Role: "assistant", Content: text}}}, "usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12}})
			}))
			defer server.Close()
			ok, reason, usage := reviewApproval(context.Background(), Settings{BaseURL: server.URL, Model: "test", ReasoningEffort: "high"}, []Message{{Role: "system", Content: assistedApprovalPolicy}, {Role: "user", Content: "mkdir reports"}})
			if ok != tc.want || int(n.Load()) != tc.calls || strings.TrimSpace(reason) == "" {
				t.Fatalf("result %v %q calls %d", ok, reason, n.Load())
			}
			if usage.Total != 12*tc.calls {
				t.Fatalf("lost review attempt usage: %+v", usage)
			}
		})
	}
}
func TestApprovalReviewerErrorsNeverApprove(t *testing.T) {
	for _, mode := range []string{"http", "tools", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n.Add(1)
				if mode == "http" {
					http.Error(w, "upstream unavailable", 503)
					return
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{readCall("run_shell", `{"command":"touch x"}`)}}}}})
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			approved, reason, _ := reviewApproval(ctx, Settings{BaseURL: server.URL, Model: "test"}, []Message{{Role: "user", Content: "mkdir reports"}})
			expected := int32(1)
			if mode == "cancelled" {
				expected = 0
			}
			if approved || n.Load() != expected || reason == "" {
				t.Fatalf("unsafe reviewer result %v %q calls%d", approved, reason, n.Load())
			}
		})
	}
}

func TestAssistedBasicCommandRecoversAndExecutesOnce(t *testing.T) {
	a := testApp(t)
	var n atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := n.Add(1)
		text := ""
		if call > 1 {
			text = `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"用户授权在任务目录内创建报告目录"}`
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": Message{Role: "assistant", Content: text}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	task := &Task{ID: "basic-command", Prompt: "在当前工程创建 reports 目录用于保存报告", Status: "running", AutoReview: true, WorkspaceID: a.wsID()}
	s := &Session{ID: "basic-owner", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[s.ID] = s
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, ok := a.approvedShell(ctx, task, "mkdir reports", 0)
	if !ok || !strings.Contains(result, "exit code: 0") {
		t.Fatalf("recovered reviewer did not release command: %s", result)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if n.Load() != 2 || len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Status != "approved" {
		t.Fatalf("unexpected attempts/receipt %d %+v", n.Load(), task.ApprovalReviews)
	}
	if info, err := os.Stat(filepath.Join(a.workPath, "reports")); err != nil || !info.IsDir() {
		t.Fatal("approved directory was not created")
	}
}
