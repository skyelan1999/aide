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
func complete(ctx context.Context, cfg Settings, messages []Message) (string, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", errors.New("请先在模型设置中配置 API 地址和模型")
	}
	b, err := json.Marshal(map[string]any{"model": cfg.Model, "messages": messages, "stream": false})
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
