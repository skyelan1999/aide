package server

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMixSoulNativeLateASRHangupPersistsEvidence(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	id := assistantSessionID(t, a)
	release := make(chan struct{})
	learned := make(chan struct{}, 8)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"items":[]}`}}}})
		learned <- struct{}{}
	}))
	defer model.Close()
	a.mu.Lock()
	a.settings.Model = "fixture"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models = []ModelRef{{ID: "fixture"}}
	a.settings.ModelProviders = nil
	a.settings.XiaomiModelSource = "custom"
	a.settings.XiaomiBaseURL = model.URL
	a.settings.XiaomiModel = "fixture"
	a.mu.Unlock()
	if e := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("synthetic-key"), ""); e != nil {
		t.Fatal(e)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer ws.Close()
		ws.WriteJSON(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		ws.WriteJSON(map[string]any{"type": "session.updated"})
		sent := false
		for {
			var in map[string]any
			if ws.ReadJSON(&in) != nil {
				return
			}
			if sent || in["type"] != "input_audio_buffer.append" {
				continue
			}
			sent = true
			for _, e := range []map[string]any{
				{"type": "input_audio_buffer.speech_stopped", "item_id": "project"},
				{"type": "conversation.item.input_audio_transcription.completed", "item_id": "project", "transcript": "松鹤项目评审延期了"},
				{"type": "input_audio_buffer.speech_stopped", "item_id": "affect"},
				{"type": "response.created", "response": map[string]any{"id": "reply"}},
				{"type": "response.audio_transcript.done", "response_id": "reply", "transcript": "先写下一个卡点。编号：731864"},
				{"type": "response.done", "response": map[string]any{"id": "reply", "status": "completed"}},
				{"type": "conversation.item.input_audio_transcription.completed", "item_id": "affect", "transcript": "我今天很焦虑"},
			} {
				if ws.WriteJSON(e) != nil {
					return
				}
			}
		}
	}))
	defer provider.Close()
	dial := func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, dial)
	}))
	defer local.Close()
	client, _, e := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
	if e != nil {
		close(release)
		t.Fatal(e)
	}
	client.SetReadDeadline(time.Now().Add(20 * time.Second))
	var event map[string]any
	if client.ReadJSON(&event) != nil {
		close(release)
		t.Fatal("ready")
	}
	client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": "AAAAAA=="})
	for {
		if e = client.ReadJSON(&event); e != nil {
			client.Close()
			close(release)
			t.Fatal("late ASR", e)
		}
		if event["type"] == "conversation.item.input_audio_transcription.completed" && event["item_id"] == "affect" {
			break
		}
	}
	client.Close()
	close(release)
	select {
	case <-learned:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted learning canceled by ordinary hangup")
	}
	b, e := a.selectedMixSoul()
	if e != nil {
		t.Fatal(e)
	}
	result, e := a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.recall", "heard": "松鹤项目，之前建议的编号是什么，当时情绪是什么", "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if e != nil || !strings.Contains(result.Instructions, "731864") || !strings.Contains(result.Instructions, "explicit-user-affect") || !strings.Contains(result.Instructions, "supportingStatements") {
		t.Fatal("native evidence lost after late ASR/hangup", e)
	}
}

func TestMixSoulNativeWorkCannotRecallFromVoiceHistory(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.recordAssistantExchangeLocked("松鹤项目的建议", VoiceHistoryEntry{Action: "ignore", Reply: "history-only-731864"}, "call")
	a.mu.Unlock()
	if len(a.recentAssistantConversation()) == 0 {
		t.Fatal("missing history fixture")
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := json.Marshal(payload)
		if strings.Contains(string(raw), "history-only-731864") {
			t.Error("native work bypassed memory via voice history")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "未知"}}}})
	}))
	defer model.Close()
	b, e := a.selectedMixSoul()
	if e != nil {
		t.Fatal(e)
	}
	answer, e := a.runMixSoul(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, *b, "不要保存本轮内容。松鹤项目上次的建议编号是什么？", "call")
	if e != nil || answer.Reply != "未知" {
		t.Fatal("native work history isolation", e)
	}
}
