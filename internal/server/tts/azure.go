package tts

// Azure 官方 Speech REST 合成（阶段1预留/可启用）。
// 与 edge-tts 同源音色（zh-CN-*-Neural），但走官方 endpoint + 订阅 key，稳定、不被风控。
// 填了 AzureKey 即纳入多引擎编排：edge 不可用时自动切到这里。未填 key 则跳过。

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type azure struct {
	cfg Config
}

func newAzure(cfg Config) *azure { return &azure{cfg: cfg} }

func (a *azure) Name() string  { return "azure" }
func (a *azure) Format() string { return "mp3" }
func (a *azure) Available() bool { return a.cfg.AzureKey != "" }

// azureEndpoint 拼官方 REST URL。
func (a *azure) endpoint() string {
	region := a.cfg.AzureRegion
	if region == "" {
		region = "eastasia"
	}
	return fmt.Sprintf("https://%s.tts.speech.microsoft.com/cognitiveservices/v1", region)
}

func (a *azure) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("合成文本为空")
	}
	voice := ResolveVoice(opts.Voice, opts.Gender)
	if voice == "" {
		voice = ResolveVoice(a.cfg.Voice, a.cfg.Gender)
	}
	ssml := buildSSML(text, voice, opts)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader([]byte(ssml)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", a.cfg.AzureKey)
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "audio-24khz-48kbitrate-mono-mp3")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: azure 请求失败：%w", ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("%w: azure HTTP %d: %s", ErrUnavailable, resp.StatusCode, string(body))
	}
	return resp.Body, nil
}
