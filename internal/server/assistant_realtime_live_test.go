package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: uses a read-only mount of local credentials for one short
// synthetic provider probe. No microphone, user history or workspace is sent.
func TestAssistantRealtimeLiveSyntheticAudio(t *testing.T) {
	data := os.Getenv("AIDE_NATIVE_LIVE_DATA")
	if data == "" {
		t.Skip("opt-in paid provider probe")
	}
	a := testApp(t)
	b, e := os.ReadFile(filepath.Join(data, "config", "settings.json"))
	if e != nil {
		t.Fatal("settings unavailable")
	}
	var cfg Settings
	if json.Unmarshal(b, &cfg) != nil {
		t.Fatal("settings format")
	}
	token, e := os.ReadFile(filepath.Join(data, "auth", "access-token"))
	if e != nil {
		t.Fatal("local token unavailable")
	}
	v, e := newSecretVault(data)
	if e != nil {
		t.Fatal("vault unavailable")
	}
	if v.Load() != nil {
		t.Fatal("vault load")
	}
	key := sha256.Sum256([]byte(strings.TrimSpace(string(token))))
	v.Unlock(key[:])
	a.vault = v
	a.settings = cfg
	cfg, e = a.realtimeCallConfigLocked()
	if e != nil {
		t.Fatal("native provider configuration unavailable")
	}
	u, e := realtimeCallURL(cfg)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ws, resp, e := websocket.DefaultDialer.DialContext(ctx, u, http.Header{"Authorization": {"Bearer " + cfg.APIKey}})
	if e != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("provider handshake failed HTTP %d", code)
	}
	defer ws.Close()
	audioBytes := 0
	var firstPCM []byte
	round := 0
	transcribed := false
	feeding := false
	ready := false
	ws.SetReadDeadline(time.Now().Add(40 * time.Second))
	for {
		var event map[string]json.RawMessage
		if ws.ReadJSON(&event) != nil {
			t.Fatal("provider stream ended before audio response")
		}
		var kind string
		json.Unmarshal(event["type"], &kind)
		switch kind {
		case "session.created":
			session := realtimeCallSession(cfg)
			if round == 0 {
				session["turn_detection"] = nil
			}
			ws.WriteJSON(map[string]any{"type": "session.update", "session": session})
		case "session.updated":
			ready = true
			if round == 1 {
				if feeding {
					continue
				}
				feeding = true
				ws.WriteJSON(map[string]any{"type": "input_audio_buffer.clear"})
				// Resample the provider's own synthetic greeting from 24k to 16k.
				samples := len(firstPCM) / 2
				pcm := make([]byte, int(float64(samples)/1.5)*2)
				for i := 0; i < len(pcm)/2; i++ {
					j := i * 3 / 2
					binary.LittleEndian.PutUint16(pcm[i*2:], binary.LittleEndian.Uint16(firstPCM[j*2:]))
				}
				pcm = append(pcm, make([]byte, 32000)...)
				for offset := 0; offset < len(pcm); offset += 640 {
					end := offset + 640
					if end > len(pcm) {
						end = len(pcm)
					}
					ws.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm[offset:end])})
					time.Sleep(20 * time.Millisecond)
				}
				continue
			}
			ws.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(make([]byte, 3200))})
			ws.WriteJSON(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "仅用于实时音频连通测试，请只说：你好，实时通话已接通。不要调用工具。"}}}})
			ws.WriteJSON(map[string]any{"type": "response.create"})
		case "error":
			var errInfo struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			json.Unmarshal(event["error"], &errInfo)
			t.Fatalf("provider error code=%s message=%s", errInfo.Code, strings.ReplaceAll(errInfo.Message, cfg.APIKey, "[redacted]"))
		case "response.audio.delta":
			var delta string
			json.Unmarshal(event["delta"], &delta)
			pcm, _ := base64.StdEncoding.DecodeString(delta)
			audioBytes += len(pcm)
			if round == 0 {
				firstPCM = append(firstPCM, pcm...)
			}
		case "conversation.item.input_audio_transcription.completed":
			var transcript string
			json.Unmarshal(event["transcript"], &transcript)
			transcribed = strings.TrimSpace(transcript) != ""
		case "response.done":
			if !ready || audioBytes == 0 {
				t.Fatal("provider did not return native PCM")
			}
			if round == 0 {
				round = 1
				audioBytes = 0
				ws.Close()
				ws, resp, e = websocket.DefaultDialer.DialContext(ctx, u, http.Header{"Authorization": {"Bearer " + cfg.APIKey}})
				if e != nil {
					t.Fatal("second synthetic audio connection failed")
				}
				defer ws.Close()
				ws.SetReadDeadline(time.Now().Add(30 * time.Second))
				continue
			}
			if !transcribed {
				t.Fatal("synthetic spoken input was not transcribed")
			}
			t.Logf("real provider: native PCM output %d bytes; synthetic speech input transcribed and semantic VAD triggered response; no human microphone tested", audioBytes)
			return
		}
	}
}
