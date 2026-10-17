package tts

// Qwen's native DashScope API returns a signed WAV URL, not OpenAI audio/speech.
// See https://help.aliyun.com/zh/model-studio/qwen-tts-api.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type qwenProvider struct {
	cfg    Config
	client *http.Client
}

func newQwen(cfg Config) *qwenProvider {
	return &qwenProvider{cfg: cfg, client: &http.Client{Timeout: 45 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (*qwenProvider) Name() string      { return "qwen" }
func (*qwenProvider) Format() string    { return "wav" }
func (q *qwenProvider) Available() bool { return strings.TrimSpace(q.cfg.APIKey) != "" }

func (q *qwenProvider) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	if !q.Available() {
		return nil, errors.New("请先配置百炼 TTS API Key")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(text)); n == 0 || n > 600 {
		return nil, errors.New("千问 TTS 每段需为 1–600 个字符")
	}
	host := "dashscope.aliyuncs.com"
	switch q.cfg.Region {
	case "", "beijing":
	case "singapore":
		host = "dashscope-intl.aliyuncs.com"
	default:
		return nil, errors.New("不支持的百炼地域")
	}
	model := strings.TrimSpace(q.cfg.Model)
	if model == "" {
		model = "qwen3-tts-flash"
	}
	if strings.Contains(model, "realtime") {
		return nil, errors.New("请选择非实时 Qwen-TTS 模型")
	}
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = "Cherry"
	}
	b, _ := json.Marshal(map[string]any{"model": model, "input": map[string]string{
		"text": text, "voice": voice, "language_type": "Auto",
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/api/v1/services/aigc/multimodal-generation/generation", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+q.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: 百炼合成连接失败或超时", ErrUnavailable)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&failure)
		code := regexp.MustCompile(`[^a-zA-Z0-9_.-]`).ReplaceAllString(failure.Code, "")
		if len(code) > 80 {
			code = code[:80]
		}
		return nil, fmt.Errorf("百炼合成 HTTP %d %s，请检查地域、密钥、配额与模型权限", res.StatusCode, code)
	}
	var output struct {
		Code   string `json:"code"`
		Output struct {
			Audio struct {
				URL string `json:"url"`
			} `json:"audio"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&output); err != nil || output.Code != "" {
		return nil, errors.New("百炼 TTS 返回无效音频响应")
	}
	u, err := qwenAudioURL(output.Output.Audio.URL)
	if err != nil {
		return nil, err
	}
	download, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Never forward the API key to the signed audio URL or follow redirects.
	audio, err := q.client.Do(download)
	if err != nil {
		return nil, fmt.Errorf("%w: 百炼音频下载失败", ErrUnavailable)
	}
	if audio.StatusCode != http.StatusOK || audio.ContentLength == 0 {
		audio.Body.Close()
		return nil, fmt.Errorf("百炼音频下载 HTTP %d，合成已成功，下载失败", audio.StatusCode)
	}
	return audio.Body, nil
}

func qwenAudioURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("百炼返回了无效的音频地址")
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasPrefix(host, "dashscope-result-") || !strings.Contains(host, ".oss-") || !strings.HasSuffix(host, ".aliyuncs.com") {
		return "", errors.New("百炼返回了非受信的音频地址")
	}
	// Official examples use HTTP signed URLs; fetch them over TLS instead.
	u.Scheme = "https"
	return u.String(), nil
}
