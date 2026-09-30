package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// agenticScriptServer 返回一个按脚本依次吐响应的 mock 模型：
// 每轮 LLM 调用依次返回 script[i] = (content, toolCalls)。
func agenticScriptServer(t *testing.T, script [][2]any) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		i := idx
		idx++
		var content string
		var calls []ToolCall
		if i < len(script) {
			if c, ok := script[i][0].(string); ok {
				content = c
			}
			if tc, ok := script[i][1].([]ToolCall); ok {
				calls = tc
			}
		}
		msg := map[string]any{"role": "assistant", "content": content}
		if calls != nil {
			msg["tool_calls"] = calls
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
}

func asstToolCall(name, args string) ToolCall {
	c := ToolCall{ID: "call-1", Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// TestAssistantAgenticChatNoDispatch 小秘陪聊：模型只回正文、不调任何派发工具，
// 必须不创建 aide 会话、不向主会话派发，回复留在小秘会话。
func TestAssistantAgenticChatNoDispatch(t *testing.T) {
	srv := agenticScriptServer(t, [][2]any{
		{"你今天听起来有点累呢，早点休息吧，我在这儿陪你。", nil},
	})
	defer srv.Close()

	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = srv.URL
	a.settings.Model = "test"
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a) // #62：agentic 路径需先过小秘密码门
	assistantID := assistantSessionID(t, a)

	beforeSessions := 0
	a.mu.Lock()
	beforeSessions = len(a.sessions)
	a.mu.Unlock()

	w := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message",
		map[string]string{"text": "今天上班好累啊"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	if body["action"] != "chat" {
		t.Fatalf("expected chat, got %v", body)
	}
	if body["dispatched"] != nil {
		t.Fatalf("chat must NOT dispatch, got %v", body["dispatched"])
	}
	// 没有新建 aide 会话
	a.mu.Lock()
	afterSessions := len(a.sessions)
	a.mu.Unlock()
	if afterSessions != beforeSessions {
		t.Fatalf("chat created new session: before=%d after=%d", beforeSessions, afterSessions)
	}
	// assistant 会话里留下了用户原话 + 小秘正文回复
	a.mu.Lock()
	msgs := a.findAssistantSessionLocked().Messages
	a.mu.Unlock()
	if len(msgs) < 2 {
		t.Fatalf("expected >=2 recorded msgs, got %d", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || last.Type != "" {
		t.Fatalf("chat reply should be normal assistant bubble, got %+v", last)
	}
}

func TestAssistantAgenticPluginResultReturnedToModel(t *testing.T) {
	pluginCode, err := os.ReadFile(filepath.Join("..", "..", "plugins", "current-time", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	var secondRound []Message
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestCount++
		if requestCount == 1 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant", "content": "", "tool_calls": []ToolCall{asstToolCall("get_current_datetime", `{"timeZone":"Asia/Shanghai"}`)},
			}}}})
			return
		}
		secondRound = body.Messages
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "现在是 2026-09-30 21:00，Asia/Shanghai。"}}}})
	}))
	defer srv.Close()

	a := testApp(t)
	install := request(a, "POST", "/api/plugins", map[string]any{"id": "current-time", "name": "Current time", "code": string(pluginCode)})
	requireStatus(t, install, 201)
	dec, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, "现在是什么时间？", "", "text")
	if err != nil {
		t.Fatalf("runAssistantAgenticLoop: %v", err)
	}
	if !strings.Contains(dec.Reply, "Asia/Shanghai") || len(secondRound) == 0 {
		t.Fatalf("tool result did not return to final model turn: decision=%+v messages=%+v", dec, secondRound)
	}
	found := false
	for _, message := range secondRound {
		if message.Role == "tool" && message.ToolCallID == "call-1" {
			var result struct {
				Date     string `json:"date"`
				Time     string `json:"time"`
				TimeZone string `json:"timeZone"`
			}
			if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
				t.Fatalf("tool result is not JSON: %q (%v)", message.Content, err)
			}
			if result.TimeZone != "Asia/Shanghai" || result.Date == "" || result.Time == "" {
				t.Fatalf("unexpected tool result: %+v", result)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("second model request omitted tool result: %+v", secondRound)
	}
}

