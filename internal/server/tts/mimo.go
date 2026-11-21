package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const mimoDefaultBaseURL = "https://api.xiaomimimo.com/v1"

func MiMoVoices() []Voice {
	return []Voice{
		{ID: "mimo_default", Name: "默认音色"}, {ID: "冰糖", Name: "冰糖"},
		{ID: "茉莉", Name: "茉莉"}, {ID: "苏打", Name: "苏打"}, {ID: "白桦", Name: "白桦"},
		{ID: "Mia", Name: "Mia"}, {ID: "Chloe", Name: "Chloe"}, {ID: "Milo", Name: "Milo"}, {ID: "Dean", Name: "Dean"},
	}
}

func IsMiMoTTSModel(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.HasPrefix(id, "mimo-") && strings.Contains(id, "-tts") && !strings.Contains(id, "realtime") && !strings.Contains(id, "voiceclone")
}

type mimoProvider struct {
	cfg    Config
	client *http.Client
}

func newMiMo(cfg Config) *mimoProvider {
	return &mimoProvider{cfg: cfg, client: &http.Client{Timeout: 75 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (*mimoProvider) Name() string      { return "mimo" }
func (*mimoProvider) Format() string    { return "wav" }
func (p *mimoProvider) Available() bool { return strings.TrimSpace(p.cfg.APIKey) != "" }

func (p *mimoProvider) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	if !p.Available() {
		return nil, errors.New("请在模型设置中选择已配置的 MiMo API 密钥")
	}
	model := strings.TrimSpace(p.cfg.Model)
	if !IsMiMoTTSModel(model) {
		return nil, errors.New("请选择已发现的 MiMo TTS 模型")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(text)); n == 0 || n > 4096 {
		return nil, errors.New("MiMo TTS 每段需为 1–4096 个字符")
	}
	base, err := MiMoBaseURL(p.cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = "mimo_default"
	}
	messages := make([]map[string]string, 0, 2)
	if model == "mimo-v2.5-tts-voicedesign" {
		style := strings.TrimSpace(opts.Style)
		if style == "" {
			return nil, errors.New("请填写音色角色描述，例如温和、清晰、语速稍慢")
		}
		messages = append(messages, map[string]string{"role": "user", "content": style})
	} else if style := strings.TrimSpace(opts.Style); style != "" {
		messages = append(messages, map[string]string{"role": "user", "content": style})
	}
	messages = append(messages, map[string]string{"role": "assistant", "content": text})
	body, _ := json.Marshal(map[string]any{"model": model, "messages": messages, "audio": map[string]any{"format": "wav", "voice": voice}, "stream": false})
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("MiMo TTS 请求创建失败")
	}
	req.Header.Set("api-key", p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: MiMo TTS 连接失败或超时", ErrUnavailable)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MiMo TTS HTTP %d，请检查密钥、模型权限与额度", res.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&result); err != nil || len(result.Choices) == 0 {
		return nil, errors.New("MiMo TTS 返回无效音频响应")
	}
	encoded := result.Choices[0].Message.Audio.Data
	if encoded == "" {
		return nil, errors.New("MiMo TTS 响应中没有音频数据")
	}
	audio, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(audio) == 0 || len(audio) > 48<<20 {
		return nil, errors.New("MiMo TTS 返回的音频数据无效或过大")
	}
	return io.NopCloser(bytes.NewReader(audio)), nil
}

func IsMiMoHost(host string) bool {
	switch strings.ToLower(host) {
	case "api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com", "token-plan-sgp.xiaomimimo.com", "token-plan-ams.xiaomimimo.com":
		return true
	}
	return false
}

func MiMoBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = mimoDefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || !IsMiMoHost(u.Hostname()) {
		return nil, errors.New("MiMo TTS 需要小米官方 HTTPS API 地址")
	}
	if u.Path == "" {
		u.Path = "/v1"
	}
	if strings.TrimRight(u.Path, "/") != "/v1" {
		return nil, errors.New("MiMo TTS API 地址需使用 /v1 基础路径")
	}
	return u, nil
}
