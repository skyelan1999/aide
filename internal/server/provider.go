package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The provider boundary is intentionally small: any Chat Completions compatible
// endpoint can be used, including a local model through host.docker.internal.
// params 是本次任务的采样参数（FR-61），未设置字段不进入请求体；
// deepseek-reasoner 不支持的参数会被剔除，避免上游 400。
func complete(ctx context.Context, cfg Settings, messages []Message, params ProfileParams) (string, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", errors.New("请先在模型设置中配置 API 地址和模型")
	}
	body := map[string]any{"model": cfg.Model, "messages": messages, "stream": false}
	if params.Temperature != nil {
		body["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		body["top_p"] = *params.TopP
	}
	if params.MaxTokens != 0 {
		body["max_tokens"] = params.MaxTokens
	}
	if params.FrequencyPenalty != nil {
		body["frequency_penalty"] = *params.FrequencyPenalty
	}
	if params.PresencePenalty != nil {
		body["presence_penalty"] = *params.PresencePenalty
	}
	if params.ResponseFormat != "" {
		body["response_format"] = map[string]string{"type": params.ResponseFormat}
	}
	if len(params.Stop) > 0 {
		body["stop"] = params.Stop
	}
	if strings.Contains(strings.ToLower(cfg.Model), "reasoner") {
		delete(body, "temperature")
		delete(body, "top_p")
		delete(body, "frequency_penalty")
		delete(body, "presence_penalty")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("模型连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("模型 API 返回 HTTP %d；请检查地址、模型、密钥和额度", resp.StatusCode)
	}
	b, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(b, &out); err != nil {
		return "", errors.New("模型返回了无效 JSON")
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("模型没有返回文本内容")
	}
	return out.Choices[0].Message.Content, nil
}
