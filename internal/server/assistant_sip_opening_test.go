package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantSIPOpeningUsesHistoryWithoutToolsOrSelfIntroduction(t *testing.T) {
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	id := assistantSessionID(t, a)
	a.mu.Lock()
	a.sessionLocked(id).Messages = []Message{{Role: "user", Content: "我们继续聊天津的天气"}, {Role: "user", Content: "密码 secret"}, {Role: "user", Content: "审批188"}}
	workspace := a.wsID()
	a.mu.Unlock()
	a.mu.Lock()
	for i := 0; i < 4; i++ {
		a.sessionLocked(id).Messages = append(a.sessionLocked(id).Messages, Message{Role: "user", Content: "Hello."})
	}
	a.mu.Unlock()
	instructions := a.nativeOpeningInstructions(id, workspace)
	if !strings.Contains(instructions, "天津的天气") || strings.Contains(instructions, "secret") || strings.Contains(instructions, "审批188") {
		t.Fatal("unsafe historical topic selection")
	}
	for _, text := range []string{"上次聊到天津的天气，要接着聊吗？", "你好，我是小绵。"} {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid request")
			}
			if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
				t.Error("opening offered tools")
			}
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}}})
		}))
		got, err := nativeIncomingOpeningText(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, instructions)
		provider.Close()
		if strings.Contains(text, "我是") {
			if err == nil {
				t.Fatal("fixed self introduction accepted")
			}
		} else if err != nil || got != text {
			t.Fatal(got, err)
		}
	}
}

func TestAssistantSIPOpeningRequiresUnboundIncomingHostTicket(t *testing.T) {
	a, ctx, _, _ := sipFixture(t)
	requireStatus(t, request(a, "POST", "/api/assistant-sip/opening", map[string]any{"callId": sipContext(ctx).CallID}), 403)
	requireStatus(t, sipFixtureRequest(a, "/api/assistant-sip/opening", map[string]any{"callId": sipContext(ctx).CallID}), 409)
	requireStatus(t, sipFixtureRequest(a, "/api/assistant-sip/opening", map[string]any{"callId": "unknown"}), 409)
}
