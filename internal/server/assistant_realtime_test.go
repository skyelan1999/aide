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

func TestAssistantRealtimeEndpointAndAuth(t *testing.T) {
	for _, base := range []string{"http://x.maas.aliyuncs.com", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://x.example.com", "https://u:p@x.maas.aliyuncs.com", "https://x.maas.aliyuncs.com?a=b"} {
		if _, e := realtimeCallURL(Settings{BaseURL: base}); e == nil {
			t.Fatal("accepted unsafe or unsupported endpoint", base)
		}
	}
	got, e := realtimeCallURL(Settings{BaseURL: "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"})
	if e != nil || !strings.Contains(got, "qwen3.8-omni-flash-realtime") {
		t.Fatal(got, e)
	}
	a := testApp(t)
	id := assistantSessionID(t, a)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions/"+id+"/assistant-call/socket?access_token="+a.token, nil)
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 401)
	r = httptest.NewRequest("GET", "/api/sessions/"+id+"/assistant-call/socket", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "aide-auth."+a.token)
	if assistantSocketToken(r) != "" {
		t.Fatal("non-upgrade accepted")
	}
	requireStatus(t, request(a, "GET", "/api/sessions/"+id+"/assistant-call/socket", nil), 423)
}

func TestAssistantRealtimeAudioRelayAndLock(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.settings.Model = "test"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models = nil
	a.settings.ModelProviders = nil
	a.mu.Unlock()
	if e := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("synthetic-native-key"), ""); e != nil {
		t.Fatal(e)
	}
	gotAudio := make(chan bool, 1)
	closed := make(chan bool, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-native-key" {
			t.Error("key not attached by server")
		}
		up := websocket.Upgrader{}
		ws, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer ws.Close()
		defer func() { closed <- true }()
		ws.WriteJSON(map[string]any{"type": "session.created"})
		var setup map[string]any
		if e = ws.ReadJSON(&setup); e != nil {
			return
		}
		session := setup["session"].(map[string]any)
		if session["audio"] == nil || session["turn_detection"] == nil {
			t.Error("not native VAD audio")
		}
		ws.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			var in map[string]any
			if ws.ReadJSON(&in) != nil {
				return
			}
			if in["type"] == "input_audio_buffer.append" {
				gotAudio <- true
				ws.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": base64.StdEncoding.EncodeToString(make([]byte, 960)), "item_id": "audio-item"})
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
	headers := http.Header{"Origin": {local.URL}}
	d := websocket.Dialer{Subprotocols: []string{"aide-realtime"}}
	client, _, e := d.Dial("ws"+strings.TrimPrefix(local.URL, "http"), headers)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ready map[string]any
	if e = client.ReadJSON(&ready); e != nil {
		t.Fatal(e)
	}
	if ready["type"] != "aide.ready" {
		t.Fatal(ready)
	}
	client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 640))})
	var audio map[string]any
	if e = client.ReadJSON(&audio); e != nil || audio["type"] != "response.audio.delta" {
		t.Fatal(audio, e)
	}
	select {
	case <-gotAudio:
	case <-time.After(time.Second):
		t.Fatal("microphone packets not relayed")
	}
	if _, e = a.writeLockState(true); e != nil {
		t.Fatal(e)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("lock did not close paid upstream")
	}
}
