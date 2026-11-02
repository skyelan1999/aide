package server

import (
	"errors"
	"net/url"
	"strings"
)

// Voice protocol is explicit for aliases and inferred only for known model IDs.
// Selection never changes the workbench model or stores a copy of a vault key.
func voiceModelAPI(m ModelRef) string {
	if m.VoiceAPI != "" {
		return m.VoiceAPI
	}
	id := strings.ToLower(m.ID)
	switch {
	case strings.HasPrefix(id, "qwen") && strings.Contains(id, "omni") && strings.Contains(id, "realtime"):
		return "qwen-realtime"
	case strings.HasPrefix(id, "qwen") && strings.Contains(id, "tts") && !strings.Contains(id, "realtime"):
		return "qwen-tts"
	case strings.HasPrefix(id, "gpt-realtime"):
		return "openai-realtime"
	case strings.HasPrefix(id, "tts-1") || strings.Contains(id, "-tts"):
		return "openai-speech"
	}
	return ""
}
func voiceModel(cfg Settings, id string, call bool) (ModelRef, error) {
	for _, m := range cfg.Models {
		if m.ID == id {
			kind := voiceModelAPI(m)
			if kind == "" || call && kind != "openai-realtime" && kind != "qwen-realtime" || !call && kind == "qwen-realtime" {
				return m, errors.New("所选模型的语音接口不适用于此用途，请在模型设置中检查语音接口类型")
			}
			return m, nil
		}
	}
	return ModelRef{}, errors.New("所选语音模型已不可用，请在模型设置中重新配置")
}
func voiceBindingMetadata(cfg Settings) map[string]any {
	out := map[string]any{"modelRef": cfg.TTSModelRef, "callModelRef": cfg.AssistantCallModelRef, "callVoice": cfg.AssistantCallVoice}
	if cfg.TTSModelRef == "" {
		return out
	}
	m, err := voiceModel(cfg, cfg.TTSModelRef, false)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	kind := voiceModelAPI(m)
	provider := "openai"
	voice := cfg.OpenAITTSVoice
	if kind == "qwen-tts" {
		provider = "qwen"
		voice = cfg.TTSVoice
	}
	out["provider"], out["model"], out["voice"], out["api"] = provider, m.ID, voice, kind
	return out
}

// Caller holds a.mu. Missing bindings/keys fail closed instead of borrowing
// whichever provider happens to be active in chat or another voice slot.
func (a *App) voiceModelConfigLocked(id string, call bool) (Settings, error) {
	m, err := voiceModel(a.settings, id, call)
	if err != nil {
		return Settings{}, err
	}
	cfg, err := settingsForSelectedModel(a.settings, id)
	if err != nil {
		return cfg, err
	}
	key, err := a.modelAPIKeyForConfigLocked(cfg)
	if err != nil {
		return cfg, err
	}
	if key == "" {
		return cfg, errors.New("请在模型设置中配置所选语音模型的厂商密钥")
	}
	switch voiceModelAPI(m) {
	case "openai-realtime", "openai-speech":
		cfg.TTSProvider = "openai"
		cfg.OpenAITTSAPIKey = key
		cfg.OpenAITTSBaseURL = cfg.BaseURL
		cfg.OpenAITTSModel = m.ID
		cfg.OpenAITTSMode = "speech"
		if voiceModelAPI(m) == "openai-realtime" {
			cfg.OpenAITTSMode = "realtime"
		}
		if call && cfg.AssistantCallVoice != "" {
			cfg.OpenAITTSVoice = cfg.AssistantCallVoice
		}
		if call {
			return openAINativeCallConfig(cfg)
		}
	case "qwen-tts":
		u, e := url.Parse(cfg.BaseURL)
		if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return cfg, errors.New("千问 TTS 需要有效的百炼 HTTPS 提供商地址")
		}
		// Native TTS uses the official regional endpoint, never a chat proxy URL.
		switch strings.ToLower(u.Hostname()) {
		case "dashscope.aliyuncs.com":
			cfg.TTSRegion = "beijing"
		case "dashscope-intl.aliyuncs.com":
			cfg.TTSRegion = "singapore"
		default:
			return cfg, errors.New("千问 TTS 提供商请使用 dashscope.aliyuncs.com 或 dashscope-intl.aliyuncs.com")
		}
		cfg.TTSProvider = "qwen"
		cfg.TTSModel = m.ID
		cfg.TTSAPIKey = key
	case "qwen-realtime":
		cfg.TTSProvider = "qwen"
		cfg.APIKey = key
		if _, err = realtimeCallURL(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
