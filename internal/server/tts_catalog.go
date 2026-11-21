package server

import (
	"aide/internal/server/tts"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) discoverTTSModels(w http.ResponseWriter, r *http.Request) {
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	var in struct {
		ProviderID string `json:"providerId"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	a.mu.Lock()
	cfg := a.settings
	provider, ok := voiceProvider(cfg, strings.TrimSpace(in.ProviderID))
	if !ok {
		a.mu.Unlock()
		fail(w, 400, errors.New("请选择模型设置中已配置的提供商"))
		return
	}
	apiKey, err := a.activeVoiceKeyLocked(provider)
	a.mu.Unlock()
	if err != nil {
		fail(w, 400, err)
		return
	}
	u, err := tts.OpenAITTSBaseURL(provider.BaseURL)
	if err == nil && tts.IsMiMoHost(u.Hostname()) {
		u, err = tts.MiMoBaseURL(provider.BaseURL)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if tts.IsMiMoHost(u.Hostname()) {
		req.Header.Set("api-key", apiKey)
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		fail(w, 502, errors.New("无法连接此提供商的模型目录"))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(w, 502, fmt.Errorf("模型目录 HTTP %d，请检查密钥和 API 权限", resp.StatusCode))
		return
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&catalog); err != nil {
		fail(w, 502, errors.New("模型目录返回格式无效"))
		return
	}
	if catalog.Data == nil {
		fail(w, 502, errors.New("模型目录没有返回模型列表"))
		return
	}
	type voiceChoice struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		API  string `json:"voiceAPI"`
		Read bool   `json:"read"`
		Call bool   `json:"call"`
	}
	models := make([]voiceChoice, 0)
	seen := make(map[string]bool)
	for _, entry := range catalog.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || len(id) > 160 || seen[id] {
			continue
		}
		model := providerVoiceModel(cfg, provider, id)
		if model.VoiceAPI == "" {
			continue
		}
		seen[id] = true
		models = append(models, voiceChoice{id, model.Name, model.VoiceAPI, voiceModelSupports(model.VoiceAPI, false), voiceModelSupports(model.VoiceAPI, true)})
	}
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	jsonOut(w, 200, map[string]any{"providerId": provider.ID, "models": models})
}

func (a *App) ttsCatalog(w http.ResponseWriter, r *http.Request) {
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	if r.Method == http.MethodGet {
		jsonOut(w, 200, tts.DefaultQwenCatalog())
		return
	}
	var in struct {
		ModelRef string `json:"modelRef,omitempty"`
		Region   string `json:"region"`
		APIKey   string `json:"apiKey"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	cfg := tts.Config{Region: a.settings.TTSRegion, APIKey: a.settings.TTSAPIKey}
	var bindingErr error
	if in.ModelRef != "" {
		var bound Settings
		bound, bindingErr = a.voiceModelConfigLocked(in.ModelRef, false)
		if bindingErr == nil {
			if bound.TTSProvider != "qwen" {
				bindingErr = errors.New("请选择千问 TTS 模型后刷新目录")
			} else {
				cfg.Region, cfg.APIKey = bound.TTSRegion, bound.TTSAPIKey
			}
		}
	}
	a.mu.Unlock()
	if bindingErr != nil {
		fail(w, 400, bindingErr)
		return
	}
	if in.ModelRef == "" && in.Region != "" {
		cfg.Region = in.Region
	}
	if in.ModelRef == "" && in.APIKey != "" {
		cfg.APIKey = in.APIKey
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	catalog := tts.DiscoverQwen(ctx, cfg)
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	jsonOut(w, 200, catalog)
}
