package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type responsesTestTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t responsesTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = t.target.Scheme, t.target.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}

func TestOpenAIResponsesToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/v1/responses" {
					t.Errorf("wrong endpoint %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["thinking"] != nil || body["reasoning_effort"] != nil || body["messages"] != nil || body["store"] != false {
					t.Error("invalid Responses request", body)
				}
				if body["reasoning"].(map[string]any)["effort"] != "high" {
					t.Error("intensity lost")
				}
				functions := body["tools"].([]any)
				if functions[0].(map[string]any)["name"] != "read_file" || functions[0].(map[string]any)["strict"] != false {
					t.Error("function mapping failed")
				}
				output := `[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"cipher"},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{}","status":"completed"}]`
				if requests == 2 {
					encoded, _ := json.Marshal(body["input"])
					if !bytes.Contains(encoded, []byte(`"encrypted_content":"cipher"`)) || !bytes.Contains(encoded, []byte(`"type":"function_call_output"`)) {
						t.Error("tool continuation lost", string(encoded))
					}
					output = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]`
				}
				result := `{"status":"completed","output":` + output + `,"usage":{"input_tokens":20,"output_tokens":5,"total_tokens":25,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, result)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if requests == 1 {
					io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"read_file\"}}\n\n")
					io.WriteString(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"delta\":\"{}\"}\n\n")
				} else {
					io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n")
				}
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":"+result+"}\n\n")
			}))
			defer srv.Close()
			target, _ := url.Parse(srv.URL)
			original := http.DefaultTransport
			http.DefaultTransport = responsesTestTransport{target, original}
			defer func() { http.DefaultTransport = original }()
			cfg := Settings{BaseURL: "https://api.openai.com/v1", Model: "gpt-6-luna", ReasoningEffort: "high"}
			messages := []Message{{Role: "user", Content: "test"}}
			tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}}
			run := func() (string, []ToolCall, TokenUsage, error) {
				if stream {
					out, calls, u, _, err := completeStream(context.Background(), cfg, messages, ProfileParams{}, tools, nil, nil, nil)
					return out, calls, u, err
				}
				return complete(context.Background(), cfg, messages, ProfileParams{}, tools, nil)
			}
			out, calls, usage, err := run()
			if err != nil || len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Arguments != "{}" {
				t.Fatalf("tool round failed: %v %v", calls, err)
			}
			if usage.Total != 25 || usage.CacheHit != 3 || !usage.Reasoning {
				t.Fatal("usage lost", usage)
			}
			assistant := providerAssistantMessage(cfg, out, calls, usage)
			encoded, _ := json.Marshal(outgoingMessages([]Message{assistant}))
			if bytes.Contains(encoded, []byte("cipher")) || bytes.Contains(encoded, []byte("responseItems")) {
				t.Fatal("provider state leaked to Chat")
			}
			messages = append(messages, assistant, Message{Role: "tool", ToolCallID: "call_1", Content: "file contents"})
			out, calls, _, err = run()
			if err != nil || out != "OK" || len(calls) != 0 {
				t.Fatalf("final reply %q calls=%v err=%v", out, calls, err)
			}
		})
	}
}

func TestOpenAIResponsesTruncatedStream(t *testing.T) {
	var dst bytes.Buffer
	err := translateResponsesStream(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"), &dst)
	if err == nil || strings.Contains(dst.String(), "[DONE]") {
		t.Fatal("truncated stream treated as success", err)
	}
}
