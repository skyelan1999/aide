package tts

// 少样本音色克隆 Provider（#36）。
//
// 设计原则：可插拔 + 自托管。aide 主镜像不内置 GB 级克隆大模型（GPU 才实用），
// 而是把"克隆合成"抽象成一个 HTTP 适配层：用户在设置里填一个自托管克隆服务的
// BaseURL + voiceID，小秘朗读时就走那个声音；未配置/不可达时优雅降级到 edge/Web Speech。
//
// 通信协议优先级：
//  1. OpenAI 兼容 /audio/speech（IndexTTS2/CosyVoice2 的 OpenAI 兼容层、自建网关默认）
//  2. GPT-SoVITS 官方 HTTP API（/tts）
//  3. OpenVoice v2 预留扩展位（默认按 OpenAI 兼容协议请求）
//
// 具体后端由 Settings.CloneTTSBackend 选择；默认 "openai"。新增后端只需在 Synth 里加一个 case。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 后端标识（Settings.CloneTTSBackend 取值）。
const (
	CloneBackendOpenAI     = "openai"      // OpenAI 兼容 POST /audio/speech（默认）
	CloneBackendGPTSoVITS  = "gpt-sovits"  // GPT-SoVITS 官方 HTTP /tts
	CloneBackendIndexTTS2  = "indextts2"  // IndexTTS2 OpenAI 兼容层（同 openai）
	CloneBackendCosyVoice2 = "cosyvoice2" // CosyVoice2 OpenAI 兼容层
	CloneBackendOpenVoice  = "openvoice"  // OpenVoice v2 REST（预留，同 openai）
)

// cloneSynthTimeout 克隆服务合成总超时。克隆模型首包可能比 edge 慢（GPU 冷启动/队列），给 30s。
var cloneSynthTimeout = 30 * time.Second

// ErrCloneNotConfigured 未配置克隆服务 BaseURL；上层应降级到 edge/Web Speech。
var ErrCloneNotConfigured = fmt.Errorf("克隆音色服务未配置")

type cloneProvider struct {
	baseURL string
	apiKey  string
	voiceID string
	backend string
	client  *http.Client
}

// newClone 构造克隆 Provider。baseURL 为空时 Available()=false，Synth 返回 ErrCloneNotConfigured。
func newClone(cfg Config) *cloneProvider {
	backend := strings.ToLower(strings.TrimSpace(cfg.CloneBackend))
	if backend == "" {
		backend = CloneBackendOpenAI
	}
	return &cloneProvider{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.CloneBaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.CloneAPIKey),
		voiceID: strings.TrimSpace(cfg.CloneVoiceID),
		backend: backend,
		client:  &http.Client{Timeout: cloneSynthTimeout},
	}
}

func (c *cloneProvider) Name() string { return "clone" }

// Format 克隆服务多数返回 mp3（OpenAI 兼容）。
func (c *cloneProvider) Format() string { return "mp3" }

// Available 仅看 BaseURL 是否已配；真实可达性在 Synth 首包判断（与 edge 一致）。
func (c *cloneProvider) Available() bool { return c.baseURL != "" }

// Synth 把文本发到自托管克隆服务，返回音频字节流。
// 未配置 → ErrCloneNotConfigured（上层降级 edge）；不可达/非 2xx → 包装 ErrUnavailable（上层切下一引擎）。
func (c *cloneProvider) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("合成文本为空")
	}
	if c.baseURL == "" {
		return nil, ErrCloneNotConfigured
	}
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = c.voiceID
	}

	ctx, cancel := context.WithTimeout(ctx, cloneSynthTimeout)
	defer cancel()

	var req *http.Request
	var err error
	switch c.backend {
	case CloneBackendGPTSoVITS:
		req, err = c.buildGPTSoVITSRequest(ctx, text, voice)
	default: // openai | indextts2 | cosyvoice2 | openvoice(预留,同openai)
		req, err = c.buildOpenAIRequest(ctx, text, voice, opts)
	}
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: clone 请求失败：%w", ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: clone HTTP %d: %s", ErrUnavailable, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength == 0 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: clone 返回空音频", ErrUnavailable)
	}
	return resp.Body, nil
}

// buildOpenAIRequest OpenAI 兼容 POST {baseURL}/audio/speech。
// body: {model, input, voice, response_format, speed}
// IndexTTS2 / CosyVoice2 的 OpenAI 兼容层均接受该 schema；voice 即克隆出的 voiceID。
func (c *cloneProvider) buildOpenAIRequest(ctx context.Context, text, voice string, opts SynthOpts) (*http.Request, error) {
	if voice == "" {
		voice = "clone"
	}
	speed := opts.Rate
	if speed <= 0 {
		speed = 1.0
	}
	body := map[string]any{
		"model":           "clone",
		"input":           text,
		"voice":           voice,
		"response_format": "mp3",
		"speed":           speed,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/audio/speech", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	return req, nil
}

// buildGPTSoVITSRequest GPT-SoVITS 官方 HTTP API：GET /tts?text=...&text_lang=zh&text_id=...
// 自托管部署通常把参考音频固化在服务端（voiceID 对应已绑定的参考音频），这里只传 text 与 text_id。
func (c *cloneProvider) buildGPTSoVITSRequest(ctx context.Context, text, voice string) (*http.Request, error) {
	q := url.Values{}
	q.Set("text", text)
	q.Set("text_lang", "zh")
	if voice != "" {
		q.Set("text_id", voice) // GPT-SoVITS 用 text_id 路由已保存的音色配置
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tts?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "audio/wav")
	return req, nil
}

// ProbeClone 轻量探测克隆服务可用性：合成一句固定短文本。供 server 健康探测/预热。
func ProbeClone(ctx context.Context, cfg Config) error {
	p := newClone(cfg)
	if !p.Available() {
		return ErrCloneNotConfigured
	}
	rc, err := p.Synth(ctx, "你好", SynthOpts{Voice: cfg.CloneVoiceID, Rate: 1.0})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}
