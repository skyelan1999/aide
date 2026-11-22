package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// assistantSessionID 从测试 App 取出唯一小秘系统会话 ID。
func assistantSessionID(t *testing.T, a *App) string {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.findAssistantSessionLocked()
	if s == nil {
		t.Fatal("assistant session not created at startup")
	}
	return s.ID
}

// unlockAssistantSessionForTest 模拟前端通过小秘密码门：标记唯一小秘会话为已解锁。
// #62 后 assistant-message / voice-filter 的发送门查 a.assistantUnlocked，跑 agentic
// 路径的测试需先解锁；未解锁的门控测试则不调用本函数。
func unlockAssistantSessionForTest(t *testing.T, a *App) {
	t.Helper()
	a.mu.Lock()
	s := a.findAssistantSessionLocked()
	a.mu.Unlock()
	if s == nil {
		t.Fatal("assistant session not created at startup")
	}
	a.markAssistantUnlocked(s.ID)
}

func waitDispatchedRunTerminal(t *testing.T, a *App, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		var status string
		if s := a.sessions[sessionID]; s != nil && len(s.Runs) > 0 {
			status = s.Runs[len(s.Runs)-1].Status
		}
		a.mu.Unlock()
		if status == "completed" || status == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("dispatched aide run did not finish before cleanup (session %s)", sessionID)
}

// TestContextToolsForAssistantOnly 跨会话工具只在小秘系统会话注入，普通会话不可见。
func TestContextToolsForAssistantOnly(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	assistant := a.findAssistantSessionLocked()
	normal := &Session{ID: "x", Kind: ""}
	a.mu.Unlock()

	tools := a.contextToolsFor(assistant)
	names := map[string]bool{}
	for _, tl := range tools {
		if fn, ok := tl.(map[string]any)["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	for _, want := range []string{"search_sessions", "get_session", "follow_session", "push_to_session"} {
		if !names[want] {
			t.Fatalf("assistant session missing cross tool %q; have %v", want, names)
		}
	}

	plain := a.contextToolsFor(normal)
	for _, tl := range plain {
		if fn, ok := tl.(map[string]any)["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); n == "search_sessions" || n == "push_to_session" {
				t.Fatalf("normal session leaked cross tool %q", n)
			}
		}
	}
}

// Missing model configuration must not turn a chat message into work.
func TestAssistantMessageMissingModelDoesNotDispatch(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	before := len(a.sessions)
	w := request(a, "POST", "/api/sessions/"+id+"/assistant-message", map[string]string{"text": "帮我把首页按钮改成蓝色"})
	requireStatus(t, w, 503)
	if len(a.sessions) != before || len(a.findAssistantSessionLocked().Messages) != 0 {
		t.Fatal("missing model dispatched or recorded a false action")
	}
}

// TestAssistantMessageRejectsNonAssistant 非小秘会话拒绝该端点。
func TestAssistantMessageRejectsNonAssistant(t *testing.T) {
	a := testApp(t)
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "普通会话"})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/assistant-message",
		map[string]string{"text": "你好"})
	requireStatus(t, w, 400)
}

// Missing voice model configuration must not turn ambient speech into work.
func TestVoiceFilterMissingModelDoesNotDispatch(t *testing.T) {
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	before := len(a.sessions)
	w := request(a, "POST", "/api/voice-filter", map[string]string{"text": "只读查询审批状态，不批准。"})
	requireStatus(t, w, 503)
	if len(a.sessions) != before || len(a.findAssistantSessionLocked().Messages) != 0 {
		t.Fatal("voice model failure dispatched or recorded a false action")
	}
}

// TestPushToSessionCreatesAndPushes 通过 handler 直接覆盖 push_to_session 的 new/ref 分支。
func TestDispatchToAideLockedCreatesSession(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	disp := a.dispatchToAideLocked("写一个单元测试")
	a.mu.Unlock()
	if disp == nil || disp["number"] == nil {
		t.Fatalf("dispatch returned %v", disp)
	}
}

