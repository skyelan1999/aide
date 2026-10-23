package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Qwen Image 3.0 uses the synchronous OpenAI Images API, not Chat Completions.
// Keep the configured provider endpoint and key; never guess a different region.
func isQwenImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "qwen-image-3.0" || strings.HasPrefix(model, "qwen-image-3.0-") || strings.HasPrefix(model, "qwen-image-2.1-")
}

func generateQwenImage(ctx context.Context, cfg Settings, prompt string, rec func([]byte)) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("请填写图片描述")
	}
	endpoint, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", fmt.Errorf("请配置有效的图像模型 API 基础地址")
	}
	wire := map[string]any{"model": cfg.Model, "prompt": prompt, "n": 1, "size": "1024x1024"}
	host := strings.ToLower(endpoint.Hostname())
	native := host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com" || host == "dashscope-us.aliyuncs.com" || strings.HasPrefix(endpoint.Path, "/api/v1")
	if native {
		endpoint.Path = "/api/v1/services/aigc/multimodal-generation/generation"
		wire = map[string]any{"model": cfg.Model, "input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]string{"text": prompt}}}}}, "parameters": map[string]any{"n": 1, "size": "1024*1024"}}
	} else {
		endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/images/generations"
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	if rec != nil {
		rec(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("图像模型连接失败；请检查基础地址和网络后手动重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", fmt.Errorf("图像 API 返回 HTTP 429：请求受限。请检查百炼模型限流、模型开通状态及所选 Key；稍后手动重试，本次未自动重复生图")
		}
		return "", fmt.Errorf("图像 API 返回 HTTP %d；请检查模型、基础地址、密钥和服务状态。本次未自动重复生图", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return "", fmt.Errorf("图像 API 响应读取失败或超过大小限制；未自动重复生图")
	}
	var result struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", fmt.Errorf("图像 API 没有返回有效图片；未自动重复生图")
	}
	var urls []string
	for _, item := range result.Data {
		urls = append(urls, item.URL)
	}
	for _, choice := range result.Output.Choices {
		for _, part := range choice.Message.Content {
			if part.Image != "" {
				urls = append(urls, part.Image)
			}
		}
	}
	if len(urls) == 0 {
		return "", fmt.Errorf("图像 API 没有返回有效图片；未自动重复生图")
	}
	var output []string
	for _, item := range urls {
		u, err := url.Parse(item)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return "", fmt.Errorf("图像 API 返回了无效图片地址；未自动重复生图")
		}
		link := strings.NewReplacer("<", "%3C", ">", "%3E").Replace(u.String())
		output = append(output, "![生成图片](<"+link+">)")
	}
	return strings.Join(output, "\n\n") + "\n\n图片链接可能限时有效，请及时保存。", nil
}

// A dedicated single-shot path prevents title/review/retry/parallel chat calls
// from creating duplicate billable images. Image counts are not token counts.
func (a *App) imageChat(ctx context.Context, cfg Settings, input []Message, task *Task, stepIndex int) (string, []Message, error) {
	if task.Mode != "chat" {
		return "", input, fmt.Errorf("图像模型仅支持对话模式文生图，请切换到对话；工作流和工具任务请使用文本模型")
	}
	if len(task.Attachments) > 0 {
		return "", input, fmt.Errorf("当前图像适配仅支持文字生成图片，请先移除附件")
	}
	for _, message := range input {
		if len(message.Images) > 0 {
			return "", input, fmt.Errorf("当前图像适配仅支持文字生成图片，暂不支持参考图编辑")
		}
	}
	a.publishStream(task.ID, streamEvent{Event: "note", Text: "正在生成图片…", Round: 0})
	out, err := generateQwenImage(ctx, cfg, task.Prompt, func(body []byte) {
		sum := sha256.Sum256(body)
		a.mu.Lock()
		defer a.mu.Unlock()
		task.RequestSnapshots = append(task.RequestSnapshots, RequestSnapshot{Purpose: "image#1", Model: cfg.Model, Messages: []Message{{Role: "user", Content: task.Prompt}}, Body: append(json.RawMessage{}, body...), SHA256: hex.EncodeToString(sum[:]), At: time.Now().UTC().Format(time.RFC3339Nano)})
	})
	if err != nil {
		return "", input, err
	}
	if saved, saveErr := a.saveQwenImageAsset(ctx, out); saveErr == nil {
		out = saved
	} else {
		out += "\n\n图片未能自动保存，可使用原始链接下载；请勿为下载而重复生图。"
	}
	if err := a.saveOutputSegment(task, stepIndex, 0, out); err != nil {
		return out, input, err
	}
	a.publishStream(task.ID, streamEvent{Event: "delta", Text: out, Round: 0})
	return out, input, nil
}
