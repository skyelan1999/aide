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

func nativeOpenAIFixture() Settings {
	return Settings{TTSProvider: "openai", OpenAITTSMode: "realtime", OpenAITTSModel: "gpt-realtime-2.1-mini", OpenAITTSVoice: "cedar", OpenAITTSAPIKey: "fixture-openai-key", OpenAITTSBaseURL: "https://api.openai.com/v1"}
}

func TestAssistantOpenAINativeConfig(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings = nativeOpenAIFixture()
	a.settings.APIKey = "wrong-chat-key"
	a.settings.TTSAPIKey = "wrong-qwen-key"
	cfg, err := a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err != nil || cfg.APIKey != "fixture-openai-key" {
		t.Fatalf("independent key not selected: %v", err)
	}
	u, err := realtimeCallURL(cfg)
	if err != nil || u != "wss://api.openai.com/v1/realtime?model=gpt-realtime-2.1-mini" {
		t.Fatalf("wrong URL %q %v", u, err)
	}
	meta := realtimeCallMetadata(cfg)
	if meta["provider"] != "openai" || meta["voice"] != "cedar" || meta["inputRate"] != 24000 {
		t.Fatal("wrong metadata", meta)
	}
	for _, kind := range []string{"missing-key", "speech-mode", "unsafe-url", "http-url"} {
		c := nativeOpenAIFixture()
		switch kind {
		case "missing-key":
			c.OpenAITTSAPIKey = ""
		case "speech-mode":
			c.OpenAITTSMode = "speech"
		case "unsafe-url":
			c.OpenAITTSBaseURL = "https://user:secret@example.com/v1"
		case "http-url":
			c.OpenAITTSBaseURL = "http://example.com/v1"
		}
		a.mu.Lock()
		a.settings = c
		a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
		a.settings.APIKey = "wrong-qwen-key"
		_, err = a.realtimeCallConfigLocked()
		a.mu.Unlock()
		if err == nil {
			t.Errorf("silently accepted/fell back: %s", kind)
		}
	}
}

func TestAssistantOpenAINativeSessionAndEvents(t *testing.T) {
	cfg := nativeOpenAIFixture()
	s := realtimeCallSession(cfg)
	if s["type"] != "realtime" || s["modalities"] != nil || s["turn_detection"] != nil {
		t.Fatal("legacy session fields")
	}
	audio := s["audio"].(map[string]any)
	input := audio["input"].(map[string]any)
	if input["format"].(map[string]any)["rate"] != 24000 || audio["output"].(map[string]any)["voice"] != "cedar" || s["tools"] == nil {
		t.Fatal("wrong GA audio/tool configuration")
	}
	e := map[string]any{"type": "session.update", "session": map[string]any{"instructions": "updated"}}
	wire := realtimeCallWireEvent(cfg, e)
	if wire["session"].(map[string]any)["type"] != "realtime" || e["session"].(map[string]any)["type"] != nil {
		t.Fatal("wire conversion mutated shared session")
	}
	for from, to := range map[string]string{"response.output_audio.delta": "response.audio.delta", "response.output_audio.done": "response.audio.done", "response.output_audio_transcript.delta": "response.audio_transcript.delta", "response.output_audio_transcript.done": "response.audio_transcript.done", "response.function_call_arguments.done": "response.function_call_arguments.done"} {
		b, _ := json.Marshal(from)
		event := map[string]json.RawMessage{"type": b, "item_id": json.RawMessage(`"item"`)}
		realtimeCallNormalizeEvent(cfg, event)
		var kind string
		_ = json.Unmarshal(event["type"], &kind)
		if kind != to || string(event["item_id"]) != `"item"` {
			t.Fatal("lost event identity")
		}
	}
}

func TestAssistantOpenAIRealtimeRelayAndLock(t *testing.T) {
	for _, change := range []string{"lock", "voice-change"} {
		t.Run(change, func(t *testing.T) { testOpenAIRealtimeRelayClose(t, change) })
	}
}

func testOpenAIRealtimeRelayClose(t *testing.T, change string) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.settings.TTSProvider = "openai"
	a.settings.OpenAITTSMode = "realtime"
	a.settings.OpenAITTSAPIKey = "fixture-openai-key"
	a.settings.OpenAITTSVoice = "cedar"
	a.settings.OpenAITTSModel = "gpt-realtime-2.1-mini"
	a.mu.Unlock()
	status := request(a, "GET", "/api/sessions/"+id+"/assistant-call", nil)
	requireStatus(t, status, 200)
	if strings.Contains(status.Body.String(), "fixture-openai-key") || !strings.Contains(status.Body.String(), `"provider":"openai"`) {
		t.Fatal("unsafe/wrong status")
	}
	closed := make(chan struct{})
	gotAudio := make(chan int, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		if r.Header.Get("Authorization") != "Bearer fixture-openai-key" {
			t.Error("wrong provider credential")
		}
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteJSON(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		session := setup["session"].(map[string]any)
		if session["type"] != "realtime" || session["audio"].(map[string]any)["output"].(map[string]any)["voice"] != "cedar" {
			t.Error("not selected OpenAI session")
		}
		_ = ws.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			var event map[string]any
			if ws.ReadJSON(&event) != nil {
				return
			}
			if event["type"] == "input_audio_buffer.append" {
				pcm, _ := base64.StdEncoding.DecodeString(event["audio"].(string))
				gotAudio <- len(pcm)
				_ = ws.WriteJSON(map[string]any{"type": "response.output_audio.delta", "response_id": "response-fixture", "item_id": "item-fixture", "delta": base64.StdEncoding.EncodeToString(make([]byte, 960))})
			}
		}
	}))
	defer provider.Close()
	dial := func(ctx context.Context, u string, h http.Header) (*websocket.Conn, *http.Response, error) {
		if !strings.HasPrefix(u, "wss://api.openai.com/v1/realtime?") || !strings.Contains(u, "gpt-realtime-2.1-mini") {
			t.Error("Qwen URL selected")
		}
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, dial)
	}))
	defer local.Close()
	client, _, err := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ready map[string]any
	if client.ReadJSON(&ready) != nil || ready["type"] != "aide.ready" || ready["provider"] != "openai" || ready["voice"] != "cedar" || ready["inputRate"] != float64(24000) {
		t.Fatal("wrong actual call identity", ready)
	}
	_ = client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 960))})
	var audio map[string]any
	if client.ReadJSON(&audio) != nil || audio["type"] != "response.audio.delta" || audio["item_id"] != "item-fixture" {
		t.Fatal("GA audio not relayed", audio)
	}
	select {
	case n := <-gotAudio:
		if n != 960 {
			t.Fatal("24k audio packet changed")
		}
	case <-time.After(time.Second):
		t.Fatal("missing microphone packet")
	}
	if change == "voice-change" {
		a.mu.Lock()
		a.settings.OpenAITTSVoice = "marin"
		a.mu.Unlock()
	} else {
		if _, err = a.writeLockState(true); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("lock did not close paid OpenAI connection")
	}
}
