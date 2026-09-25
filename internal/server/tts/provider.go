// Package tts 提供可插拔的文本转语音（TTS）Provider 抽象。
//
// 设计目标：小蜜对话/导览朗读不再只能依赖浏览器内置 Web Speech（macOS 常回落机械音），
// 而是由后端把文本合成成神经音 MP3 流，前端按句排队播放。Provider 可插拔：
// 当前接入 edge-tts（微软 Read-Aloud，免费、零 key、中文自然度高），
// 未来可再接云端/本地开源引擎；浏览器 Web Speech 始终作为兜底（在前端实现）。
package tts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SynthOpts 一次合成的参数。零值表示用 Provider 默认值。
type SynthOpts struct {
	// Voice 目标音色名（edge-tts 形如 zh-CN-XiaoxiaoNeural）。空=按 Gender 映射默认音色。
	Voice string
	// Rate 语速倍率：1.0 正常，0.8 慢，1.3 快。
	Rate float64
	// Pitch 音调倍率：1.0 正常，预留。
	Pitch float64
	// Style edge-tts express-as 情感风格（chat/gentle/cheerful…），空=不包 express-as。
	Style string
	// Gender male|female，未指定 Voice 时用于选默认音色。
	Gender string
	// Expressiveness 情感强度 0..1，映射为 edge-tts styledegree。
	Expressiveness float64
}

// Config 是创建 Provider 所需的外部配置（由 server.Settings 转换而来，避免循环依赖）。
type Config struct {
	Provider       string // auto | edge | webspeech
	Voice          string
	Endpoint       string // 预留：自定义 edge 端点/云端地址
	APIKey         string // 预留：云端引擎 key（edge-tts 不需要）
	Rate           float64
	Expressiveness float64
	Gender         string
	AzureKey       string // Azure 官方 Speech 订阅 key（可选；填了即作为 edge 的后备引擎）
	AzureRegion    string // Azure region，如 eastasia / eastus
	// ── 本地离线 sherpa-onnx（#44）：二进制打进镜像，模型外置 /data/tts ──
	SherpaBin string // sherpa-onnx-offline-tts 路径（空=默认 /usr/local/bin，可用 SHERPA_BIN 覆盖）
	SherpaDir string // 模型目录（空=默认 /data/tts，可用 SHERPA_TTS_DIR 覆盖）
	// ── 少样本音色克隆（#36）：自托管克隆服务接入 ──
	CloneBaseURL  string // 克隆服务 BaseURL（如 http://127.0.0.1:9880）；空=未配置
	CloneAPIKey   string // 克隆服务 Bearer token（可选，不回显）
	CloneVoiceID  string // 克隆出的音色 ID（创建音色后由克隆服务返回）
	CloneBackend  string // openai | gpt-sovits | indextts2 | cosyvoice2 | openvoice
}

// TTSProvider 一个具体的语音合成引擎。实现必须是并发安全的（每次 Synth 独立连接/会话）。
type TTSProvider interface {
	// Name 引擎标识：edge | webspeech | <云端名>。
	Name() string
	// Synth 合成一段文本，返回音频字节流。调用方负责 Close。
	// 首包（第一个音频字节）应在超时内到达，否则应尽快返回 error 供上层降级。
	Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error)
	// Format 输出容器："mp3" | "wav"。
	Format() string
	// Available 当前运行环境是否可用（如 edge-tts 需要联网）。
	Available() bool
}

