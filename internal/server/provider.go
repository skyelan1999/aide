package server

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
	"sync/atomic"
	"time"
)

// TokenUsage 一次模型调用的用量（FR-90）；Estimated 表示上游未返回 usage 时的估算值。
type TokenUsage struct {
	Prompt     int    `json:"prompt"`
	Completion int    `json:"completion"`
	Total      int    `json:"total"`
	Estimated  bool   `json:"estimated,omitempty"`
	Model      string `json:"model,omitempty"`
	Provider   string `json:"provider,omitempty"` // R08：调用归属的 provider（baseURL 快照）
}

// tokenUsageRecorder 由 New() 注入（atomic 防并行测试竞态）；complete() 成功后调用。
var tokenUsageRecorder atomic.Value // func(TokenUsage)

// The provider boundary is intentionally small: any Chat Completions compatible
// endpoint can be used, including a local model through host.docker.internal.
// params 是本次任务的采样参数（FR-61），未设置字段不进入请求体；
// deepseek-reasoner 不支持的参数会被剔除，避免上游 400。
// rec 为可选的请求体记录回调（R08-04 请求快照）；在真正发出前以已序列化字节调用。
func complete(ctx context.Context, cfg Settings, messages []Message, params ProfileParams, tools []any, rec func(body []byte)) (string, []ToolCall, TokenUsage, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", nil, TokenUsage{}, errors.New("请先在模型设置中配置 API 地址和模型")
	}
	body := map[string]any{"model": cfg.Model, "messages": messages, "stream": false}
	promptChars := 0
	for _, m := range messages {
		promptChars += len(m.Content)
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
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
		return "", nil, TokenUsage{}, err
	}
	if rec != nil {
		rec(b)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", nil, TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, TokenUsage{}, fmt.Errorf("模型连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil, TokenUsage{}, fmt.Errorf("模型 API 返回 HTTP %d；请检查地址、模型、密钥和额度", resp.StatusCode)
	}
	b, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", nil, TokenUsage{}, err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err = json.Unmarshal(b, &out); err != nil {
		return "", nil, TokenUsage{}, errors.New("模型返回了无效 JSON")
	}
	if len(out.Choices) == 0 {
		return "", nil, TokenUsage{}, errors.New("模型没有返回内容")
	}
	msg := out.Choices[0].Message
	if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
		return "", nil, TokenUsage{}, errors.New("模型没有返回文本内容")
	}
	usage := TokenUsage{Prompt: out.Usage.PromptTokens, Completion: out.Usage.CompletionTokens, Total: out.Usage.TotalTokens, Model: cfg.Model, Provider: cfg.BaseURL}
	if usage.Total == 0 {
		// 上游未返回 usage → 4 字符/词估算并标记
		usage.Prompt = promptChars / 4
		usage.Completion = len(msg.Content) / 4
		usage.Total = usage.Prompt + usage.Completion
		usage.Estimated = true
	}
	if rec := tokenUsageRecorder.Load(); rec != nil {
		rec.(func(TokenUsage))(usage)
	}
	return msg.Content, msg.ToolCalls, usage, nil
}

// listModels 代理 GET {baseURL}/models 拉取可用模型 id 列表（FR-68 / LIM-25）。
// 返回 OpenAI 兼容格式 {models:[id,…]}；上游失败或格式不符时给出友好错误。
func (a *App) listModels(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	baseURL, key := a.settings.BaseURL, a.settings.APIKey
	a.mu.Unlock()
	if r.Method == http.MethodPost {
		var in struct {
			BaseURL  string `json:"baseURL"`
			APIKey   string `json:"apiKey"`
			ClearKey bool   `json:"clearKey"`
		}
		if err := decode(w, r, &in); err != nil {
			fail(w, 400, err)
			return
		}
		requested := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if requested != strings.TrimRight(baseURL, "/") || in.ClearKey {
			key = ""
		}
		baseURL = requested
		if !in.ClearKey && strings.TrimSpace(in.APIKey) != "" {
			key = strings.TrimSpace(in.APIKey)
		}
	}
	u, parseErr := url.Parse(baseURL)
	if parseErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		fail(w, 400, errors.New("请输入有效的 HTTP(S) API Base URL"))
		return
	}
	if baseURL == "" {
		fail(w, 400, errors.New("请先在模型设置中填写 API Base URL"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		fail(w, 400, fmt.Errorf("获取模型列表失败: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 400, fmt.Errorf("模型列表接口返回 HTTP %d（该服务可能不支持模型列表）", resp.StatusCode))
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		fail(w, 400, errors.New("模型列表返回格式无法解析"))
		return
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		fail(w, 400, errors.New("未获取到模型列表"))
		return
	}
	jsonOut(w, 200, map[string]any{"models": ids})
}

// listBalance 代理 GET {baseURL}/user/balance 查询账户余额（DeepSeek 官方接口）。
func (a *App) listBalance(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	baseURL, key := a.settings.BaseURL, a.settings.APIKey
	a.mu.Unlock()
	if baseURL == "" {
		fail(w, 400, errors.New("请先在模型设置中填写 API Base URL"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/user/balance", nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		fail(w, 400, fmt.Errorf("余额查询失败: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 400, fmt.Errorf("余额接口返回 HTTP %d（该服务可能不支持余额查询）", resp.StatusCode))
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		fail(w, 400, errors.New("余额返回格式无法解析"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
