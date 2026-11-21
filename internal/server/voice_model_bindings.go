package server

import (
	"aide/internal/server/tts"
	"errors"
	"strings"
)

// Voice protocol is explicit for aliases and inferred only for known model IDs.
// Selection never changes the workbench model or stores a copy of a vault key.
func voiceModelAPI(m ModelRef) string {
	id := strings.ToLower(strings.TrimSpace(m.ID))
	if strings.HasPrefix(id, "gpt-realtime-whisper") || strings.HasPrefix(id, "gpt-realtime-translate") {
		return ""
	}
	// MiMo exposes a separate chat-completions TTS protocol. It must not be
	// inferred as generic OpenAI Speech just because its model ID ends in -tts.
	if strings.HasPrefix(id, "mimo-") && strings.Contains(id, "-tts") && !strings.Contains(id, "realtime") {
		return "mimo-tts"
	}
	if m.VoiceAPI != "" {
		return m.VoiceAPI
	}
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
func voiceProvider(cfg Settings, id string) (ModelProvider, bool) {
	for _, provider := range cfg.ModelProviders {
		if id != "" && provider.ID == id {
			return provider, true
		}
	}
	return ModelProvider{}, false
}

func (a *App) activeVoiceKeyLocked(provider ModelProvider) (string, error) {
	if provider.ActiveKeyID != "" {
		key, err := a.vaultKeyByIDLocked(modelProviderVaultID(provider.ID, provider.ActiveKeyID))
		if err != nil || key != "" {
			return key, err
		}
	}
	if tts.LocalSpeechEndpoint(provider.BaseURL) {
		return "", nil
	}
	return "", errors.New("请在模型设置中配置此提供商的当前 API 密钥")
}

func legacyQwenRealtimeHost(host, model string) bool {
	return !strings.HasPrefix(strings.ToLower(model), "qwen3.8-") &&
		isQwenRealtime(model) && (host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com")
}

// A discovered model can be selected without adding it to the workbench list.
func providerVoiceModel(cfg Settings, provider ModelProvider, id string) ModelRef {
	model := ModelRef{ID: id, ProviderID: provider.ID}
	for _, configured := range cfg.Models {
		if configured.ID == id && configured.ProviderID == provider.ID {
			model = configured
			break
		}
	}
	model.VoiceAPI = providerVoiceAPI(provider.BaseURL, model)
	return model
}

func providerVoiceAPI(baseURL string, model ModelRef) string {
	u, err := tts.OpenAITTSBaseURL(baseURL)
	if err != nil || strings.TrimSpace(baseURL) == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	kind := voiceModelAPI(model)
	if tts.IsMiMoHost(host) {
		if _, err := tts.MiMoBaseURL(baseURL); err == nil && tts.IsMiMoTTSModel(model.ID) {
			return "mimo-tts"
		}
		return ""
	}
	if isBailianEndpoint(baseURL) {
		if u.Scheme != "https" || u.Port() != "" {
			return ""
		}
		workspace := strings.HasSuffix(host, ".maas.aliyuncs.com")
		switch kind {
		case "qwen-realtime":
			if workspace || legacyQwenRealtimeHost(host, model.ID) {
				return kind
			}
		case "qwen-tts":
			if workspace || host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com" {
				return kind
			}
		}
		return ""
	}
	switch kind {
	case "openai-realtime":
		if u.Scheme == "https" && !tts.LocalSpeechEndpoint(baseURL) {
			return kind
		}
	case "openai-speech":
		id := strings.ToLower(model.ID)
		if model.VoiceAPI == kind || strings.HasPrefix(id, "tts-1") || strings.HasPrefix(id, "gpt-4o-mini-tts") {
			return kind
		}
	}
	return ""
}

func voiceModelSupports(kind string, call bool) bool {
	if call {
		return kind == "openai-realtime" || kind == "qwen-realtime"
	}
	return kind == "openai-realtime" || kind == "openai-speech" || kind == "qwen-tts" || kind == "mimo-tts"
}

func voiceModel(cfg Settings, id string, call bool) (ModelRef, error) {
	if cfg.VoiceModelProviderID != nil {
		if id == "" || len(id) > 160 {
			return ModelRef{}, errors.New("语音模型 ID 无效")
		}
		provider, ok := voiceProvider(cfg, *cfg.VoiceModelProviderID)
		if !ok {
			return ModelRef{}, errors.New("请在助理与语音设置中选择可用的模型提供商")
		}
		model := providerVoiceModel(cfg, provider, id)
		if !voiceModelSupports(model.VoiceAPI, call) {
			return ModelRef{}, errors.New("所选提供商的模型不支持此语音用途")
		}
		return model, nil
	}
	for _, m := range cfg.Models {
		if m.ID == id {
			kind := voiceModelAPI(m)
			if !voiceModelSupports(kind, call) {
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
	switch kind {
	case "qwen-tts":
		provider, voice = "qwen", cfg.TTSVoice
	case "mimo-tts":
		provider, voice = "mimo", cfg.TTSVoice
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
	cfg := a.settings
	var key string
	if cfg.VoiceModelProviderID != nil {
		provider, _ := voiceProvider(cfg, *cfg.VoiceModelProviderID)
		cfg.Model, cfg.BaseURL = m.ID, provider.BaseURL
		key, err = a.activeVoiceKeyLocked(provider)
		if err != nil {
			return cfg, err
		}
	} else {
		cfg, err = settingsForSelectedModel(a.settings, id)
		if err != nil {
			return cfg, err
		}
		keyID := ""
		if !call {
			keyID = strings.TrimSpace(a.settings.TTSModelKeyID)
		}
		if keyID != "" {
			providerID := "default"
			if m.ProviderID != "" {
				providerID = m.ProviderID
			}
			valid := false
			for _, provider := range a.settings.ModelProviders {
				if provider.ID != providerID {
					continue
				}
				for _, providerKey := range provider.Keys {
					if providerKey.ID == keyID {
						valid = true
						break
					}
				}
				break
			}
			if !valid {
				return cfg, errors.New("所选语音密钥不属于当前模型连接")
			}
			key, err = a.vaultKeyByIDLocked(modelProviderVaultID(providerID, keyID))
			if err == nil && key == "" {
				err = errors.New("所选语音密钥尚未配置")
			}
		} else {
			key, err = a.modelAPIKeyForConfigLocked(cfg)
		}
		if err != nil {
			return cfg, err
		}
		if key == "" {
			return cfg, errors.New("请在模型设置中配置所选语音模型的厂商密钥")
		}
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
	case "mimo-tts":
		if cfg.VoiceModelProviderID == nil {
			return cfg, errors.New("请在助理与语音设置中选择 MiMo 提供商和模型")
		}
		cfg.TTSProvider, cfg.TTSModel, cfg.TTSAPIKey = "mimo", m.ID, key
		cfg.TTSEndpoint = cfg.BaseURL
	case "qwen-tts":
		u, e := tts.QwenSpeechBaseURL(cfg.BaseURL)
		if e != nil {
			return cfg, e
		}
		cfg.TTSProvider, cfg.TTSModel, cfg.TTSAPIKey = "qwen", m.ID, key
		if cfg.VoiceModelProviderID != nil {
			cfg.TTSEndpoint = u.String()
		}
		if strings.Contains(u.Hostname(), "southeast-1") || u.Hostname() == "dashscope-intl.aliyuncs.com" {
			cfg.TTSRegion = "singapore"
		} else {
			cfg.TTSRegion = "beijing"
		}
	case "qwen-realtime":
		cfg.TTSProvider = "qwen"
		cfg.APIKey = key
		if _, err = realtimeCallURL(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// mimoTTSConfigLocked resolves the independently selected TTS provider/key from
// the encrypted vault; it never changes the chat provider's active key.
func (a *App) mimoTTSConfigLocked(cfg Settings) (Settings, error) {
	providerID := strings.TrimSpace(cfg.TTSModelProviderID)
	if providerID == "" {
		return cfg, errors.New("请先在语音设置中选择 MiMo 模型连接")
	}
	var selected *ModelProvider
	for i := range cfg.ModelProviders {
		if cfg.ModelProviders[i].ID == providerID {
			selected = &cfg.ModelProviders[i]
			break
		}
	}
	if selected == nil {
		return cfg, errors.New("所选 MiMo 模型连接已不存在，请重新选择")
	}
	keyID := strings.TrimSpace(cfg.TTSModelKeyID)
	if keyID == "" {
		keyID = selected.ActiveKeyID
	}
	valid := false
	for _, item := range selected.Keys {
		if item.ID == keyID {
			valid = true
			break
		}
	}
	if !valid {
		return cfg, errors.New("所选语音密钥不属于当前模型连接")
	}
	key, err := a.vaultKeyByIDLocked(modelProviderVaultID(providerID, keyID))
	if err != nil || key == "" {
		if err == nil {
			err = errors.New("所选语音密钥尚未配置")
		}
		return cfg, err
	}
	cfg.TTSProvider = "mimo"
	cfg.TTSModelKeyID = keyID
	cfg.TTSModel = strings.TrimSpace(cfg.TTSModel)
	cfg.TTSEndpoint = selected.BaseURL
	cfg.APIKey = key
	cfg.TTSAPIKey = key
	return cfg, nil
}