// TestAssistantAgenticDispatch 小秘判断是工作任务：自行调用 dispatch_to_aide，
// 后端据此新建 aide 会话承接，并回报会话编号。
func TestAssistantAgenticDispatch(t *testing.T) {
	srv := agenticScriptServer(t, [][2]any{
		{"", []ToolCall{asstToolCall("dispatch_to_aide", `{"summary":"把首页主按钮改成蓝色","mode":"queue"}`)}},
		{"好，我已经把这件事交给 aide 了。", nil},
	})
	defer srv.Close()

	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = srv.URL
	a.settings.Model = "test"
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a) // #62：agentic 路径需先过小秘密码门
	assistantID := assistantSessionID(t, a)

	w := request(a, "POST", "/api/sessions/"+assistantID+"/assistant-message",
		map[string]string{"text": "帮我把那个首页按钮改成蓝色"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	if body["action"] != "dispatch" {
		t.Fatalf("expected dispatch, got %v", body)
	}
	disp, _ := body["dispatched"].(map[string]any)
	if disp == nil || disp["sessionId"] == nil {
		t.Fatalf("expected dispatched aide session, got %v", body["dispatched"])
	}
	if body["text"] != "把首页主按钮改成蓝色" {
		t.Fatalf("dispatch text not propagated, got %v", body["text"])
	}
	// 小秘确实调用了 dispatch_to_aide
	toolsUsed, _ := body["toolsUsed"].([]any)
	found := false
	for _, tl := range toolsUsed {
		if tl == "dispatch_to_aide" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected toolsUsed to include dispatch_to_aide, got %v", toolsUsed)
	}
}

// TestAssistantAgenticSilent 背景声/与他人对话：小秘调用 be_silent，不派发、不追问。
func TestAssistantAgenticSilent(t *testing.T) {
	srv := agenticScriptServer(t, [][2]any{
		{"", []ToolCall{asstToolCall("be_silent", `{"reason":"电视里在放广告"}`)}},
		{"", nil},
	})
	defer srv.Close()

	a := testApp(t)
	a.mu.Lock()
	a.settings.BaseURL = srv.URL
	a.settings.Model = "test"
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a) // #62：voice-filter 需先过小秘密码门

	beforeSessions := 0
	a.mu.Lock()
	beforeSessions = len(a.sessions)
	a.mu.Unlock()

	w := request(a, "POST", "/api/voice-filter", map[string]string{"text": "电视里在放广告"})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	// voice-filter 契约：silent 映射成 ignore，不 send
	if body["action"] != "ignore" {
		t.Fatalf("expected ignore (silent mapped), got %v", body)
	}
	a.mu.Lock()
	afterSessions := len(a.sessions)
	a.mu.Unlock()
	if afterSessions != beforeSessions {
		t.Fatalf("silent must not create aide session: before=%d after=%d", beforeSessions, afterSessions)
	}
}

// TestAssistantAgenticSelfIdentity 小秘 system prompt 仍含自我身份（不回归）。
func TestAssistantAgenticSelfIdentity(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	cfg := a.settings
	a.mu.Unlock()
	p := voiceIdentityPrompt(cfg)
	if !strings.Contains(p, "小秘") && !strings.Contains(p, "我是") {
		t.Fatal("identity prompt missing self-awareness")
	}
	if !strings.Contains(p, "aide") {
		t.Fatal("identity prompt missing aide division of labor")
	}
}

func TestAssistantRecentConversationRestoresShortTermContext(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	s := a.findAssistantSessionLocked()
	s.Messages = []Message{
		{Role: "user", Content: "你翻得历史记录嘛", Type: msgTypeTextIn},
		{Role: "assistant", Content: "能的，我可以帮你找", Type: ""},
		{Role: "user", Content: "支气管炎", Type: msgTypeTextIn},
	}
	a.mu.Unlock()

	history := a.recentAssistantConversation()
	if len(history) != 3 {
		t.Fatalf("recent context has %d messages, want 3: %+v", len(history), history)
	}
	if history[0].Content != "你翻得历史记录嘛" || history[2].Content != "支气管炎" {
		t.Fatalf("recent context order/content lost: %+v", history)
	}
	if !strings.Contains(assistantAgentPrinciples, "历史查询后只补充一个关键词") {
		t.Fatal("assistant instructions must treat a short follow-up topic as a continuation of a history lookup")
	}

	var sent []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = body.Messages
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "我去找一下历史记录。"}}}})
	}))
	defer srv.Close()
	_, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, "支气管炎", "", "text")
	if err != nil {
		t.Fatalf("runAssistantAgenticLoop: %v", err)
	}
	if len(sent) < 4 || sent[1].Content != "你翻得历史记录嘛" || sent[2].Content != "能的，我可以帮你找" || sent[3].Content != "支气管炎" {
		t.Fatalf("model request did not include ordered prior conversation and new input: %+v", sent)
	}
}
