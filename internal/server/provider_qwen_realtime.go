package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// The workbench text/tool bridge uses the Realtime wire protocol. Microphone
// duplex audio and camera streams are separate UI features, not chat payloads.
func completeQwenRealtime(ctx context.Context, cfg Settings, messages []Message, tools []any, rec func([]byte), onDelta func(string)) (string, []ToolCall, TokenUsage, string, error) {
	if len(tools) > 0 && modelCapabilityStatus(cfg, "tool_calls") == "unsupported" {
		return "", nil, TokenUsage{}, "", fmt.Errorf("所选模型已标记为不支持工具调用")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", nil, TokenUsage{}, "", fmt.Errorf("请配置有效的 Realtime 业务空间服务地址")
	}
	h := strings.ToLower(u.Hostname())
	if strings.HasPrefix(cfg.Model, "qwen3.8-") && (h == "dashscope.aliyuncs.com" || h == "dashscope-intl.aliyuncs.com") {
		return "", nil, TokenUsage{}, "", fmt.Errorf("此 Realtime 型号要求业务空间地址：请在提供商设置填写 https://业务空间ID.cn-beijing.maas.aliyuncs.com/compatible-mode/v1（新加坡使用 ap-southeast-1），保留对应地域的 API Key")
	}
	for _, m := range messages {
		if len(m.Images) > 0 {
			return "", nil, TokenUsage{}, "", fmt.Errorf("Realtime 工作台桥接当前支持文本和工具，请用非 realtime 的 Omni 模型处理图片附件")
		}
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api-ws/v1/realtime"
	u.RawQuery = url.Values{"model": []string{cfg.Model}}.Encode()
	ws, resp, err := (&websocket.Dialer{HandshakeTimeout: 10 * time.Second}).DialContext(ctx, u.String(), http.Header{"Authorization": []string{"Bearer " + cfg.APIKey}})
	if err != nil {
		if ctx.Err() != nil {
			return "", nil, TokenUsage{}, "", ctx.Err()
		}
		if resp != nil {
			if resp.Body != nil {
				resp.Body.Close()
			}
			return "", nil, TokenUsage{}, "", fmt.Errorf("Realtime 连接返回 HTTP %d；请检查业务空间、地域、Key 和模型开通状态", resp.StatusCode)
		}
		return "", nil, TokenUsage{}, "", fmt.Errorf("Realtime 连接失败；请检查地址和网络")
	}
	defer ws.Close()
	ws.SetReadLimit(2 << 20)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			ws.Close()
		case <-done:
		}
	}()
	send := func(v any) error { ws.SetWriteDeadline(time.Now().Add(10 * time.Second)); return ws.WriteJSON(v) }
	read := func() (map[string]json.RawMessage, error) {
		ws.SetReadDeadline(time.Now().Add(3 * time.Minute))
		var event map[string]json.RawMessage
		err := ws.ReadJSON(&event)
		return event, err
	}
	kind := func(e map[string]json.RawMessage) string { var s string; _ = json.Unmarshal(e["type"], &s); return s }
	fail := func(err error) (string, []ToolCall, TokenUsage, string, error) {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			err = fmt.Errorf("Realtime 响应中断；未自动重复请求")
		}
		return "", nil, TokenUsage{}, "", err
	}
	for {
		e, err := read()
		if err != nil {
			return fail(err)
		}
		if kind(e) == "error" {
			return "", nil, TokenUsage{}, "", fmt.Errorf("Realtime 会话创建失败，请检查服务状态")
		}
		if kind(e) == "session.created" {
			break
		}
	}
	var instructions []string
	var rtTools []any
	for _, m := range messages {
		if m.Role == "system" {
			instructions = append(instructions, m.Content)
		}
	}
	for _, raw := range tools {
		b, _ := json.Marshal(raw)
		var tool struct {
			Type     string         `json:"type"`
			Function map[string]any `json:"function"`
		}
		if json.Unmarshal(b, &tool) == nil && tool.Type == "function" && tool.Function != nil {
			tool.Function["type"] = "function"
			rtTools = append(rtTools, tool.Function)
		}
	}
	session := map[string]any{"modalities": []string{"text"}, "turn_detection": nil, "instructions": strings.Join(instructions, "\n\n")}
	if len(rtTools) > 0 {
		session["tools"] = rtTools
		session["tool_choice"] = "auto"
	}
	config := map[string]any{"type": "session.update", "session": session}
	if rec != nil {
		b, _ := json.Marshal(map[string]any{"model": cfg.Model, "session": session, "messages": outgoingMessages(messages)})
		rec(b)
	}
	if err := send(config); err != nil {
		return fail(err)
	}
	for {
		e, err := read()
		if err != nil {
			return fail(err)
		}
		if kind(e) == "error" {
			return "", nil, TokenUsage{}, "", fmt.Errorf("Realtime 配置不被模型接受，请检查模型支持的输出模态和工具")
		}
		if kind(e) == "session.updated" {
			break
		}
	}
	for _, m := range messages {
		var item map[string]any
		switch m.Role {
		case "user", "assistant":
			if strings.TrimSpace(m.Content) != "" {
				part := "input_text"
				if m.Role == "assistant" {
					part = "text"
				}
				item = map[string]any{"type": "message", "role": m.Role, "content": []any{map[string]any{"type": part, "text": m.Content}}}
			}
		case "tool":
			item = map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content}
		}
		if item != nil {
			if err := send(map[string]any{"type": "conversation.item.create", "item": item}); err != nil {
				return fail(err)
			}
		}
		for _, call := range m.ToolCalls {
			if err := send(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments}}); err != nil {
				return fail(err)
			}
		}
	}
	if err := send(map[string]any{"type": "response.create"}); err != nil {
		return fail(err)
	}
	var text strings.Builder
	for {
		e, err := read()
		if err != nil {
			_, _, _, _, e2 := fail(err)
			return text.String(), nil, TokenUsage{}, "", e2
		}
		switch kind(e) {
		case "error":
			return text.String(), nil, TokenUsage{}, "", fmt.Errorf("Realtime 模型返回错误；请检查服务状态，未自动重复请求")
		case "response.text.delta", "response.audio_transcript.delta":
			var delta string
			_ = json.Unmarshal(e["delta"], &delta)
			if text.Len()+len(delta) > 2<<20 {
				return text.String(), nil, TokenUsage{}, "", fmt.Errorf("Realtime 文本超过大小限制")
			}
			text.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		case "response.done":
			var response struct {
				Status string `json:"status"`
				Output []struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"output"`
				Usage struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
					Total  int `json:"total_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(e["response"], &response) != nil || response.Status != "completed" {
				return text.String(), nil, TokenUsage{}, "", fmt.Errorf("Realtime 本轮未完成，未自动重复请求")
			}
			var calls []ToolCall
			for _, item := range response.Output {
				if item.Type == "function_call" {
					call := ToolCall{ID: item.CallID, Type: "function"}
					call.Function.Name = item.Name
					call.Function.Arguments = item.Arguments
					calls = append(calls, call)
				}
			}
			usage := TokenUsage{Prompt: response.Usage.Input, Completion: response.Usage.Output, Total: response.Usage.Total, Model: cfg.Model, Provider: cfg.BaseURL}
			if text.Len() == 0 && len(calls) == 0 {
				return "", nil, usage, "", fmt.Errorf("Realtime 没有返回文本或工具调用，未自动重复请求")
			}
			return text.String(), calls, usage, "stop", nil
		}
	}
}
