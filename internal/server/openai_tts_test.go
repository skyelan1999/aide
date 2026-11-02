package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenAITTSSettingsAndIsolation(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsProvider": "qwen", "ttsModel": "qwen3-tts-flash", "ttsVoice": "Cherry", "ttsAPIKey": "fixture-qwen-key"}), 200)
	in := map[string]any{"ttsProvider": "openai", "openaiTTSMode": "speech", "openaiTTSModel": "tts-1", "openaiTTSVoice": "nova", "openaiTTSBaseURL": "https://api.openai.com/v1", "openaiTTSAPIKey": "fixture-openai-key"}
	requireStatus(t, request(a, "PUT", "/api/settings", in), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsRate": 1.2}), 200)
	a.mu.Lock()
	saved := a.settings
	a.mu.Unlock()
	if saved.OpenAITTSModel != "tts-1" || saved.OpenAITTSAPIKey != "fixture-openai-key" || saved.TTSAPIKey != "fixture-qwen-key" || saved.TTSVoice != "Cherry" || saved.TTSModel != "qwen3-tts-flash" {
		t.Fatal("provider setting overwritten")
	}
	cfg := request(a, "GET", "/api/config", nil)
	requireStatus(t, cfg, 200)
	if strings.Contains(cfg.Body.String(), "fixture-openai-key") {
		t.Fatal("key echoed")
	}
	var out map[string]any
	_ = json.Unmarshal(cfg.Body.Bytes(), &out)
	if out["ttsProvider"] != "openai" || out["hasOpenAITTSKey"] != true || out["openaiTTSVoice"] != "nova" {
		t.Fatal("missing safe config")
	}
	backup := request(a, "POST", "/api/config/export", map[string]any{})
	requireStatus(t, backup, 200)
	if strings.Contains(backup.Body.String(), "fixture-openai-key") {
		t.Fatal("key in public backup")
	}
	for _, patch := range []map[string]any{{"openaiTTSMode": "bad"}, {"openaiTTSBaseURL": "https://user:secret@example.com/v1"}} {
		requireStatus(t, request(a, "PUT", "/api/settings", patch), 400)
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"clearOpenAITTSKey": true}), 200)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.OpenAITTSAPIKey != "" || a.settings.TTSAPIKey != "fixture-qwen-key" {
		t.Fatal("clear crossed providers")
	}
}

func TestOpenAITTSSynthesisUsesSeparateConfig(t *testing.T) {
	a := testApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" || r.Header.Get("Authorization") != "Bearer fixture-openai" {
			t.Error("wrong credential/endpoint")
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["voice"] != "nova" || in["model"] != "tts-1-hd" {
			t.Error("Qwen settings leaked into OpenAI request")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fixture"))
	}))
	defer srv.Close()
	a.mu.Lock()
	a.settings.TTSProvider = "openai"
	a.settings.TTSAPIKey = "wrong-qwen"
	a.settings.TTSVoice = "Cherry"
	a.settings.TTSModel = "qwen3-tts-flash"
	a.settings.OpenAITTSMode = "speech"
	a.settings.OpenAITTSAPIKey = "fixture-openai"
	a.settings.OpenAITTSBaseURL = srv.URL + "/v1"
	a.settings.OpenAITTSModel = "tts-1-hd"
	a.settings.OpenAITTSVoice = "nova"
	a.mu.Unlock()
	r := request(a, "POST", "/api/tts/synthesize", map[string]any{"text": "你好"})
	requireStatus(t, r, 200)
	if r.Header().Get("X-TTS-Engine") != "openai" || r.Header().Get("Content-Type") != "audio/mpeg" || r.Body.String() != "ID3fixture" {
		t.Fatal("wrong audio response")
	}
}

func TestOpenAITTSPersistenceAndImport(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"openaiTTSAPIKey": "fixture-current", "openaiTTSVoice": "cedar", "openaiTTSModel": "gpt-realtime-2.1-mini"}), 200)
	raw, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	var disk Settings
	if err = json.Unmarshal(raw, &disk); err != nil || disk.OpenAITTSAPIKey != "fixture-current" || disk.OpenAITTSVoice != "cedar" {
		t.Fatal("not persisted")
	}
	backup := json.RawMessage(`{"openaiTTSAPIKey":"fixture-backup","openaiTTSVoice":"marin"}`)
	requireStatus(t, importBackupRaw(t, a, backup, configBackupVersion, configSettingsVersion, false), 200)
	if curSettings(a).OpenAITTSAPIKey != "fixture-current" {
		t.Fatal("non-secret import replaced current key")
	}
	requireStatus(t, importBackupRaw(t, a, backup, configBackupVersion, configSettingsVersion, true), 200)
	if curSettings(a).OpenAITTSAPIKey != "fixture-backup" {
		t.Fatal("explicit secret import did not restore key")
	}
}
