package server

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Paid opt-in. Feed only an operator-owned synthetic WAV into a fresh test
// assistant, through the real authenticated Aide relay. No user mic/history.
func TestAssistantOpenAICallLiveRelay(t *testing.T) {
	data := os.Getenv("AIDE_OPENAI_CALL_LIVE_DATA")
	if data == "" {
		t.Skip("opt-in real OpenAI call relay")
	}
	raw, err := os.ReadFile(filepath.Join(data, "config", "settings.json"))
	if err != nil {
		t.Fatal("review settings unavailable")
	}
	var saved Settings
	if json.Unmarshal(raw, &saved) != nil {
		t.Fatal("review settings invalid")
	}
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.settings.TTSProvider = "openai"
	a.settings.OpenAITTSMode = saved.OpenAITTSMode
	a.settings.OpenAITTSModel = saved.OpenAITTSModel
	a.settings.OpenAITTSAPIKey = saved.OpenAITTSAPIKey
	a.settings.OpenAITTSBaseURL = saved.OpenAITTSBaseURL
	a.settings.OpenAITTSVoice = "cedar"
	a.mu.Unlock()
	wav, err := os.ReadFile(os.Getenv("AIDE_OPENAI_CALL_LIVE_WAV"))
	if err != nil || len(wav) < 44 || string(wav[:4]) != "RIFF" || binary.LittleEndian.Uint32(wav[24:]) != 24000 {
		t.Fatal("24k synthetic WAV required")
	}
	local := httptest.NewServer(a.Handler())
	defer local.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"aide-realtime", "aide-auth." + a.token}, HandshakeTimeout: 10 * time.Second}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(local.URL, "http")+"/api/sessions/"+id+"/assistant-call/socket", http.Header{"Origin": {local.URL}})
	if err != nil {
		t.Fatal("local authenticated call failed")
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(40 * time.Second))
	var event map[string]any
	if client.ReadJSON(&event) != nil || event["type"] != "aide.ready" || event["provider"] != "openai" || event["voice"] != "cedar" || event["inputRate"] != float64(24000) {
		t.Fatal("OpenAI call did not become ready")
	}
	pcm := append(append([]byte(nil), wav[44:]...), make([]byte, 48000)...)
	for offset := 0; offset < len(pcm); offset += 960 {
		end := offset + 960
		if end > len(pcm) {
			end = len(pcm)
		}
		if client.WriteJSON(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm[offset:end])}) != nil {
			t.Fatal("microphone relay failed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var output []byte
	transcribed, done := false, false
	for !(transcribed && done && len(output) > 0) {
		if client.ReadJSON(&event) != nil {
			t.Fatal("call ended before speech, transcript and response completion")
		}
		switch event["type"] {
		case "aide.error":
			t.Fatal("real call relay returned a provider error")
		case "conversation.item.input_audio_transcription.completed":
			text, _ := event["transcript"].(string)
			transcribed = strings.TrimSpace(text) != ""
		case "response.audio.delta":
			delta, _ := event["delta"].(string)
			part, e := base64.StdEncoding.DecodeString(delta)
			if e != nil || len(output)+len(part) > 8<<20 {
				t.Fatal("invalid or oversized PCM")
			}
			output = append(output, part...)
		case "response.done":
			done = true
		}
	}
	if outputPath := os.Getenv("AIDE_OPENAI_CALL_LIVE_OUTPUT"); outputPath != "" {
		buf := make([]byte, 44+len(output))
		copy(buf, "RIFF")
		binary.LittleEndian.PutUint32(buf[4:], uint32(len(buf)-8))
		copy(buf[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(buf[16:], 16)
		binary.LittleEndian.PutUint16(buf[20:], 1)
		binary.LittleEndian.PutUint16(buf[22:], 1)
		binary.LittleEndian.PutUint32(buf[24:], 24000)
		binary.LittleEndian.PutUint32(buf[28:], 48000)
		binary.LittleEndian.PutUint16(buf[32:], 2)
		binary.LittleEndian.PutUint16(buf[34:], 16)
		copy(buf[36:], "data")
		binary.LittleEndian.PutUint32(buf[40:], uint32(len(output)))
		copy(buf[44:], output)
		if os.WriteFile(outputPath, buf, 0600) != nil {
			t.Fatal("audio evidence write failed")
		}
	}
	t.Logf("authenticated Aide relay: OpenAI model %v voice cedar, input transcribed, %d PCM output bytes, response completed; no real microphone tested", realtimeCallMetadata(a.settings)["model"], len(output))
}
