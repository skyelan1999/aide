package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssistantWatchReadOnlyLifecycle(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	path := "/api/sessions/" + id + "/assistant-watch"
	unauth := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, path, nil))
	requireStatus(t, unauth, 401)
	requireStatus(t, request(a, "GET", path, nil), 423)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.sessions["followed"] = &Session{ID: "followed", Title: "交办的工作", Number: 99, FollowedByAssistant: true, Runs: []*Task{{ID: "watch-run", Status: "running"}}}
	a.sessions["unfollowed"] = &Session{ID: "unfollowed", Title: "无关会话", Runs: []*Task{{ID: "other-run", Status: "completed"}}}
	a.reminders = append(a.reminders, Reminder{ID: "watch-due", Title: "到期提醒", Area: "global", Notify: "xiaomi", Status: "pending", DueAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}, Reminder{ID: "watch-hidden", Title: "其他工作区提醒", Area: "workspace", WorkspaceID: "not-this-workspace", Status: "pending", Notify: "xiaomi", DueAt: time.Now().Add(-time.Minute).Format(time.RFC3339)})
	beforeSessions := len(a.sessions)
	beforeMessages := len(a.sessions[id].Messages)
	a.mu.Unlock()
	get := func() []assistantWatchEvent {
		w := request(a, "GET", path, nil)
		requireStatus(t, w, 200)
		var out struct {
			Events []assistantWatchEvent `json:"events"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Events
	}
	events := get()
	if len(events) != 2 {
		t.Fatalf("unexpected watch scope: %+v", events)
	}
	var old string
	for _, event := range events {
		if event.Kind == "task" {
			old = event.ID
			if event.SessionID != "followed" {
				t.Fatal("unrelated task leaked")
			}
		}
	}
	a.mu.Lock()
	a.sessions["followed"].Runs[0].Status = "completed"
	a.mu.Unlock()
	changed := get()
	for _, event := range changed {
		if event.Kind == "task" && event.ID == old {
			t.Fatal("status transition did not produce a fresh event")
		}
	}
	a.mu.Lock()
	if len(a.sessions) != beforeSessions || len(a.sessions[id].Messages) != beforeMessages {
		t.Fatal("passive watch mutated sessions")
	}
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", path, nil), 423)
}
func TestAssistantWorkLedgerPrioritizesBlockersAndVisibleResults(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.sessions["done-work"] = &Session{ID: "done-work", Title: "Deliverable", FollowedByAssistant: true, Runs: []*Task{{ID: "done-run", Status: "completed", Steps: []Step{{Name: "plan", Content: "INTERNAL PLAN"}, {Name: "chat", Content: strings.Repeat("结果", 200), Reasoning: "PRIVATE REASONING"}}}}}
	a.sessions["blocked-work"] = &Session{ID: "blocked-work", Title: "Approval", FollowedByAssistant: true, Runs: []*Task{{ID: "block-run", Status: "awaiting_approval"}}}
	a.mu.Unlock()
	w := request(a, "GET", "/api/sessions/"+id+"/assistant-watch", nil)
	requireStatus(t, w, 200)
	var out struct {
		Events []assistantWatchEvent `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 2 || out.Events[0].SessionID != "blocked-work" || out.Events[0].Priority != 3 {
		t.Fatalf("blocker priority: %+v", out.Events)
	}
	result := out.Events[1]
	if result.Status != "completed" || len([]rune(result.Detail)) != 241 || strings.Contains(result.Detail, "PRIVATE") || strings.Contains(result.Detail, "INTERNAL") {
		t.Fatalf("unsafe or missing result preview: %+v", result)
	}
}
func TestAssistantVoiceUsesAgentAndNeverDraftFallback(t *testing.T) {
	srv := agenticScriptServer(t, [][2]any{{"我会继续陪你。", nil}})
	defer srv.Close()
	a := testApp(t)
	id := assistantSessionID(t, a)
	a.mu.Lock()
	a.settings.BaseURL = srv.URL
	a.settings.Model = "test"
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a)
	w := request(a, "POST", "/api/sessions/"+id+"/assistant-message", map[string]any{"text": "继续陪我聊", "inputSource": "voice"})
	requireStatus(t, w, 200)
	a.mu.Lock()
	s := a.sessions[id]
	if len(s.Messages) < 2 || s.Messages[0].Type != msgTypeVoiceIn {
		a.mu.Unlock()
		t.Fatal("voice was not recorded through assistant pipeline")
	}
	a.settings.BaseURL = ""
	a.settings.Model = ""
	before := len(a.sessions)
	a.mu.Unlock()
	w = request(a, "POST", "/api/sessions/"+id+"/assistant-message", map[string]any{"text": "背景声不能派发工作", "inputSource": "voice"})
	requireStatus(t, w, 503)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) != before {
		t.Fatal("failed voice inference silently dispatched a new task")
	}
	dispatch := a.dispatchToAideLocked("测试交办任务")
	target := a.sessions[dispatch["sessionId"].(string)]
	if !target.FollowedByAssistant {
		t.Fatal("assistant dispatch did not opt into follow-up")
	}
}
