package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssistantInitiativeGroundedAndOneShot(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["messages"].([]any)) != 2 {
			t.Error("host history included")
		}
		if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
			t.Error("tools exposed")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"speak":true,"reply":"要不要先定一个小步骤？"}`}}}})
	}))
	defer model.Close()
	a.mu.Lock()
	a.settings.XiaomiModelSource = "custom"
	a.settings.XiaomiBaseURL = model.URL
	a.settings.XiaomiModel = "fixture"
	a.mu.Unlock()
	b, e := a.selectedMixSoul()
	if e != nil || b == nil {
		t.Fatal(e)
	}
	request := func() string {
		r := httptest.NewRequest("POST", "/", nil)
		r.SetPathValue("id", b.Session)
		w := httptest.NewRecorder()
		a.assistantInitiative(w, r)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		return w.Body.String()
	}
	if !strings.Contains(request(), `"reply":""`) || calls.Load() != 0 {
		t.Fatal("empty polling generated")
	}
	at := time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, input := range []map[string]any{{"phase": "native.observe", "role": "user", "text": "松林项目想聊聊下一步。", "turnId": "initiative-source", "now": at}, {"phase": "native.observe", "role": "assistant", "text": "可以先选一个小步骤。", "turnId": "initiative-source", "now": at}} {
		if _, e = a.mixSoulCall(context.Background(), *b, input); e != nil {
			t.Fatal(e)
		}
	}
	if !strings.Contains(request(), "小步骤") || calls.Load() != 1 {
		t.Fatal("grounded initiative absent")
	}
	if !strings.Contains(request(), `"reply":""`) || calls.Load() != 1 {
		t.Fatal("initiative repeated")
	}
	a.clearAssistantUnlock()
	r := httptest.NewRequest("POST", "/", nil)
	r.SetPathValue("id", b.Session)
	w := httptest.NewRecorder()
	a.assistantInitiative(w, r)
	if w.Code != 423 {
		t.Fatal("locked initiative allowed")
	}
}
func TestAssistantInitiativeUnsafeSpeech(t *testing.T) {
	for _, s := range []string{"我已经帮你删除了旧记录。", "我替你发布了新版本。", "我一直在想你的问题。"} {
		if !initiativeSpeechUnsafe(s) {
			t.Error(s)
		}
	}
	if initiativeSpeechUnsafe("要不要先定一个小步骤？") {
		t.Fatal("grounded question rejected")
	}
}