func TestStartDispatchedAssistantTaskStartsRun(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = "http://127.0.0.1:1" // execution may fail later; run creation is synchronous
	a.settings.Model = "test"
	disp := a.dispatchToAideLocked("写一个单元测试")
	a.mu.Unlock()

	a.startDispatchedAssistantTask(disp, "写一个单元测试", false)
	if disp["started"] != true || disp["runId"] == nil {
		t.Fatalf("delegated task was not started: %v", disp)
	}
	sessionID, _ := disp["sessionId"].(string)
	a.mu.Lock()
	s := a.sessions[sessionID]
	if s == nil || s.PendingPrompt != "" || len(s.Runs) != 1 || s.Runs[0].Status != "running" {
		a.mu.Unlock()
		t.Fatalf("delegated session did not enter running state: %+v", s)
	}
	a.mu.Unlock()
	waitDispatchedRunTerminal(t, a, sessionID)
}

// 让上面引用的 httptest 包在未使用时不报错（保持与既有测试一致的 import 风格）。
var _ = httptest.NewRecorder

// A failed model decision grants no authority to dispatch, including explicit
// work requests and read-only approval queries across text and call inputs.
func TestAssistantMessageModelFailureDoesNotDispatch(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		for _, source := range []string{"text", "call", "voice", "filter"} {
			t.Run(engine+"/"+source, func(t *testing.T) {
				t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Unsupported model fixture", 400) }))
				defer srv.Close()
				a := testApp(t)
				a.settings.BaseURL = srv.URL
				a.settings.Model = "test"
				unlockAssistantSessionForTest(t, a)
				id := assistantSessionID(t, a)
				before := len(a.sessions)
				for _, text := range []string{"只读查询待审批数量，不批准、不派发任务。", "帮我把那个按钮改一下", "你好"} {
					path := "/api/sessions/" + id + "/assistant-message"
					if source == "filter" {
						path = "/api/voice-filter"
					}
					w := request(a, "POST", path, map[string]string{"text": text, "inputSource": source})
					requireStatus(t, w, 503)
					if len(a.sessions) != before || len(a.findAssistantSessionLocked().Messages) != 0 {
						t.Fatal("model failure dispatched or recorded a false action")
					}
				}
			})
		}
	}
}

// TestAssistantMessageGateLockedUnlockedClear #62：assistant-message 走 assistantUnlocked 门。
// 未解锁→locked(新文案)；解锁后放行；clearAssistantUnlock(锁屏)后再次 locked。
// 门在调用模型前拦截，故 BaseURL 指向不可达地址也能验证门本身。
func TestAssistantMessageGateLockedUnlockedClear(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = "http://127.0.0.1:1"
	a.settings.Model = "test"
	a.mu.Unlock()
	assistantID := assistantSessionID(t, a)

	// 1) 未解锁：返回 locked + 新文案
	w := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message", map[string]string{"text": "你好"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "locked" {
		t.Fatalf("before unlock: expected action=locked, got %v", body)
	}
	if reason, _ := body["reason"].(string); reason != "小秘已锁定，请在小秘会话中解锁" {
		t.Fatalf("lock reason = %q, want 新文案", reason)
	}

	// 2) Unlocked requests reach the unavailable model and fail without dispatch.
	unlockAssistantSessionForTest(t, a)
	w2 := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message", map[string]string{"text": "你好"})
	requireStatus(t, w2, 503)
	var body2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &body2)
	if body2["action"] == "locked" {
		t.Fatalf("after unlock: must NOT be locked, got %v", body2)
	}

	// 3) 锁屏清空内存解锁态：再次 locked
	a.clearAssistantUnlock()
	w3 := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message", map[string]string{"text": "你好"})
	requireStatus(t, w3, 200)
	var body3 map[string]any
	_ = json.Unmarshal(w3.Body.Bytes(), &body3)
	if body3["action"] != "locked" {
		t.Fatalf("after lockscreen clear: expected locked, got %v", body3)
	}
}

// TestVoiceFilterGateLocked #62：voice-filter 与文字同一把锁，未解锁返回 locked + 新文案。
func TestVoiceFilterGateLocked(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = "http://127.0.0.1:1"
	a.settings.Model = "test"
	a.mu.Unlock()

	w := request(a, "POST", "/api/voice-filter", map[string]string{"text": "测试语音"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "locked" {
		t.Fatalf("voice-filter before unlock: expected locked, got %v", body)
	}
	if reason, _ := body["reason"].(string); reason != "小秘已锁定，请在小秘会话中解锁" {
		t.Fatalf("voice-filter lock reason = %q, want 新文案", reason)
	}
}
