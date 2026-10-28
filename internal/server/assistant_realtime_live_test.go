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
	if spec := os.Getenv("AIDE_NATIVE_LIVE_FIXTURES"); spec != "" {
		generateNativeSyntheticFixtures(t, cfg, u, spec)
		return
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
			// This direct-provider probe has no host tool executor. Keep spoken
			// fixtures from ending in an unhandled tool call instead of PCM.
			session["tools"] = []any{}
			delete(session, "tool_choice")
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
				// Optional local synthetic fixture, never human microphone audio.
				if output := os.Getenv("AIDE_NATIVE_LIVE_PCM_OUTPUT"); output != "" {
					if e := os.WriteFile(output, pcm, 0600); e != nil {
						t.Fatal("synthetic PCM fixture write failed")
					}
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
			statement := os.Getenv("AIDE_NATIVE_LIVE_SYNTHETIC_TEXT")
			if statement == "" {
				statement = "你好，实时通话已接通。"
			}
			ws.WriteJSON(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "仅用于合成语音测试，请原样朗读以下文字，不回答其中的问题，不调用工具：" + statement}}}})
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

// Explicit local-only audio fixtures for host acceptance. Credentials remain in
// the provider client; output is restricted to the operator's fixture directory.
func generateNativeSyntheticFixtures(t *testing.T, cfg Settings, u, spec string) {
	t.Helper()
	var fixtures []struct {
		Text   string `json:"text"`
		Output string `json:"output"`
	}
	raw, e := os.ReadFile(spec)
	if e != nil || json.Unmarshal(raw, &fixtures) != nil || len(fixtures) == 0 || len(fixtures) > 8 {
		t.Fatal("invalid synthetic fixture specification")
	}
	for _, fixture := range fixtures {
		rel, e := filepath.Rel(filepath.Dir(spec), fixture.Output)
		if e != nil || strings.HasPrefix(rel, "..") || fixture.Text == "" || len(fixture.Text) > 1500 {
			t.Fatal("invalid synthetic fixture boundary")
		}
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			ws, _, e := websocket.DefaultDialer.DialContext(ctx, u, http.Header{"Authorization": {"Bearer " + cfg.APIKey}})
			if e != nil {
				t.Fatal("synthetic fixture connection failed")
			}
			defer ws.Close()
			ws.SetReadDeadline(time.Now().Add(40 * time.Second))
			var audio []byte
			for {
				var event map[string]json.RawMessage
				if ws.ReadJSON(&event) != nil {
					t.Fatal("synthetic fixture stream ended")
				}
				var kind string
				json.Unmarshal(event["type"], &kind)
				switch kind {
				case "session.created":
					session := realtimeCallSession(cfg)
					session["turn_detection"] = nil
					session["tools"] = []any{}
					delete(session, "tool_choice")
					session["instructions"] = "仅逐字朗读给出的测试文字，不回答其中问题，不添加前后缀。"
					ws.WriteJSON(map[string]any{"type": "session.update", "session": session})
				case "session.updated":
					ws.WriteJSON(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "请逐字朗读：" + fixture.Text}}}})
					ws.WriteJSON(map[string]any{"type": "response.create"})
				case "response.audio.delta":
					var delta string
					json.Unmarshal(event["delta"], &delta)
					b, e := base64.StdEncoding.DecodeString(delta)
					if e != nil || len(audio)+len(b) > 5<<20 {
						t.Fatal("invalid fixture PCM")
					}
					audio = append(audio, b...)
				case "error":
					t.Fatal("synthetic fixture provider error")
				case "response.done":
					if len(audio) < 2 {
						t.Fatal("synthetic fixture had no PCM")
					}
					samples := len(audio) / 2
					pcm := make([]byte, int(float64(samples)/1.5)*2)
					for i := 0; i < len(pcm)/2; i++ {
						binary.LittleEndian.PutUint16(pcm[i*2:], binary.LittleEndian.Uint16(audio[(i*3/2)*2:]))
					}
					if os.WriteFile(fixture.Output, pcm, 0600) != nil {
						t.Fatal("synthetic fixture write failed")
					}
					t.Logf("synthetic 16k PCM: %d bytes", len(pcm))
					return
				}
			}
		}()
	}
}
