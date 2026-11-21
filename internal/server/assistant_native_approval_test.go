package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNativeApprovalUsesOneCurrentSessionQuestion(t *testing.T) {
	for _, phrase := range []string{"查看 #12 的审批", "查看会话 12 的审批", "12 号会话有什么待审批"} {
		if got := nativeApprovalTarget(phrase); got != "#12" {
			t.Fatalf("could not resolve %q: %q", phrase, got)
		}
	}
	if nativeApprovalTarget("查看 #12 和 #13 的审批") != "" || nativeApprovalDecision("我没有说批准本次审批") != "" {
		t.Fatal("accepted ambiguous voice instructions")
	}
	for _, changed := range []string{"", "round", "workspace", "expired", "lock"} {
		t.Run("change="+changed, func(t *testing.T) {
			a, session, run := issue108Pending(t, "shell", "confirm")
			session.Number = 12
			id := assistantSessionID(t, a)
			unlockAssistantSessionForTest(t, a)
			offer, reply := a.nativeApprovalForSession(a.wsID(), "#12")
			if offer == nil || !strings.Contains(reply, "会话 #12") || offer.detail != "mkdir owned-fixture-output" || len(run.AnswerCh) != 0 {
				t.Fatalf("offer=%+v reply=%q", offer, reply)
			}
			switch changed {
			case "round":
				run.answerRound++
			case "workspace":
				a.wsRevision++
			case "expired":
				offer.expires = time.Now().Add(-time.Second)
			case "lock":
				a.clearAssistantUnlock()
			}
			result := a.submitNativeApproval(context.Background(), id, offer, nativeApprovalDecision("批准本次审批。"))
			if changed != "" {
				if len(run.AnswerCh) != 0 || strings.Contains(result, "已向") {
					t.Fatal("stale request was approved", result)
				}
				return
			}
			if len(run.AnswerCh) != 1 || !strings.Contains(result, "已向会话 #12") || len(run.ApprovalReviews) != 1 || run.ApprovalReviews[0].Source != "human" {
				t.Fatal("approval did not use the standard human channel", result)
			}
			if got := <-run.AnswerCh; got != "确认" {
				t.Fatal("wrong approval answer", got)
			}
			if replay := a.submitNativeApproval(context.Background(), id, offer, "确认"); strings.Contains(replay, "已向") || len(run.AnswerCh) != 0 {
				t.Fatal("approval could be replayed", replay)
			}
		})
	}
}

func TestNativeApprovalFileProposalIsVersionBound(t *testing.T) {
	for _, action := range []string{"确认", "拒绝", "proposal-changed"} {
		t.Run(action, func(t *testing.T) {
			a, session, run := issue108Pending(t, "shell", "confirm")
			session.Number = 12
			run.Status = "awaiting_approval"
			run.Files = []Change{{Path: "approval-fixture.txt", Content: "verified proposal"}}
			id := assistantSessionID(t, a)
			unlockAssistantSessionForTest(t, a)
			offer, reply := a.nativeApprovalForSession(a.wsID(), "#12")
			if offer == nil || !strings.Contains(offer.detail, "verified proposal") {
				t.Fatal("missing file preview", reply)
			}
			answer := action
			if action == "proposal-changed" {
				run.Files[0].Content = "changed after review"
				answer = "确认"
			}
			result := a.submitNativeApproval(context.Background(), id, offer, answer)
			contents, err := a.workspace.ReadFile(run.Files[0].Path)
			if action == "确认" {
				if err != nil || string(contents) != "verified proposal" || !strings.Contains(result, "已向会话") {
					t.Fatal("approved file proposal was not applied", result, err)
				}
				return
			}
			if !os.IsNotExist(err) {
				t.Fatal("rejected or stale proposal wrote to the workspace", result, err)
			}
			if action == "拒绝" && (len(run.ApprovalReviews) != 1 || run.ApprovalReviews[0].Status != "declined") {
				t.Fatal("rejection did not leave a human receipt")
			}
		})
	}
}

func TestNativeApprovalWaitsForSameTurnTranscriptionAndOpensFromHistory(t *testing.T) {
	a, session, run := issue108Pending(t, "shell", "confirm")
	session.Number = 12
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.settings.TTSProvider = "openai"
	a.settings.OpenAITTSMode = "realtime"
	a.settings.OpenAITTSModel = "gpt-realtime-2.1-mini"
	a.settings.OpenAITTSAPIKey = "fixture"
	a.sessions[id].Messages = []Message{{Role: "user", Content: "聊聊桂花乌龙"}, {Role: "user", Content: "我的密码是 fixture-private"}}
	a.mu.Unlock()
	outputs := make(chan string, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		send := func(event map[string]any) { _ = ws.WriteJSON(event) }
		read := func() map[string]any {
			var event map[string]any
			_ = ws.ReadJSON(&event)
			return event
		}
		send(map[string]any{"type": "session.created"})
		if read()["type"] != "session.update" {
			t.Error("missing session setup")
			return
		}
		send(map[string]any{"type": "session.updated"})
		event := read()
		opening, _ := event["response"].(map[string]any)
		text, _ := opening["instructions"].(string)
		if event["type"] != "response.create" || opening["tool_choice"] != "none" || !strings.Contains(text, "桂花乌龙") || strings.Contains(text, "fixture-private") {
			t.Error("expected a history-based opening without tools or private credentials")
			return
		}
		send(map[string]any{"type": "response.created", "response": map[string]any{"id": "opening"}})
		send(map[string]any{"type": "response.done", "response": map[string]any{"id": "opening", "status": "completed"}})
		for _, turn := range []struct{ id, transcript string }{{"offer", "查看会话 12 的审批。"}, {"decision", "批准本次审批。"}} {
			if read()["type"] != "input_audio_buffer.append" {
				t.Error("missing synthetic microphone frame")
				return
			}
			send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": turn.id})
			send(map[string]any{"type": "response.created", "response": map[string]any{"id": turn.id}})
			send(map[string]any{"type": "response.function_call_arguments.done", "response_id": turn.id, "call_id": "call-" + turn.id, "name": "assistant_work"})
			send(map[string]any{"type": "response.done", "response": map[string]any{"id": turn.id, "status": "completed"}})
			send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": turn.id, "transcript": turn.transcript})
			item, _ := read()["item"].(map[string]any)
			output, _ := item["output"].(string)
			outputs <- output
			if read()["type"] != "response.create" {
				t.Error("missing one tool-output response")
				return
			}
		}
	}))
	defer provider.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
		})
	}))
	defer local.Close()
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
	for want := range 2 {
		if want == 0 {
			var event map[string]any
			if err := client.ReadJSON(&event); err != nil || event["type"] != "aide.ready" {
				t.Fatal("call never became ready", err)
			}
		}
		if err := client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 960))}); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-outputs:
			if want == 0 && (!strings.Contains(result, "第 7 轮命令") || len(run.AnswerCh) != 0) {
				t.Fatal("offer executed or named the wrong approval", result)
			}
			if want == 1 && (!strings.Contains(result, "已向会话 #12") || len(run.AnswerCh) != 1) {
				t.Fatal("confirmed approval did not execute", result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("tool did not wait for the matching transcript")
		}
	}
}
