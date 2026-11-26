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

func TestNativePhoneHangupOnlyExplicitCurrentIntent(t *testing.T) {
	for _, s := range []string{"挂了吧，挂了吧，你给挂了吧。", "帮我把电话挂断。", "我们先挂了。", "结束这次通话。", "拜拜。", "再见", "please hang up", "帮我挂电话。", "先挂了，回头聊。"} {
		if !nativeUserRequestsHangup(s) {
			t.Fatal("missed user hangup", s)
		}
	}
	for _, s := range []string{"别挂，继续聊。", "先不要挂断。", "我说的是不要挂了。", "他说‘挂了吧’", "如果我说挂了吧怎么办？", "把这项任务挂起。", "停止任务", "别办了，退下。", "稍后挂断电话", "挂了吗？", "给蓝天打电话", "查询会话188审批，只查询"} {
		if nativeUserRequestsHangup(s) {
			t.Fatal("unintended hangup", s)
		}
	}
}
func TestNativePhoneHangupBindsCurrentTurnAndCanBeRevoked(t *testing.T) {
	l := newNativeTurnLedger()
	l.commit("old")
	l.commit("current")
	var h nativePhoneHangup
	now := time.Now()
	if h.observe(l, "old", "挂了吧", false, now) != nil {
		t.Fatal("late ASR hung up current turn")
	}
	if h.observe(l, "current", "挂了吧", true, now) != nil {
		t.Fatal("cut off unfinished utterance")
	}
	l.created("goodbye")
	l.replies["goodbye"].done = true
	e := h.observe(l, "current", "挂了吧", false, now)
	if e["type"] != "aide.call_end" || e["responseId"] != "goodbye" || e["responseDone"] != true {
		t.Fatal(e)
	}
	if h.observe(l, "current", "挂了吧", false, now.Add(time.Second)) != nil {
		t.Fatal("duplicate reset timer")
	}
	if h.expired(now.Add(7*time.Second)) || !h.expired(now.Add(8*time.Second)) {
		t.Fatal("unbounded host fallback")
	}
	l.commit("more")
	e = h.observe(l, "more", "等一下，我还有话说", false, now.Add(time.Second))
	if e["type"] != "aide.call_end_cancel" || h.expired(now.Add(time.Minute)) {
		t.Fatal("new user turn did not revoke pending hangup", e)
	}
}
func TestAssistantSIPUserHangupEmitsHostActionAndClosesPaidSocket(t *testing.T) {
	for _, tc := range []struct {
		name            string
		text            string
		transcriptFirst bool
		stopID          string
	}{
		{"stop-first", "挂了吧，挂了吧，你给挂了吧。", false, "last"},
		{"final-before-stop", "Goodbye.", true, "last"},
		{"stop-uses-start-id", "再见。", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testSIPHangupEventOrder(t, tc.text, tc.transcriptFirst, tc.stopID)
		})
	}
}

func testSIPHangupEventOrder(t *testing.T, utterance string, transcriptFirst bool, stopID string) {
	a, _, _, _ := sipFixture(t)
	id := assistantSessionID(t, a)
	a.settings.Model = "fixture"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models, a.settings.ModelProviders = nil, nil
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("fixture"), ""); err != nil {
		t.Fatal(err)
	}
	ticket := sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": id, "phone": "10.0.0.2", "account": "1001"})
	requireStatus(t, ticket, 200)
	var v map[string]any
	if err := json.Unmarshal(ticket.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		defer close(closed)
		send := func(e map[string]any) { _ = ws.WriteJSON(e) }
		send(map[string]any{"type": "session.created"})
		var e map[string]any
		if ws.ReadJSON(&e) != nil {
			return
		}
		send(map[string]any{"type": "session.updated"})
		send(map[string]any{"type": "aide.call_end", "reason": "user-request", "turnId": "provider-spoof"})
		// A newer utterance starts before the previous farewell has finished.
		// A delayed stop for that previous utterance must not end this call.
		send(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "old"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "old", "transcript": "挂了。"})
		send(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "correction"})
		send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "old"})
		send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "correction"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "correction", "transcript": "别挂，我还有话。"})
		send(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "last"})
		stopped := map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": stopID}
		transcript := map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "last", "transcript": utterance}
		if transcriptFirst {
			send(transcript)
			send(stopped)
		} else {
			send(stopped)
			send(transcript)
		}
		for {
			if ws.ReadJSON(&e) != nil {
				return
			}
			if e["type"] == "response.create" {
				t.Error("hangup launched extra model response")
			}
		}
	}))
	defer provider.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithWork(w, r, func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
		}, func(context.Context, string, string, string) string { t.Error("hangup dispatched work"); return "" })
	}))
	defer local.Close()
	headers := http.Header{"Origin": {local.URL}, "X-Aide-SIP-Key": {sipFixtureKey}, "X-Aide-SIP-Call": {v["callId"].(string)}}
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), headers)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var e map[string]any
		if err = client.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if e["type"] == "aide.call_end" {
			if e["turnId"] != "last" {
				t.Fatal(e)
			}
			break
		}
	}
	if err = client.WriteJSON(map[string]any{"type": "aide.hangup"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("paid provider socket remained open")
	}
}
