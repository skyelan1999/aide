package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func configureVoiceModels(t *testing.T, a *App, base string) {
	t.Helper()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"modelProviders":    []ModelProvider{{ID: "default", Name: "Chat", BaseURL: "https://chat.example/v1", Keys: []ModelProviderKey{{ID: "key", Label: "Chat"}}, ActiveKeyID: "key"}, {ID: "voice", Name: "Speech", BaseURL: base, Keys: []ModelProviderKey{{ID: "key", Label: "Voice"}}, ActiveKeyID: "key"}},
		"providerKeyWrites": []map[string]any{{"providerId": "default", "keyId": "key", "apiKey": "fixture-chat-secret"}, {"providerId": "voice", "keyId": "key", "apiKey": "fixture-voice-secret"}},
		"models":            []ModelRef{{ID: "chat", ProviderID: "default"}, {ID: "speech-alias", ProviderID: "voice", VoiceAPI: "openai-speech"}, {ID: "gpt-realtime-2.1-mini", ProviderID: "voice"}}, "activeModel": "chat",
	}), 200)
}
func TestVoiceModelBindingsIsolationAndPersistence(t *testing.T) {
	a := testApp(t)
	configureVoiceModels(t, a, "https://api.openai.com/v1")
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsModelRef": "speech-alias", "assistantCallModelRef": "gpt-realtime-2.1-mini", "assistantCallVoice": "cedar", "openaiTTSVoice": "nova", "ttsAPIKey": "fixture-legacy"}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"voiceAssistantName": "Fixture"}), 200)
	if cfg := curSettings(a); cfg.Model != "chat" || cfg.ActiveModel != "chat" || cfg.TTSModelRef != "speech-alias" || cfg.AssistantCallVoice != "cedar" || cfg.TTSAPIKey != "fixture-legacy" {
		t.Fatal("partial update changed chat selection, binding or old credential")
	}
	a.mu.Lock()
	speech, err := a.voiceModelConfigLocked("speech-alias", false)
	call, callErr := a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err != nil || speech.OpenAITTSAPIKey != "fixture-voice-secret" || speech.OpenAITTSModel != "speech-alias" || speech.OpenAITTSMode != "speech" {
		t.Fatal("wrong voice provider resolution", err)
	}
	if callErr != nil || call.APIKey != "fixture-voice-secret" || call.OpenAITTSVoice != "cedar" {
		t.Fatal("wrong call binding", callErr)
	}
	cfg := request(a, "GET", "/api/config", nil)
	requireStatus(t, cfg, 200)
	if strings.Contains(cfg.Body.String(), "fixture-voice-secret") || strings.Contains(cfg.Body.String(), "fixture-legacy") || !strings.Contains(cfg.Body.String(), `"voiceAPI":"openai-speech"`) {
		t.Fatal("unsafe/incomplete config")
	}
	for _, patch := range []map[string]any{{"ttsModelRef": "missing"}, {"assistantCallModelRef": "speech-alias"}, {"ttsModelRef": "chat"}} {
		requireStatus(t, request(a, "PUT", "/api/settings", patch), 400)
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsModelRef": "", "assistantCallModelRef": ""}), 200)
	if cfg := curSettings(a); cfg.TTSModelRef != "" || cfg.AssistantCallModelRef != "" || cfg.TTSAPIKey != "fixture-legacy" {
		t.Fatal("clear lost old credential")
	}
}
func TestVoiceModelBindingsSynthesisUsesVault(t *testing.T) {
	a := testApp(t)
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer fixture-voice-secret" {
			t.Error("wrong key")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "speech-alias" || body["voice"] != "nova" {
			t.Error("wrong model/voice")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fixture"))
	}))
	defer upstream.Close()
	configureVoiceModels(t, a, upstream.URL+"/v1")
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsModelRef": "speech-alias", "openaiTTSVoice": "nova"}), 200)
	requireStatus(t, request(a, "POST", "/api/tts/synthesize", map[string]any{"text": "Fixture", "provider": "qwen", "voice": "wrong"}), 200)
	if hits != 1 {
		t.Fatal("missing synthesis")
	}
	a.mu.Lock()
	a.settings.Models = a.settings.Models[:1]
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/tts/synthesize", map[string]any{"text": "Fixture"}), 400)
	if hits != 1 {
		t.Fatal("deleted binding fell back to other provider")
	}
}
func TestVoiceModelBindingsQwenProtocols(t *testing.T) {
	a := testApp(t)
	configureVoiceModels(t, a, "https://dashscope-intl.aliyuncs.com/compatible-mode/v1")
	a.mu.Lock()
	a.settings.Models = append(a.settings.Models, ModelRef{ID: "qwen3-tts-flash", ProviderID: "voice"}, ModelRef{ID: "qwen3.8-omni-flash-realtime", ProviderID: "voice"})
	a.mu.Unlock()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"ttsModelRef": "qwen3-tts-flash"}), 200)
	a.mu.Lock()
	cfg, err := a.voiceModelConfigLocked("qwen3-tts-flash", false)
	a.mu.Unlock()
	if err != nil || cfg.TTSRegion != "singapore" || cfg.TTSAPIKey != "fixture-voice-secret" {
		t.Fatal("wrong Qwen region or key", err)
	}
	// A bound call honors the selected Qwen model and rejects a non-business endpoint.
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"assistantCallModelRef": "qwen3.8-omni-flash-realtime"}), 200)
	a.mu.Lock()
	_, err = a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err == nil {
		t.Fatal("non-business call endpoint accepted")
	}
	a.mu.Lock()
	a.settings.ModelProviders[1].BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.AssistantCallVoice = "Tina"
	cfg, err = a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	u, err := realtimeCallURL(cfg)
	if err != nil || !strings.Contains(u, "qwen3.8-omni-flash-realtime") {
		t.Fatal("wrong bound Qwen model")
	}
	if realtimeCallMetadata(cfg)["voice"] != "Tina" {
		t.Fatal("wrong call voice")
	}
}

