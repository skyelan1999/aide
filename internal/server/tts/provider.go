// Package tts 提供可插拔的文本转语音（TTS）Provider 抽象。
//
// 设计目标：小蜜对话/导览朗读不再只能依赖浏览器内置 Web Speech（macOS 常回落机械音），
// 而是由后端把文本合成成神经音 MP3 流，前端按句排队播放。Provider 可插拔：
// 当前接入 edge-tts（微软 Read-Aloud，免费、零 key、中文自然度高），
// 未来可再接云端/本地开源引擎；浏览器 Web Speech 始终作为兜底（在前端实现）。
package tts

import (
	"context"
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

// DefaultVoice 按性别挑一个默认中文神经音。
func DefaultVoice(gender string) string {
	if gender == "male" {
		return "zh-CN-YunxiNeural"
	}
	return "zh-CN-XiaoxiaoNeural"
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
func NewProvider(name string, cfg Config) (TTSProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return newEdge(cfg), nil
	case "edge", "edge-tts", "edgetts":
		return newEdge(cfg), nil
	case "webspeech", "browser", "web":
		return nil, ErrBrowserOnly
	default:
		return nil, fmt.Errorf("未知 TTS 引擎：%s", name)
	}
}

// ErrBrowserOnly 表示该引擎只能在浏览器端跑（Web Speech），后端不提供合成。
var ErrBrowserOnly = fmt.Errorf("该引擎由浏览器本地合成，后端不提供音频流")
