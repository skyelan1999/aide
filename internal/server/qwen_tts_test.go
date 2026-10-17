package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQwenTTSSettings(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsProvider": "qwen", "ttsModel": "qwen3-tts-flash", "ttsRegion": "singapore", "ttsVoice": "Cherry", "ttsAPIKey": "synthetic-tts-secret"}), 200)
	a.mu.Lock()
	if a.settings.TTSProvider != "qwen" || a.settings.TTSModel != "qwen3-tts-flash" || a.settings.TTSRegion != "singapore" || a.settings.TTSAPIKey != "synthetic-tts-secret" {
		t.Fatal("settings not persisted")
	}
	a.mu.Unlock()
	r := request(a, "GET", "/api/config", nil)
	requireStatus(t, r, 200)
	if strings.Contains(r.Body.String(), "synthetic-tts-secret") {
		t.Fatal("key leaked")
	}
	var cfg map[string]any
	json.Unmarshal(r.Body.Bytes(), &cfg)
	if cfg["ttsProvider"] != "qwen" || cfg["hasTTSKey"] != true {
		t.Fatalf("wrong config %#v", cfg["ttsProvider"])
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsRate": 1.1}), 200)
	a.mu.Lock()
	if a.settings.TTSModel != "qwen3-tts-flash" || a.settings.TTSAPIKey != "synthetic-tts-secret" {
		t.Fatal("partial save lost settings")
	}
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"clearTTSKey": true}), 200)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.TTSAPIKey != "" {
		t.Fatal("key not cleared")
	}
}

func TestQwenCatalogReadOnly(t *testing.T) {
	a := testApp(t)
	r := request(a, "GET", "/api/tts/catalog", nil)
	requireStatus(t, r, 200)
	if !strings.Contains(r.Body.String(), "Serena") {
		t.Fatal("voice choices unavailable")
	}
	requireStatus(t, request(a, "POST", "/api/tts/catalog", map[string]any{"region": "unsupported"}), 200)
	a.mu.Lock()
	if a.settings.TTSRegion != "" {
		t.Fatal("discovery changed settings")
	}
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsBrowserFallback": true}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsRate": 1.2}), 200)
	a.mu.Lock()
	if !a.settings.TTSBrowserFallback {
		t.Fatal("partial settings cleared fallback preference")
	}
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsBrowserFallback": false}), 200)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.TTSBrowserFallback {
		t.Fatal("could not disable fallback")
	}
}
