package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func usesOpenAIResponses(cfg Settings) bool {
	return isOpenAIEndpoint(cfg.BaseURL) && strings.HasPrefix(strings.ToLower(cfg.Model), "gpt-6")
}

func providerAssistantMessage(cfg Settings, text string, calls []ToolCall, usage TokenUsage) Message {
	m := Message{Role: "assistant", Content: text, ToolCalls: calls}
	if usesOpenAIResponses(cfg) && usage.responseOutput != nil {
		m.ResponseItems = usage.responseOutput.Output
		m.ResponseModel = cfg.Model
	}
	return m
}

func buildResponsesBody(cfg Settings, messages []Message, params ProfileParams, tools []any, stream bool) map[string]any {
	input := []any{}
	for _, m := range messages {
		if m.Role == "assistant" && m.ResponseModel == cfg.Model && len(m.ResponseItems) > 0 {
			for _, item := range m.ResponseItems {
				input = append(input, item)
			}
			continue
		}
		if m.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if m.Content != "" || len(m.Images) > 0 {
			if len(m.Images) == 0 {
				input = append(input, map[string]any{"role": m.Role, "content": m.Content})
			} else {
				parts := []any{map[string]any{"type": "input_text", "text": m.Content}}
				for _, image := range m.Images {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": image.DataURL})
				}
				input = append(input, map[string]any{"role": m.Role, "content": parts})
			}
		}
		for _, call := range m.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
		}
	}
	body := map[string]any{"model": cfg.Model, "input": input, "stream": stream, "store": false, "include": []string{"reasoning.encrypted_content"}}
	if modelCapabilityStatus(cfg, "reasoning") != "unsupported" {
		effort := cfg.ReasoningEffort
		if effort == "off" {
			effort = "none"
			if openAIRequiresReasoning(cfg.Model) {
				effort = "low"
			}
		}
		if effort != "auto" && effort != "" {
			body["reasoning"] = map[string]any{"effort": effort}
		}
	}
	if params.MaxTokens > 0 {
		body["max_output_tokens"] = params.MaxTokens
	}
	if params.ResponseFormat != "" && modelCapabilityStatus(cfg, "structured_output") != "unsupported" {
		body["text"] = map[string]any{"format": map[string]any{"type": params.ResponseFormat}}
	}
	if modelCapabilityStatus(cfg, "tool_calls") != "unsupported" {
		functions := []any{}
		for _, tool := range tools {
			encoded, _ := json.Marshal(tool)
			var wrapped struct {
				Type     string         `json:"type"`
				Function map[string]any `json:"function"`
			}
			if json.Unmarshal(encoded, &wrapped) == nil && wrapped.Type == "function" && wrapped.Function != nil {
				wrapped.Function["type"] = "function"
				wrapped.Function["strict"] = false
				functions = append(functions, wrapped.Function)
			}
		}
		if len(functions) > 0 {
			body["tools"] = functions
		}
	}
	return body
}

type openAIResponse struct {
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Usage  struct {
		Input        int `json:"input_tokens"`
		Output       int `json:"output_tokens"`
		Total        int `json:"total_tokens"`
		InputDetails struct {
			Cached int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			Reasoning int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func (r *openAIResponse) chatResult() (string, []ToolCall, string, providerUsage) {
	var text strings.Builder
	calls := []ToolCall{}
	for _, raw := range r.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if item.Type == "function_call" {
			call := ToolCall{ID: item.CallID, Type: "function"}
			call.Function.Name, call.Function.Arguments = item.Name, item.Arguments
			calls = append(calls, call)
		}
		for _, content := range item.Content {
			text.WriteString(content.Text)
			text.WriteString(content.Refusal)
		}
	}
	finish := "stop"
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	if r.Status == "incomplete" {
		finish = "length"
	}
	u := providerUsage{PromptTokens: r.Usage.Input, CompletionTokens: r.Usage.Output, TotalTokens: r.Usage.Total, ResponseOutput: r}
	u.PromptDetails.CachedTokens = &r.Usage.InputDetails.Cached
	u.CompletionDetails.ReasoningTokens = &r.Usage.OutputDetails.Reasoning
	return text.String(), calls, finish, u
}

// Normalize at the provider boundary so existing streaming cancellation,
// usage accounting and tool execution keep the same internal contract.
func normalizeOpenAIResponse(resp *http.Response, stream bool) error {
	original := resp.Body
	if !stream || !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		defer original.Close()
		var out openAIResponse
		if err := json.NewDecoder(io.LimitReader(original, 8<<20)).Decode(&out); err != nil {
			return err
		}
		if out.Status != "completed" && out.Status != "incomplete" {
			return errors.New("OpenAI response did not complete")
		}
		text, calls, finish, usage := out.chatResult()
		b, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text, "tool_calls": calls}, "finish_reason": finish}}, "usage": usage})
		if err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(b))
		resp.Header.Set("Content-Type", "application/json")
		return nil
	}
	reader, writer := io.Pipe()
	resp.Body = &responsesBody{ReadCloser: reader, upstream: original}
	go func() {
		defer original.Close()
		err := translateResponsesStream(original, writer)
		_ = writer.CloseWithError(err)
	}()
	return nil
}

type responsesBody struct {
	io.ReadCloser
	upstream io.ReadCloser
}

func (b *responsesBody) Close() error { _ = b.upstream.Close(); return b.ReadCloser.Close() }

func translateResponsesStream(src io.Reader, dst io.Writer) error {
	scanner := bufio.NewScanner(io.LimitReader(src, (8<<20)+1))
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	emit := func(delta map[string]any, finish string, usage any) error {
		choice := map[string]any{"delta": delta}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		chunk := map[string]any{"choices": []any{choice}}
		if usage != nil {
			chunk["usage"] = usage
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(dst, "data: %s\n\n", b)
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Index int    `json:"output_index"`
			Item  struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			} `json:"item"`
			Response openAIResponse `json:"response"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return err
		}
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if err := emit(map[string]any{"content": event.Delta}, "", nil); err != nil {
				return err
			}
		case "response.reasoning_summary_text.delta":
			if err := emit(map[string]any{"reasoning_content": event.Delta}, "", nil); err != nil {
				return err
			}
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				if err := emit(map[string]any{"tool_calls": []any{map[string]any{"index": event.Index, "id": event.Item.CallID, "type": "function", "function": map[string]any{"name": event.Item.Name, "arguments": ""}}}}, "", nil); err != nil {
					return err
				}
			}
		case "response.function_call_arguments.delta":
			if err := emit(map[string]any{"tool_calls": []any{map[string]any{"index": event.Index, "function": map[string]any{"arguments": event.Delta}}}}, "", nil); err != nil {
				return err
			}
		case "response.completed", "response.incomplete":
			_, _, finish, usage := event.Response.chatResult()
			if err := emit(map[string]any{}, finish, usage); err != nil {
				return err
			}
			_, err := io.WriteString(dst, "data: [DONE]\n\n")
			return err
		case "error", "response.failed":
			return errors.New("OpenAI Responses stream failed")
		default:
			if _, err := io.WriteString(dst, ": activity\n\n"); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
