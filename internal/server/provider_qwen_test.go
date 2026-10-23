package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestQwenMainstreamWire(t *testing.T) {
	for _, model := range []string{"qwen3.8-max", "qwen3.8-flash", "qwen3.7-plus", "qwen3.7-flash", "qwen3.6-plus", "qwen3.6-flash", "qwen3.5-plus", "qwen3.5-flash", "qwen-plus", "qwen-flash", "qwen3-vl-plus"} {
		for _, effort := range []string{"off", "low", "medium", "high"} {
			cfg := Settings{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: model, ReasoningEffort: effort, routedProfile: &routerProfile{WireFormat: "openai-compatible"}}
			body := buildChatBody(cfg, []Message{{Role: "user", Content: "hello"}}, ProfileParams{}, nil, true)
			if _, bad := body["thinking"]; bad {
				t.Fatalf("%s leaked vendor field", model)
			}
			if _, bad := body["reasoning_effort"]; bad {
				t.Fatalf("%s leaked effort", model)
			}
			if body["enable_thinking"] != (effort != "off") {
				t.Fatalf("%s/%s: %v", model, effort, body)
			}
		}
	}
	for _, effort := range []string{"off", "low", "medium", "high"} {
		cfg := Settings{BaseURL: "https://example.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", Model: "qwen3.8-omni-flash", ReasoningEffort: effort}
		body := buildChatBody(cfg, nil, ProfileParams{}, nil, true)
		want := effort
		if want == "off" {
			want = "none"
		}
		if body["reasoning_effort"] != want || body["preserve_thinking"] != false {
			t.Fatalf("omni %v", body)
		}
	}
	for _, model := range []string{"qwen3.8-max", "qwen3.7-plus", "qwen3.6-flash", "qwen3.5-omni-plus"} {
		if !modelSupportsVision(model, nil) {
			t.Fatalf("vision missing: %s", model)
		}
	}
	if modelSupportsVision("qwen3.7-max", nil) || modelSupportsVision("qwen3-coder-flash", nil) {
		t.Fatal("text-only model advertised as vision")
	}
	if modelSupportsVision("qwen3.8-max", []ModelRef{{ID: "qwen3.8-max", Capabilities: map[string]string{"vision": "unsupported"}}}) {
		t.Fatal("explicit vision override ignored")
	}
}

func TestQwenRealtimeTextToolBridge(t *testing.T) {
	upgrade := websocket.Upgrader{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api-ws/v1/realtime" || r.URL.Query().Get("model") != "qwen3.8-omni-flash-realtime" || r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Errorf("realtime routing %s", r.URL.Path)
		}
		ws, err := upgrade.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteJSON(map[string]any{"type": "session.created"})
		var config map[string]any
		_ = ws.ReadJSON(&config)
		if config["type"] != "session.update" {
			t.Errorf("config=%v", config)
		}
		_ = ws.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			var event map[string]any
			if ws.ReadJSON(&event) != nil {
				return
			}
			if event["type"] == "response.create" {
				break
			}
		}
		_ = ws.WriteJSON(map[string]any{"type": "response.text.delta", "delta": "hello"})
		_ = ws.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "completed", "output": []any{map[string]any{"type": "function_call", "call_id": "call-1", "name": "get_time", "arguments": "{}"}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}})
	}))
	defer provider.Close()
	var visible strings.Builder
	var snapshot []byte
	out, calls, usage, finish, err := completeStream(context.Background(), Settings{BaseURL: provider.URL, Model: "qwen3.8-omni-flash-realtime", APIKey: "synthetic"}, []Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}}, ProfileParams{}, nil, func(b []byte) { snapshot = b }, func(s string) { visible.WriteString(s) }, nil)
	if err != nil || out != "hello" || visible.String() != out || len(calls) != 1 || calls[0].Function.Name != "get_time" || usage.Total != 12 || finish != "stop" {
		t.Fatalf("out=%s calls=%v usage=%+v finish=%s err=%v", out, calls, usage, finish, err)
	}
	var recorded map[string]any
	if json.Unmarshal(snapshot, &recorded) != nil || recorded["session"] == nil {
		t.Fatal("missing wire snapshot")
	}
}

func TestQwenRealtimeRequiresWorkspace(t *testing.T) {
	_, _, _, _, err := completeStream(context.Background(), Settings{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.8-omni-flash-realtime"}, nil, ProfileParams{}, nil, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "业务空间") {
		t.Fatalf("missing workspace guidance: %v", err)
	}
}

func TestQwenRealtimeCancellation(t *testing.T) {
	connected := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		close(connected)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, _, _, _, err := completeQwenRealtime(ctx, Settings{BaseURL: provider.URL, Model: "qwen3.8-omni-flash-realtime"}, nil, nil, nil, nil)
		result <- err
	}()
	<-connected
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close socket")
	}
}
