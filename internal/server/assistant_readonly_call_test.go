package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAssistantReadOnlyCallWithholdsFalseCompletionAudio(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.settings.Model = "fixture"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models, a.settings.ModelProviders = nil, nil
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("fixture"), ""); err != nil {
		t.Fatal(err)
	}
	badAudio := base64.StdEncoding.EncodeToString([]byte{1, 2})
	goodAudio := base64.StdEncoding.EncodeToString([]byte{3, 4})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		send := func(v map[string]any) { _ = ws.WriteJSON(v) }
		send(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		if !strings.Contains(setup["session"].(map[string]any)["instructions"].(string), assistantReadOnlyCallInstructions) {
			t.Error("initial call restriction absent")
		}
		send(map[string]any{"type": "session.updated"})
		send(map[string]any{"type": "input_audio_buffer.speech_started"})
		send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "turn"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "turn", "transcript": "同意审批"})
		reply := func(id, audio, text string) {
			send(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
			send(map[string]any{"type": "response.audio.delta", "response_id": id, "item_id": id, "delta": audio})
			send(map[string]any{"type": "response.audio_transcript.done", "response_id": id, "transcript": text})
			send(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		}
		reply("false", badAudio, "已提交审批请求。")
		for {
			var event map[string]any
			if ws.ReadJSON(&event) != nil {
				return
			}
			if event["type"] == "session.update" && !strings.Contains(event["session"].(map[string]any)["instructions"].(string), assistantReadOnlyCallInstructions) {
				t.Error("repair lost call restriction")
			}
			if event["type"] == "response.create" {
				reply("corrected", goodAudio, assistantReadOnlyCallDenied)
				break
			}
		}
		for {
			var event map[string]any
			if ws.ReadJSON(&event) != nil {
				return
			}
		}
	}))
	defer provider.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, func(ctx context.Context, _ string, headers http.Header) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), headers)
		})
	}))
	defer local.Close()
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http")+"?readOnly=1", http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event map[string]any
		if err := client.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event["type"] == "aide.ready" && event["readOnly"] != true {
			t.Fatal(event)
		}
		if event["type"] == "response.audio_transcript.done" && event["transcript"] == "已提交审批请求。" {
			t.Fatal("false completion was emitted")
		}
		if event["type"] == "response.audio.delta" {
			if event["delta"] != goodAudio {
				t.Fatal("unreviewed audio escaped", event)
			}
			break
		}
	}
}

func TestAssistantReadOnlyCallInstructionsAndClaims(t *testing.T) {
	ctx := context.WithValue(context.Background(), assistantReadOnlyCallKey{}, true)
	for _, key := range []string{"session", "response"} {
		event := map[string]any{key: map[string]any{"instructions": "clock/context/repair"}}
		for i := 0; i < 2; i++ {
			event = assistantReadOnlyCallEvent(ctx, event)
		}
		instructions := event[key].(map[string]any)["instructions"].(string)
		if strings.Count(instructions, assistantReadOnlyCallInstructions) != 1 {
			t.Fatal("scope missing or duplicated", instructions)
		}
	}
	for _, text := range []string{"我已经把你的同意审批请求提交给系统。", "好的，让我先处理一下这个审批请求，然后告诉你结果。", "已提交", "已批准任务"} {
		if !assistantReadOnlyCallNeedsReview(text) {
			t.Fatal("false action claim accepted", text)
		}
	}
	for _, text := range []string{assistantReadOnlyCallDenied, "当前有35项待人工审批。", "一二三四五。", "本次为只读通话，未执行审批或任务操作。", "已经查询了会话188的审批状态，并未批准、拒绝或恢复任何任务。", "已经查到会话188目前没有需要处理的审批请求。", "会话 #188 当前有 0 项待人工审批。本次仅查询，没有更改待审任务。"} {
		if assistantReadOnlyCallNeedsReview(text) {
			t.Fatal("valid read-only reply rejected", text)
		}
	}
}

