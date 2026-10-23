package server

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Output intent is separate from input vision/audio support. Only explicit
// artifact requests enter generation; questions, coding and analysis stay text.
var mediaAction = regexp.MustCompile(`(?i)^(请|麻烦|能不能|可以|你)?\s*(帮我|为我|给我)?\s*(生成|制作|合成|画一|画个|朗读|配音|create |generate |make |synthesize |read aloud)`)

func generationIntent(prompt string) string {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if !mediaAction.MatchString(text) {
		return ""
	}
	for _, word := range []string{"代码", "脚本", "教程", "方案", "接口", "如何", "怎么", "是不是", "能否", "能不能生成", "code", "script", "tutorial", "how to"} {
		if strings.Contains(text, word) {
			return ""
		}
	}
	// Video takes precedence over images mentioned in its description.
	for _, word := range []string{"视频", "video", "mp4"} {
		if strings.Contains(text, word) {
			return "video"
		}
	}
	for _, word := range []string{"语音", "音频", "朗读", "配音", "mp3", "wav", "speech", "audio", "read aloud"} {
		if strings.Contains(text, word) {
			return "speech"
		}
	}
	for _, word := range []string{"图片", "图像", "张图", "个图", "的图", "画一", "画个", "image", "picture", "illustration"} {
		if strings.Contains(text, word) {
			return "image"
		}
	}
	return ""
}
func generationModelOutput(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if isQwenImageModel(name) {
		return "image"
	}
	if name == "wan2.7-t2v" || name == "wan2.7-t2v-2026-06-12" {
		return "video"
	}
	switch name {
	case "qwen-audio-3.0-tts-flash", "qwen-audio-3.0-tts-plus", "qwen-audio-3.1-tts-flash", "qwen-audio-3.1-tts-plus":
		return "speech"
	}
	return ""
}
func mediaTask(cfg Settings) bool {
	return cfg.generationIntent != "" || generationModelOutput(cfg.Model) != ""
}

type mediaRouteCandidate struct {
	Model  string `json:"model"`
	Score  int    `json:"score,omitempty"`
	Reason string `json:"reason"`
}

func (a *App) routeMediaModelLocked(task *Task, fallback Settings, c *routerCatalog, p *PluginManifest, output string, d *ModelRouteDecision) (Settings, *ModelRouteDecision) {
	d.Role = output
	d.Catalog = c.Revision
	fallback.generationIntent = output
	fallback.generationError = "未找到已配置、带密钥且有原生适配的" + output + "生成模型；请在模型设置中添加对应模型。没有改用文本模型或重复创建媒体任务。"
	type option struct {
		cfg          Settings
		id, provider string
		score        int
	}
	var options []option
	preference, _ := p.Settings["preference"].(string)
	for _, model := range fallback.Models {
		if task.MediaGeneration != nil && model.ID != task.MediaGeneration.Model {
			continue
		}
		var profile *routerProfile
		for i := range c.Models {
			item := &c.Models[i]
			if item.Output != output {
				continue
			}
			for _, alias := range item.Aliases {
				if routerModelName(alias) == routerModelName(model.ID) {
					profile = item
					break
				}
			}
			if profile != nil {
				break
			}
		}
		if profile == nil {
			continue
		}
		item := mediaRouteCandidate{Model: model.ID}
		cfg, err := settingsForSelectedModel(fallback, model.ID)
		switch {
		case generationModelOutput(model.ID) != output:
			item.Reason = "宿主尚无此模型的原生生成适配"
		case err != nil || !isBailianEndpoint(cfg.BaseURL):
			item.Reason = "需要已配置的百炼接口"
		case model.Capabilities[output+"_generation"] == "unsupported":
			item.Reason = "已禁用此输出能力"
		case len(task.Attachments) > 0:
			item.Reason = "此生成适配仅支持文字输入"
		default:
			key, keyErr := a.modelAPIKeyForConfigLocked(cfg)
			if keyErr != nil || key == "" {
				item.Reason = "所选提供商密钥不可用"
				break
			}
			cfg.APIKey = key
			cfg.routedProfile = profile
			cfg.generationIntent = output
			cfg.generationError = ""
			item.Score = profile.Weights[output]*100 + profile.Weights["fast"]
			if preference == "fast" {
				item.Score = profile.Weights["fast"]*100 + profile.Weights[output]*10
			} else if preference == "balanced" {
				item.Score += profile.Weights["fast"] * 10
			}
			item.Reason = "原生接口已适配；账户权限以实际调用结果为准"
			options = append(options, option{cfg, profile.ID, model.ProviderID, item.Score})
		}
		d.Candidates = append(d.Candidates, item)
	}
	if len(options) == 0 {
		d.Reason = fallback.generationError
		return fallback, d
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].score == options[j].score {
			return options[i].cfg.Model < options[j].cfg.Model
		}
		return options[i].score > options[j].score
	})
	best := options[0]
	d.Model = best.cfg.Model
	d.Provider = best.provider
	d.Profile = best.id
	d.Score = best.score
	d.Reason = fmt.Sprintf("%s 生成 · 适配分 %d · 原生单次生成；评分为维护者能力/速度启发式，非官方排名或账户开通保证", output, best.score)
	return best.cfg, d
}