func TestVoiceModelBindingsUnavailableKey(t *testing.T) {
	a := testApp(t)
	configureVoiceModels(t, a, "https://api.openai.com/v1")
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"assistantCallModelRef": "gpt-realtime-2.1-mini", "ttsModelRef": "speech-alias"}), 200)
	a.mu.Lock()
	a.vault.Lock()
	_, err := a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err == nil {
		t.Fatal("locked vault was bypassed")
	}
	requireStatus(t, request(a, "POST", "/api/tts/synthesize", map[string]any{"text": "Fixture"}), 400)
}

// The browser may send a write on every checkbox change. A failed disk write
// must never grant a permission in memory before the client sees the failure.
func TestSettingsAutoSavePermissionsPersistence(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"agentManagement": map[string]bool{"assistantPlugins": true, "aideReferences": true}}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"voiceAssistantName": "Fixture"}), 200)
	if !curSettings(a).AgentManagement.AssistantPlugins || !curSettings(a).AgentManagement.AideReferences {
		t.Fatal("unrelated autosave reset permissions")
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"agentManagement": map[string]bool{}}), 200)
	raw, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	var disk Settings
	if json.Unmarshal(raw, &disk) != nil || disk.AgentManagement != (AgentManagementPermissions{}) {
		t.Fatal("revocation not persisted")
	}
	settingsPath := SettingsPath(a.dataPath)
	backup := settingsPath + ".fixture-backup"
	if err = os.Rename(settingsPath, backup); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(settingsPath, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(settingsPath); _ = os.Rename(backup, settingsPath) })
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"agentManagement": map[string]bool{"aidePlugins": true}}), 500)
	if curSettings(a).AgentManagement.AidePlugins {
		t.Fatal("failed write granted permission")
	}
}
