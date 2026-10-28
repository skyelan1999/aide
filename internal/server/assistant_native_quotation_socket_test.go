package server

import (
	"context"
	"encoding/base64"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMixSoulNativeQuotationLateASRRejectsShortenedAudioBeforePlayback(t *testing.T) {
	for _, retryExact := range []bool{true, false} {
		t.Run(map[bool]string{true: "exact-retry", false: "fail-closed"}[retryExact], func(t *testing.T) {
			t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
			a := testApp(t)
			unlockAssistantSessionForTest(t, a)
			id := assistantSessionID(t, a)
			b, e := a.selectedMixSoul()
			if e != nil {
				t.Fatal(e)
			}
			original := "拿到新窗口后，再评估手头材料的准备节奏。"
			short := "拿到新窗口后，再评估材料准备节奏。"
			now := time.Now().UTC().Format(time.RFC3339Nano)
			for _, input := range []map[string]any{{"phase": "native.observe", "role": "user", "callId": "seed", "turnId": "seed", "text": "松鹤项目我今天很焦虑，评审延期了", "now": now}, {"phase": "native.observe", "role": "assistant", "callId": "seed", "turnId": "seed", "text": original, "proactive": true, "now": now}} {
				if _, e = a.mixSoulCall(context.Background(), *b, input); e != nil {
					t.Fatal(e)
				}
			}
			a.mu.Lock()
			a.settings.Model = "fixture"
			a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
			a.settings.Models = []ModelRef{{ID: "fixture"}}
			a.settings.ModelProviders = nil
			a.mu.Unlock()
			if e = a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("synthetic-key"), ""); e != nil {
				t.Fatal(e)
			}
			badAudio := base64.StdEncoding.EncodeToString([]byte{1, 0, 1, 0})
			goodAudio := base64.StdEncoding.EncodeToString([]byte{2, 0, 2, 0})
			instructions := make(chan string, 1)
			restored := make(chan string, 1)
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
				// Behave like the documented Omni API: only session.update can
				// change instructions; response.create overrides are ignored.
				appliedInstructions := ""
				nextTurn := false
				send := func(id, text, audio string) {
					for _, event := range []map[string]any{{"type": "response.created", "response": map[string]any{"id": id}}, {"type": "response.audio.delta", "response_id": id, "item_id": id, "delta": audio}, {"type": "response.audio_transcript.done", "response_id": id, "transcript": text}, {"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}}} {
						ws.WriteJSON(event)
					}
				}
				for {
					var event map[string]any
					if ws.ReadJSON(&event) != nil {
						return
					}
					if !sent && event["type"] == "input_audio_buffer.append" {
						sent = true
						ws.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "q"})
						send("bad", short, badAudio)
						ws.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "q", "transcript": "不要保存本轮内容。松鹤项目，上次你主动补充的原话是什么？"})
					} else if event["type"] == "input_audio_buffer.append" {
						nextTurn = true
						ws.WriteJSON(map[string]any{"type": "input_audio_buffer.speech_started"})
					} else if event["type"] == "session.update" {
						if session, ok := event["session"].(map[string]any); ok {
							appliedInstructions, _ = session["instructions"].(string)
						}
						if nextTurn {
							restored <- appliedInstructions
						}
					} else if event["type"] == "response.create" {
						if _, unsupported := event["response"]; unsupported {
							t.Error("retry used unsupported response overrides")
						}
						instructions <- appliedInstructions
						if retryExact {
							send("good", "原话是“"+original+"”当时你亲口说，因为评审延期了，所以很焦虑。", goodAudio)
						} else {
							send("still-bad", short, badAudio)
						}
					}
				}
			}))
			defer provider.Close()
			dial := func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
				return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
			}
			handlerDone := make(chan struct{})
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				r.SetPathValue("id", id)
				a.assistantRealtimeSocketWithDial(w, r, dial)
			}))
			defer local.Close()
			client, _, e := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				client.Close()
				select {
				case <-handlerDone:
				case <-time.After(5 * time.Second):
					t.Error("native handler did not finish persistence")
				}
			}()
			client.SetReadDeadline(time.Now().Add(15 * time.Second))
			var event map[string]any
			if client.ReadJSON(&event) != nil {
				t.Fatal("not ready")
			}
			client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": "AAAAAA=="})
			gotAudio := false
			gotResult := false
			for !gotResult {
				if e = client.ReadJSON(&event); e != nil {
					t.Fatal(e)
				}
				if event["type"] == "response.audio.delta" {
					if event["delta"] == badAudio {
						t.Fatal("shortened audio escaped")
					}
					gotAudio = true
				}
				if event["type"] == "response.audio_transcript.done" {
					if !retryExact || !strings.Contains(event["transcript"].(string), original) {
						t.Fatal("wrong quote released")
					}
					gotResult = true
				}
				if event["type"] == "aide.error" {
					if retryExact {
						t.Fatal("exact retry rejected")
					}
					gotResult = true
				}
			}
			if gotAudio != retryExact {
				t.Fatal("unexpected audio release")
			}
			select {
			case s := <-instructions:
				if !strings.Contains(s, original) || !strings.Contains(s, "一字不改") {
					t.Fatal("retry did not carry grounded literal target")
				}
			case <-time.After(time.Second):
				t.Fatal("missing bounded retry")
			}
			if retryExact {
				client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": goodAudio})
				select {
				case s := <-restored:
					if strings.Contains(s, "一字不改地朗读") || strings.Contains(s, "不能缩句") {
						t.Fatal("old replay format leaked into next input before ASR")
					}
				case <-time.After(time.Second):
					t.Fatal("next speech did not restore ordinary context")
				}
			}
		})
	}
}
