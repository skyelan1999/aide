package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

// TestAssistantMessageRecordsAndDispatches 未配置模型时：文字输入走兜底，
// 必须把往来记录进 assistant 会话，且 send 时新建一个 aide 会话承接。
func TestAssistantMessageRecordsAndDispatches(t *testing.T) {
	a := testApp(t)
	assistantID := assistantSessionID(t, a)

	w := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message",
		map[string]string{"text": "帮我把首页按钮改成蓝色"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "send" {
		t.Fatalf("expected send fallback, got %v", body)
	}
	disp, _ := body["dispatched"].(map[string]any)
	if disp == nil || disp["sessionId"] == nil {
		t.Fatalf("expected dispatched aide session, got %v", body)
	}

	// assistant 会话里应留下用户原话 + 小蜜说明两条
	a.mu.Lock()
	as := a.findAssistantSessionLocked()
	msgs := as.Messages
	a.mu.Unlock()
	if len(msgs) < 2 {
		t.Fatalf("assistant session messages = %d, want >=2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Type != "text-in" {
		t.Fatalf("first msg = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Type != "voice-note" {
		t.Fatalf("second msg = %+v", msgs[1])
	}

	// 被派发的 aide 会话确实存在且含用户意图
	dispID, _ := disp["sessionId"].(string)
	a.mu.Lock()
	ds := a.sessions[dispID]
	a.mu.Unlock()
	if ds == nil || len(ds.Messages) == 0 || ds.Messages[0].Content == "" {
		t.Fatal("dispatched session not persisted")
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

// TestVoiceFilterRecordsToAssistantSession 语音 filter 的决策也要归位到 assistant 会话。
func TestVoiceFilterRecordsToAssistantSession(t *testing.T) {
	a := testApp(t)
	before := 0
	a.mu.Lock()
	before = len(a.findAssistantSessionLocked().Messages)
	a.mu.Unlock()

	// 未配置模型 → 走 recordFallback 路径
	w := request(a, "POST", "/api/voice-filter", map[string]string{"text": "电视里在放广告"})
	requireStatus(t, w, 200)

	a.mu.Lock()
	after := len(a.findAssistantSessionLocked().Messages)
	last := a.findAssistantSessionLocked().Messages[after-1]
	a.mu.Unlock()
	if after <= before {
		t.Fatalf("voice exchange not recorded: before=%d after=%d", before, after)
	}
	if last.Type != "voice-note" && last.Type != "voice-ask" {
		t.Fatalf("last recorded msg type = %q, want voice-note/ask", last.Type)
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

// 让上面引用的 httptest 包在未使用时不报错（保持与既有测试一致的 import 风格）。
var _ = httptest.NewRecorder


// TestAssistantMessageAgenticFallsBackToAnalyze 当 agentic 循环（带 tools 的请求）失败时，
// 回落到既有 analyze 甄别管线（不带 tools 的请求），仍能产出 ask 且不派发。
// 保留 analyze 能力不删的回归测试。
func TestAssistantMessageAgenticFallsBackToAnalyze(t *testing.T) {
	decision := `{"action":"ask","summarized":"","ask":"你想改哪个按钮？","mode":"queue","stop":false,"reason":"缺对象，需要追问"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) > 0 {
			// agentic 循环请求：让它失败，触发回落
			http.Error(w, "upstream boom", 500)
			return
		}
		// analyze 请求：正常返回 ask 决策
		jsonOut(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: decision}}},
		})
	}))
	defer srv.Close()

	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = srv.URL
	a.settings.Model = "test"
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a) // #62：agentic 路径需先过小秘密码门
	assistantID := assistantSessionID(t, a)

	w := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message",
		map[string]string{"text": "帮我把那个按钮改一下"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "ask" {
		t.Fatalf("expected ask from analyze fallback, got %v", body)
	}
	if body["dispatched"] != nil {
		t.Fatalf("ask must not dispatch, got %v", body["dispatched"])
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

	// 2) 解锁后：绝不再返回 locked（模型不可达会回落到 fallback，但不是 locked）
	unlockAssistantSessionForTest(t, a)
	w2 := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message", map[string]string{"text": "你好"})
	requireStatus(t, w2, 200)
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
