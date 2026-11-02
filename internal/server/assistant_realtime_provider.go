package server

import (
	"aide/internal/server/tts"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

func openAINativeCall(cfg Settings) bool { return ttsProviderName(cfg.TTSProvider) == "openai" }

func openAINativeCallConfig(cfg Settings) (Settings, error) {
	if cfg.OpenAITTSMode != "" && cfg.OpenAITTSMode != "realtime" {
		return cfg, errors.New("OpenAI 语音通话需要 Realtime 接口，请先保存 Realtime 语音设置")
	}
	if strings.TrimSpace(cfg.OpenAITTSAPIKey) == "" {
		return cfg, errors.New("请先配置独立的 OpenAI 语音 API Key")
	}
	u, err := tts.OpenAITTSBaseURL(cfg.OpenAITTSBaseURL)
	if err != nil {
		return cfg, err
	}
	if u.Scheme != "https" {
		return cfg, errors.New("OpenAI 语音通话需要 HTTPS 接口地址")
	}
	cfg.BaseURL, cfg.APIKey = u.String(), strings.TrimSpace(cfg.OpenAITTSAPIKey)
	cfg.Model = strings.TrimSpace(cfg.OpenAITTSModel)
	if cfg.Model == "" {
		cfg.Model = tts.OpenAITTSDefaultModel
	}
	return cfg, nil
}

func openAINativeCallURL(cfg Settings) (string, error) {
	cfg, err := openAINativeCallConfig(cfg)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(cfg.BaseURL)
	u.Scheme = "wss"
	u.Path = strings.TrimRight(u.Path, "/") + "/realtime"
	u.RawQuery = url.Values{"model": {cfg.Model}}.Encode()
	return u.String(), nil
}

func realtimeCallMetadata(cfg Settings) map[string]any {
	provider, model, voice, rate := "qwen", "qwen3.8-omni-flash-realtime", "Tina", 16000
	if openAINativeCall(cfg) {
		provider, model, voice, rate = "openai", cfg.Model, strings.TrimSpace(cfg.OpenAITTSVoice), 24000
		if model == "" {
			model = tts.OpenAITTSDefaultModel
		}
		if voice == "" {
			voice = "marin"
		}
	}
	return map[string]any{"provider": provider, "model": model, "voice": voice, "transport": "native-duplex", "inputRate": rate, "outputRate": 24000}
}

func sameRealtimeCallProvider(a, b Settings) bool {
	uA, errA := realtimeCallURL(a)
	uB, errB := realtimeCallURL(b)
	return errA == nil && errB == nil && uA == uB && a.APIKey == b.APIKey && realtimeCallMetadata(a)["voice"] == realtimeCallMetadata(b)["voice"]
}

// The existing relay owns auth, tools, memory and interruption. Convert only
// provider wire shapes, keeping those invariants in one shared state machine.
func realtimeCallWireEvent(cfg Settings, event map[string]any) map[string]any {
	if !openAINativeCall(cfg) || event["type"] != "session.update" {
		return event
	}
	copyEvent := make(map[string]any, len(event))
	for k, v := range event {
		copyEvent[k] = v
	}
	if session, ok := event["session"].(map[string]any); ok {
		copySession := make(map[string]any, len(session)+1)
		for k, v := range session {
			copySession[k] = v
		}
		copySession["type"] = "realtime"
		copyEvent["session"] = copySession
	}
	return copyEvent
}

func realtimeCallNormalizeEvent(cfg Settings, event map[string]json.RawMessage) {
	if !openAINativeCall(cfg) {
		return
	}
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	switch kind {
	case "response.output_audio.delta":
		kind = "response.audio.delta"
	case "response.output_audio.done":
		kind = "response.audio.done"
	case "response.output_audio_transcript.delta":
		kind = "response.audio_transcript.delta"
	case "response.output_audio_transcript.done":
		kind = "response.audio_transcript.done"
	default:
		return
	}
	event["type"], _ = json.Marshal(kind)
}

func openAINativeCallSession(cfg Settings, instruction string, tool any) map[string]any {
	return map[string]any{"type": "realtime", "output_modalities": []string{"audio"}, "instructions": instruction,
		"audio": map[string]any{
			"input":  map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "transcription": map[string]any{"model": "gpt-transcribe"}, "turn_detection": map[string]any{"type": "server_vad", "silence_duration_ms": 650, "create_response": true, "interrupt_response": true}},
			"output": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "voice": realtimeCallMetadata(cfg)["voice"]}},
		"tools": []any{tool}, "tool_choice": "auto"}
}