// 已知 edge-tts 中文音色（前端下拉与性别映射共用）。
type Voice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`   // 展示名
	Gender string `json:"gender"` // female|male
}

// ChineseVoices 列出常用中文神经音（完整列表可在设置里手填音色 ID 扩展）。
func ChineseVoices() []Voice {
	return []Voice{
		{ID: "zh-CN-XiaoxiaoNeural", Name: "晓晓（温暖·女）", Gender: "female"},
		{ID: "zh-CN-YunxiNeural", Name: "云希（随和·男）", Gender: "male"},
		{ID: "zh-CN-XiaoyiNeural", Name: "晓伊（活泼·女）", Gender: "female"},
		{ID: "zh-CN-YunjianNeural", Name: "云健（解说·男）", Gender: "male"},
		{ID: "zh-CN-YunyangNeural", Name: "云扬（新闻·男）", Gender: "male"},
		{ID: "zh-CN-XiaohanNeural", Name: "晓涵（亲切·女）", Gender: "female"},
		{ID: "zh-CN-XiaomengNeural", Name: "晓梦（温柔·女）", Gender: "female"},
		{ID: "zh-CN-XiaoruiNeural", Name: "晓睿（知性·女）", Gender: "female"},
		{ID: "zh-CN-YunyeNeural", Name: "云野（沉稳·男）", Gender: "male"},
		{ID: "zh-CN-XiaoshuangNeural", Name: "晓双（儿童·女）", Gender: "female"},
	}
}

// DefaultVoice 按性别挑一个默认中文神经音：male→云希，neutral→晓伊，其余（female/空）→晓晓。
func DefaultVoice(gender string) string {
	switch gender {
	case "male":
		return "zh-CN-YunxiNeural"
	case "neutral":
		return "zh-CN-XiaoyiNeural"
	default:
		return "zh-CN-XiaoxiaoNeural"
	}
}

// ResolveVoice 归一化音色：显式 Voice 优先；否则按 Gender 映射默认。
func ResolveVoice(voice, gender string) string {
	v := strings.TrimSpace(voice)
	if v != "" {
		return v
	}
	return DefaultVoice(gender)
}

// NewProvider 按名字创建引擎。"auto" 解析为 edge（edge 不可用时返回 error，由前端降级 Web Speech）。
// "webspeech" 是浏览器端能力，后端不合成，返回 ErrBrowserOnly。
// "clone" 是少样本音色克隆（#36）：返回克隆 Provider，未配 BaseURL 时 Synth 返回 ErrCloneNotConfigured。
// "sherpa"/"local" 是本地离线引擎（#44）：模型未安装时 Synth 返回 ErrNotConfigured。
func NewProvider(name string, cfg Config) (TTSProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return newEdge(cfg), nil
	case "edge", "edge-tts", "edgetts":
		return newEdge(cfg), nil
	case "sherpa", "local", "offline", "sherpa-onnx":
		return newSherpa(cfg.SherpaBin, cfg.SherpaDir), nil
	case "webspeech", "browser", "web":
		return nil, ErrBrowserOnly
	case "clone", "voice-clone", "cloned":
		return newClone(cfg), nil
	default:
		return nil, fmt.Errorf("未知 TTS 引擎：%s", name)
	}
}

// ErrBrowserOnly 表示该引擎只能在浏览器端跑（Web Speech），后端不提供合成。
var ErrBrowserOnly = fmt.Errorf("该引擎由浏览器本地合成，后端不提供音频流")

// ErrUnavailable 表示引擎当前不可用（连接/握手/首包超时、网络不通等），应降级浏览器 Web Speech。
// edge 在超时类错误重试耗尽后包装返回；非超时错误（如 403 认证失败）不包装此错误。
var ErrUnavailable = fmt.Errorf("TTS 引擎不可用")

// ChainSynth 多引擎自动编排（#44 优先级）：
//
//	auto → 本地 sherpa(离线) → edge(联网) → Azure(配 key) → 全部失败返回 ErrUnavailable（前端降级浏览器 Web Speech）。
//
// sherpa 无模型时 Synth 返回 ErrNotConfigured，本函数自动跳过进入 edge（不向用户报错）。
// 显式选 sherpa 时链里只有 sherpa，未安装则直接报错（前端提示安装，不静默降级 edge）。
// 返回成功的音频流、实际生效的引擎名；全部失败返回聚合的 ErrUnavailable。
// 任一引擎返回 ErrBrowserOnly 直接跳过（浏览器引擎由前端处理）。
func ChainSynth(ctx context.Context, text string, opts SynthOpts, cfg Config) (io.ReadCloser, string, error) {
	engines := buildChain(cfg)
	var errs []string
	for _, eng := range engines {
		rc, err := eng.Synth(ctx, text, opts)
		if err == nil {
			return rc, eng.Name(), nil
		}
		errs = append(errs, eng.Name()+": "+err.Error())
		if errors.Is(err, ErrBrowserOnly) {
			continue
		}
	}
	return nil, "", fmt.Errorf("%w: %s", ErrUnavailable, strings.Join(errs, "; "))
}

// buildChain 构造优先级引擎链（#44：本地离线 sherpa 默认优先）。
//   - provider=auto/空：sherpa 第一（无模型自动跳过）→ edge → Azure(配 key)。
//   - provider=sherpa/local：只用 sherpa（未安装直接报错，不静默降级 edge）。
//   - provider=edge：edge → Azure（不走本地，用户明确要联网神经音）。
//   - provider=clone：克隆音色优先（#36），失败/未配置降级 edge；不进入 auto 链。
func buildChain(cfg Config) []TTSProvider {
	var chain []TTSProvider
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "clone", "voice-clone", "cloned":
		if c := newClone(cfg); c.Available() {
			chain = append(chain, c)
		}
		chain = append(chain, newEdge(cfg)) // 克隆不可达时降级 edge
	case "sherpa", "local", "offline", "sherpa-onnx":
		chain = append(chain, newSherpa(cfg.SherpaBin, cfg.SherpaDir))
	case "edge", "edge-tts", "edgetts":
		chain = append(chain, newEdge(cfg))
	default: // "", "auto"
		chain = append(chain, newSherpa(cfg.SherpaBin, cfg.SherpaDir)) // 本地离线优先；无模型自动跳过
		chain = append(chain, newEdge(cfg))
	}
	if cfg.AzureKey != "" {
		chain = append(chain, newAzure(cfg))
	}
	return chain
}

// ErrNotConfigured 该引擎未配置/未启用（如 sherpa-onnx 阶段2未接入）。
var ErrNotConfigured = fmt.Errorf("引擎未配置")
