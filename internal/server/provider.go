package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
// buildChatBody 构造 Chat Completions 请求体；stream 控制是否要求上游流式返回。
// deepseek-reasoner 不支持的采样参数会被剔除，避免上游 400。
func buildChatBody(cfg Settings, messages []Message, params ProfileParams, tools []any, stream bool) map[string]any {
	body := map[string]any{"model": cfg.Model, "messages": messages, "stream": stream}
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
	if stream {
		// 多数 OpenAI 兼容端点通过该字段在最后一个 chunk 返回 usage；不支持的端点忽略它。
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body
}

func complete(ctx context.Context, cfg Settings, messages []Message, params ProfileParams, tools []any, rec func(body []byte)) (string, []ToolCall, TokenUsage, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", nil, TokenUsage{}, errors.New("请先在模型设置中配置 API 地址和模型")
	}
	body := buildChatBody(cfg, messages, params, tools, false)
	promptChars := 0
	for _, m := range messages {
		promptChars += len(m.Content)
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

// streamChunk 是上游 SSE 每个 data 行解析后的结构（流式专用）。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// completeStream 以 stream:true 发起 Chat Completions，逐 token 聚合响应。
// onDelta 在收到文本增量时被同步调用（不写网络、不加锁即可）；它把增量交给上层推给 SSE 订阅者。
// 返回值与 complete() 一致：最终文本、工具调用、用量、错误。非流式响应或上游不支持 SSE 时自动回退。
// 部分兼容网关会拒绝 stream_options（OpenAI 可选扩展）：HTTP 400 时去掉该字段重试一次，仍失败则按错误返回。
func completeStream(ctx context.Context, cfg Settings, messages []Message, params ProfileParams, tools []any, rec func(body []byte), onDelta func(textDelta string)) (string, []ToolCall, TokenUsage, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", nil, TokenUsage{}, errors.New("请先在模型设置中配置 API 地址和模型")
	}
	body := buildChatBody(cfg, messages, params, tools, true)
	promptChars := 0
	for _, m := range messages {
		promptChars += len(m.Content)
	}
	send := func(bodyBytes []byte) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		client := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		return client.Do(req)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", nil, TokenUsage{}, err
	}
	if rec != nil {
		rec(b)
	}
	resp, err := send(b)
	if err != nil {
		return "", nil, TokenUsage{}, fmt.Errorf("模型连接失败: %w", err)
	}
	if resp.StatusCode == 400 {
		// 兼容性重试：去掉 stream_options 再试一次（只重试 400，且只此一次）。
		resp.Body.Close()
		delete(body, "stream_options")
		b, err = json.Marshal(body)
		if err != nil {
			return "", nil, TokenUsage{}, err
		}
		if rec != nil {
			rec(b) // 请求快照记录最终生效的请求体
		}
		resp, err = send(b)
		if err != nil {
			return "", nil, TokenUsage{}, fmt.Errorf("模型连接失败: %w", err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		detail := strings.TrimSpace(string(rb))
		if len(detail) > 300 {
			detail = detail[:300] + "…"
		}
		return "", nil, TokenUsage{}, fmt.Errorf("模型 API 返回 HTTP %d：%s", resp.StatusCode, detail)
	}
	// 兼容：上游声称流式但实际返回完整 JSON（少数网关如此）时，回退到非流式解析。
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "event-stream") {
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
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
			return "", nil, TokenUsage{}, errors.New("模型返回了无效 JSON")
		}
		if len(out.Choices) == 0 {
			return "", nil, TokenUsage{}, errors.New("模型没有返回内容")
		}
		msg := out.Choices[0].Message
		if onDelta != nil && msg.Content != "" {
			onDelta(msg.Content)
		}
		usage := TokenUsage{Prompt: out.Usage.PromptTokens, Completion: out.Usage.CompletionTokens, Total: out.Usage.TotalTokens, Model: cfg.Model, Provider: cfg.BaseURL}
		if usage.Total == 0 {
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

	var content strings.Builder
	toolCallsByIndex := map[int]*ToolCall{}
	var usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20) // 单个 chunk 上限 1 MiB
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			// 用户主动停止：保留已流式输出的部分内容
			return content.String(), nil, TokenUsage{}, ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue // 心跳/注释行
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 忽略无法解析的 keepalive 行
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			slot, ok := toolCallsByIndex[tc.Index]
			if !ok {
				slot = &ToolCall{}
				toolCallsByIndex[tc.Index] = slot
			}
			if tc.ID != "" {
				slot.ID = tc.ID
			}
			if tc.Type != "" {
				slot.Type = tc.Type
			}
			if tc.Function.Name != "" {
				slot.Function.Name = tc.Function.Name
			}
			slot.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return "", nil, TokenUsage{}, ctx.Err()
		}
		return "", nil, TokenUsage{}, fmt.Errorf("读取模型流式响应失败: %w", err)
	}
	// 按 index 顺序还原 tool_calls
	callIndexes := make([]int, 0, len(toolCallsByIndex))
	for i := range toolCallsByIndex {
		callIndexes = append(callIndexes, i)
	}
	sort.Ints(callIndexes)
	calls := make([]ToolCall, 0, len(callIndexes))
	for _, i := range callIndexes {
		calls = append(calls, *toolCallsByIndex[i])
	}
	text := content.String()
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return "", nil, TokenUsage{}, errors.New("模型没有返回文本内容")
	}
	tu := TokenUsage{Model: cfg.Model, Provider: cfg.BaseURL}
	if usage != nil {
		tu.Prompt = usage.PromptTokens
		tu.Completion = usage.CompletionTokens
		tu.Total = usage.TotalTokens
	}
	if tu.Total == 0 {
		tu.Prompt = promptChars / 4
		tu.Completion = len(text) / 4
		tu.Total = tu.Prompt + tu.Completion
		tu.Estimated = true
	}
	if rec := tokenUsageRecorder.Load(); rec != nil {
		rec.(func(TokenUsage))(tu)
	}
	return text, calls, tu, nil
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