func TestAssistantReadOnlyCallApprovalReceipt(t *testing.T) {
	a := testApp(t)
	a.sessions["fixture-188"] = &Session{ID: "fixture-188", Number: 188, Runs: []*Task{{ID: "pending", WorkspaceID: a.wsID(), Status: "awaiting_approval", Files: []Change{{Path: "fixture.txt", Content: "PRIVATE"}}}}}
	before, _ := json.Marshal(a.sessions)
	event, result := a.nativeReadOnlyApprovalQuery(a.wsID(), "#188", "model-call-id")
	if event["type"] != "aide.tool_result" || event["callId"] != "model-call-id" || event["pendingCount"] != 1 || event["sessionNumber"] != 188 || !strings.Contains(result, "1 项") {
		t.Fatal(event, result)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(result, "PRIVATE") {
		t.Fatal("receipt leaked proposal content")
	}
	after, _ := json.Marshal(a.sessions)
	if string(before) != string(after) {
		t.Fatal("query mutated pending proposal")
	}
	event, _ = a.nativeReadOnlyApprovalQuery("other-workspace", "#188", "other-call")
	if event["status"] != "not_found" {
		t.Fatal("workspace boundary escaped", event)
	}
	a.sessions["fixture-188"].Runs = nil
	event, result = a.nativeReadOnlyApprovalQuery(a.wsID(), "#188", "empty-call")
	if event["pendingCount"] != 0 || !strings.Contains(result, "0 项") {
		t.Fatal(event, result)
	}
}

func TestAssistantReadOnlyCallBlocksMutations(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(context.Background(), assistantReadOnlyCallKey{}, true)
	before, _ := json.Marshal(a.sessions)
	for _, name := range []string{"dispatch_to_aide", "control_session_run", "remember", "memory_notes", "manage_messages", "clear_followups", "manage_plugins", "unknown"} {
		call := ToolCall{}
		call.Function.Name = name
		call.Function.Arguments = `{"summary":"fixture","note":"fixture"}`
		dec := assistantDecision{Action: "chat"}
		if got := a.execAssistantToolWithContext(ctx, call, &dec, "批准并继续会话188"); got != assistantReadOnlyCallDenied || dec.Action != "chat" {
			t.Fatalf("%s escaped read-only guard: %s %+v", name, got, dec)
		}
	}
	if got := a.submitNativeApproval(ctx, "", nil, "通过"); got != assistantReadOnlyCallDenied {
		t.Fatal(got)
	}
	if got := a.controlAssistantSessionRun(ctx, "继续会话188"); got != assistantReadOnlyCallDenied {
		t.Fatal(got)
	}
	after, _ := json.Marshal(a.sessions)
	if string(before) != string(after) {
		t.Fatal("read-only calls changed sessions")
	}
}

func TestAssistantReadOnlyCallToolScope(t *testing.T) {
	a := testApp(t)
	tools := a.assistantAgentTools(false)
	if len(assistantCallTools(context.Background(), tools)) != len(tools) {
		t.Fatal("ordinary call tools changed")
	}
	ctx := context.WithValue(context.Background(), assistantReadOnlyCallKey{}, true)
	filtered := assistantCallTools(ctx, tools)
	if len(filtered) == 0 {
		t.Fatal("all read tools removed")
	}
	for _, tool := range filtered {
		schema := tool.(map[string]any)
		name := schema["function"].(map[string]any)["name"].(string)
		if !assistantApprovalReadOnlyTool(name) {
			t.Fatalf("mutation tool exposed: %s", name)
		}
	}
	call := ToolCall{}
	call.Function.Name = "aide_component_status"
	call.Function.Arguments = `{"component":"approvals"}`
	dec := assistantDecision{}
	if got := a.execAssistantToolWithContext(ctx, call, &dec); dec.approvalTotal == nil || *dec.approvalTotal != 0 {
		t.Fatalf("read query failed: %s", got)
	}
}

func TestAssistantAutonomyNativeRequiresWorkTool(t *testing.T) {
	forced := make(chan bool, 1)
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.settings.Model = "fixture"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models, a.settings.ModelProviders = nil, nil
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("fixture"), ""); err != nil {
		t.Fatal(err)
	}
	badAudio := base64.StdEncoding.EncodeToString([]byte{1, 2})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		send := func(v map[string]any) { _ = ws.WriteJSON(v) }
		send(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		if !strings.Contains(setup["session"].(map[string]any)["instructions"].(string), "assistant_work") {
			t.Error("initial call restriction absent")
		}
		send(map[string]any{"type": "session.updated"})
		send(map[string]any{"type": "input_audio_buffer.speech_started"})
		send(map[string]any{"type": "input_audio_buffer.speech_stopped", "item_id": "turn"})
		send(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "turn", "transcript": "今天天气怎么样？"})
		reply := func(id, audio, text string) {
			send(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
			send(map[string]any{"type": "response.audio.delta", "response_id": id, "item_id": id, "delta": audio})
			send(map[string]any{"type": "response.audio_transcript.done", "response_id": id, "transcript": text})
			send(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		}
		reply("false", badAudio, "我没有实时天气数据。")
		repairInstructions := ""
		for {
			var event map[string]any
			if ws.ReadJSON(&event) != nil {
				return
			}
			if event["type"] == "session.update" {
				session, _ := event["session"].(map[string]any)
				repairInstructions, _ = session["instructions"].(string)
			}
			if event["type"] == "response.create" {
				// Qwen requires session instructions and a bare response trigger;
				// OpenAI-style tool_choice is unsupported by this provider.
				_, unsupported := event["response"]
				forced <- !unsupported && strings.Contains(repairInstructions, "现在调用 assistant_work") && strings.Contains(repairInstructions, "工具返回前不要输出事实")
				return
			}
		}
	}))
	defer provider.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, func(ctx context.Context, _ string, headers http.Header) (*websocket.Conn, *http.Response, error) {
			return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), headers)
		})
	}))
	defer local.Close()
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http")+"", http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event map[string]any
		err := client.ReadJSON(&event)
		if event["type"] == "response.audio.delta" {
			t.Fatal("unverified refusal audio escaped", event)
		}
		if err != nil {
			break
		}
	}
	select {
	case ok := <-forced:
		if !ok {
			t.Fatal("retry did not require assistant_work")
		}
	case <-time.After(time.Second):
		t.Fatal("missing work-tool retry")
	}
}
