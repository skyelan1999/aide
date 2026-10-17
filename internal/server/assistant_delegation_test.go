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

func TestAssistantPushActuallyStartsAndLabelsTask(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = "http://127.0.0.1:1"
	a.settings.Model = "test"
	a.settings.AssistantAutoReview = true
	a.approvalPolicy.Enabled = false
	a.mu.Unlock()
	result := a.execAssistantTool(delegationCall("push_to_session", `{"ref":"new","message":"Create the requested report"}`), &assistantDecision{}, "Please create a report")
	if !strings.Contains(result, "启动") {
		t.Fatalf("not started: %s", result)
	}
	a.mu.Lock()
	var target *Session
	for _, s := range a.sessions {
		if s.Kind != assistantSessionKind && s.FollowedByAssistant {
			target = s
			break
		}
	}
	if target == nil || len(target.Runs) != 1 {
		a.mu.Unlock()
		t.Fatal("push only created a draft")
	}
	task := target.Runs[0]
	if !task.AssistantManaged || !task.AutoReview || task.AssistantAuthorization != "Please create a report" || target.PendingPrompt != "" || target.Messages[0].Type != assistantTaskMessageType {
		a.mu.Unlock()
		t.Fatal("missing provenance, label or reviewer")
	}
	id := target.ID
	a.mu.Unlock()
	waitDispatchedRunTerminal(t, a, id)
}

func TestAssistantApprovalSettingScopePreservationAndRevocation(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.approvalPolicy.Enabled = false
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"assistantAutoReview": true}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"voiceReplyEnabled": true}), 200)
	a.mu.Lock()
	if !a.settings.AssistantAutoReview {
		a.mu.Unlock()
		t.Fatal("unrelated settings patch reset approval setting")
	}
	plain := &Task{ID: "plain", Status: "running"}
	delegated := &Task{ID: "delegated", Status: "running", AssistantManaged: true, AutoReview: true}
	if a.taskAutoReviewLocked(plain) || !a.taskAutoReviewLocked(delegated) {
		a.mu.Unlock()
		t.Fatal("review scope widened")
	}
	cancelled := false
	delegated.approvalCancel = func() { cancelled = true }
	a.sessions["owners"] = &Session{ID: "owners", Runs: []*Task{plain, delegated}}
	generation := delegated.approvalGeneration
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"assistantAutoReview": false}), 200)
	a.mu.Lock()
	defer a.mu.Unlock()
	if delegated.AutoReview || !cancelled || delegated.approvalGeneration <= generation || plain.AutoReview {
		t.Fatal("disabled setting did not revoke delegated review")
	}
	var saved Settings
	b, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil || json.Unmarshal(b, &saved) != nil || saved.AssistantAutoReview {
		t.Fatal("setting was not saved")
	}
}

func TestAssistantPushNeverAnswersPendingApproval(t *testing.T) {
	a := testApp(t)
	ch := make(chan string, 1)
	a.mu.Lock()
	a.sessions["waiting"] = &Session{ID: "waiting", Runs: []*Task{{ID: "pending", Status: "awaiting_clarification", AnswerCh: ch}}}
	a.mu.Unlock()
	result := a.execAssistantTool(delegationCall("push_to_session", `{"ref":"waiting","message":"Confirm everything"}`), &assistantDecision{}, "Do the original work")
	if !strings.Contains(result, "未启动") {
		t.Fatal(result)
	}
	select {
	case <-ch:
		t.Fatal("generated task text became a user's approval answer")
	default:
	}
}

func TestAssistantReviewerAppliesNewFileUsingOriginalAuthority(t *testing.T) {
	a := testApp(t)
	a.settings.AssistantAutoReview = true
	a.approvalPolicy.Enabled = false
	task := &Task{ID: "delegated-file", Prompt: "Generated task summary", AssistantManaged: true, AssistantAuthorization: "Create qa.md containing QA", Status: "awaiting_approval", WorkspaceID: a.wsID(), Files: []Change{{Path: "qa.md", Content: "QA"}}}
	task.AutoReview = a.taskAutoReviewLocked(task)
	a.sessions["file-owner"] = &Session{ID: "file-owner", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: "MODEL GENERATED AUTHORITY", Type: assistantTaskMessageType}}}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !strings.Contains(in.Messages[1].Content, "Create qa.md") || strings.Contains(in.Messages[1].Content, "MODEL GENERATED AUTHORITY") {
			t.Error("review used generated content as authorization")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"requested new file"}`}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	a.reviewPendingFiles(context.Background(), task)
	b, err := os.ReadFile(filepath.Join(a.workPath, "qa.md"))
	if err != nil || string(b) != "QA" || !task.Applied || len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Source != "assistant" {
		t.Fatalf("no audited file application: %v %+v", err, task.ApprovalReviews)
	}
}

func delegationCall(name, args string) ToolCall {
	var c ToolCall
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}
