package server

import (
	"context"
	"encoding/base64"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssistantRealtimeSlowToolBargeInDoesNotBlockOrPostStaleOutput(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.settings.Model = "fixture"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models, a.settings.ModelProviders = nil, nil
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("fixture"), ""); err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	work := func(ctx context.Context, _, _, _ string) string {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(canceled)
		return "stale-result-must-not-reach-provider"
	}
	audio := base64.StdEncoding.EncodeToString([]byte{1, 2})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		send := func(v map[string]any) { _ = ws.WriteJSON(v) }
		send(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		send(map[string]any{"type": "session.updated"})
		turn := func(id string) {
			send(map[string]any{"type": "input_audio_buffer.speech_started"})
			send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": id})
			send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": id, "transcript": "你好"})
		}
		turn("one")
		send(map[string]any{"type": "response.created", "response": map[string]any{"id": "slow"}})
		send(map[string]any{"type": "response.function_call_arguments.done", "response_id": "slow", "name": "assistant_work", "call_id": "call_slow"})
		send(map[string]any{"type": "response.done", "response": map[string]any{"id": "slow", "status": "completed"}})
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Error("helper never started")
			return
		}
		turn("two")
		select {
		case <-canceled:
		case <-time.After(2 * time.Second):
			t.Error("VAD blocked behind slow helper")
			return
		}
		send(map[string]any{"type": "response.created", "response": map[string]any{"id": "next"}})
		send(map[string]any{"type": "response.audio.delta", "response_id": "next", "item_id": "next", "delta": audio})
		send(map[string]any{"type": "response.audio_transcript.done", "response_id": "next", "transcript": "嗯，你接着说。"})
		send(map[string]any{"type": "response.done", "response": map[string]any{"id": "next", "status": "completed"}})
		for {
			var e map[string]any
			if ws.ReadJSON(&e) != nil {
				return
			}
			if e["type"] == "conversation.item.create" {
				t.Error("stale tool output posted after new speech")
			}
		}
	}))
	defer provider.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithWork(w, r, func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
		}, work)
	}))
	defer local.Close()
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var e map[string]any
		if err = client.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if e["type"] == "response.audio.delta" {
			if e["delta"] != audio {
				t.Fatal(e)
			}
			break
		}
	}
	if calls.Load() != 1 {
		t.Fatal("helper replayed", calls.Load())
	}
}

func TestNativeExpiredCallRecoveryOnlyAcceptsExactLocallySentOutput(t *testing.T) {
	sent := map[string]bool{"call_known": true}
	for _, e := range [][2]string{{"invalid_request_error", "Unknown function call id: call_other"}, {"server_error", "Unknown function call id: call_known"}, {"invalid_request_error", "Unknown function call id: call_known and account disabled"}, {"invalid_request_error", "Quota exceeded"}} {
		if nativeKnownExpiredTool(e[0], e[1], sent) {
			t.Fatal("unrelated error hidden", e)
		}
	}
	if !nativeKnownExpiredTool("invalid_request_error", "Unknown function call id: call_known", sent) {
		t.Fatal("confirmed race not recovered")
	}
	if nativeKnownExpiredTool("invalid_request_error", "Unknown function call id: call_known", sent) {
		t.Fatal("unbounded error suppression")
	}
}
