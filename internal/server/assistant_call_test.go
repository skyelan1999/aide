package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantCallUsesSpokenGuidanceAndBoundedReply(t *testing.T) {
	var body struct {
		Messages  []Message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
		Tools     []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "嗯，你接着说。"}}}})
	}))
	defer srv.Close()
	a := testApp(t)
	_, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, "我比较", "", "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) == 0 || !strings.Contains(body.Messages[0].Content, assistantCallGuidance) {
		t.Fatal("missing explicit call guidance")
	}
	if body.MaxTokens != 240 {
		t.Fatalf("unbounded call reply: %d", body.MaxTokens)
	}
	for _, tool := range body.Tools {
		if tool.Function.Name == "be_silent" {
			t.Fatal("explicit call inherits ambient classification")
		}
	}
}

func TestAssistantCallNeverFallsBackToWorkDispatch(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.voiceAgent = nil
	before := len(a.sessions)
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions/"+id+"/assistant-message", map[string]any{"text": "随便聊聊", "inputSource": "call"})
	requireStatus(t, w, 503)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) != before {
		t.Fatal("failed call created a work task")
	}
}
