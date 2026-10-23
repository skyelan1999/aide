package server

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Generation tools use model-provider credentials and never mutate or read the
// assistant's TTS provider, voice or API key settings.
var ttsVoiceName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (a *App) ttsGenerationTool(ctx context.Context, task *Task, args map[string]any) (string, error) {
	if a.pluginOwnerOf("tts_generate") != "tts-tools" {
		return "", errors.New("请先启用 TTS Tools 插件")
	}
	text, _ := args["text"].(string)
	text = strings.TrimSpace(text)
	format, _ := args["format"].(string)
	if format == "" {
		format = "mp3"
	}
	if format != "mp3" && format != "wav" {
		return "", errors.New("当前支持 MP3、WAV 文件输出")
	}
	voice, _ := args["voice"].(string)
	if voice != "" && !ttsVoiceName.MatchString(voice) {
		return "", errors.New("无效音色 ID")
	}
	model, _ := args["model"].(string)
	a.mu.Lock()
	cfg := a.settings
	if model != "" {
		var err error
		cfg, err = settingsForSelectedModel(cfg, model)
		if err != nil {
			a.mu.Unlock()
			return "", err
		}
		if generationModelOutput(cfg.Model) != "speech" {
			a.mu.Unlock()
			return "", errors.New("请选择已配置的非实时 Qwen-Audio-TTS 语音生成模型")
		}
		cfg.APIKey, err = a.modelAPIKeyForConfigLocked(cfg)
		if err != nil {
			a.mu.Unlock()
			return "", err
		}
	} else {
		catalog, plugin := a.modelRouterLocked()
		if catalog == nil {
			a.mu.Unlock()
			return "", errors.New("请指定已配置的语音模型，或启用支持语音评分的 AutoAiModelRouter")
		}
		decision := &ModelRouteDecision{Stage: "tts_generate", At: time.Now().UTC().Format(time.RFC3339Nano)}
		requestTask := *task
		requestTask.Attachments = nil
		cfg, decision = a.routeMediaModelLocked(&requestTask, cfg, catalog, plugin, "speech", decision)
		if len(task.ModelRoutes) < 64 {
			task.ModelRoutes = append(task.ModelRoutes, *decision)
		}
	}
	a.mu.Unlock()
	if cfg.generationError != "" {
		return "", errors.New(cfg.generationError)
	}
	if !isBailianEndpoint(cfg.BaseURL) {
		return "", errors.New("语音生成模型需要对应地域的百炼业务空间接口")
	}
	out, _, err := a.mediaGenerationChatRequest(ctx, cfg, nil, task, -1, "内容："+text, voice, format, false)
	return out, err
}
func ttsSystemVoices() string {
	raw, _ := json.Marshal(map[string]any{"voices": []map[string]string{{"model": "qwen-audio-3.0-tts-flash / plus", "voice": "longanhuan_v3.6", "description": "官方 HTTP 示例系统音色"}, {"model": "qwen-audio-3.1-tts-flash / plus", "voice": "longanhuan_v3.1", "description": "官方 3.1 系统音色"}}, "formats": []string{"mp3", "wav"}, "source": "https://help.aliyun.com/zh/model-studio/qwen-audio-tts-http-api", "note": "语音文件生成独立于助理默认朗读设置。其他系统音色请以服务商模型对应音色列表为准。"})
	return string(raw)
}
