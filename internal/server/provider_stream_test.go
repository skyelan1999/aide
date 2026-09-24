package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompleteStreamTextDeltas(t *testing.T) {
	// 模拟 OpenAI 兼容 SSE：逐 chunk 推 content，最后 [DONE]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"你好"}}]}`,
			`{"choices":[{"delta":{"content":"，"}}]}`,
			`{"choices":[{"delta":{"content":"世界"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			fl.Flush()
			time.Sleep(time.Millisecond)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	var deltas []string
	cfg := Settings{BaseURL: srv.URL, Model: "test-model"}
	text, calls, usage, err := completeStream(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}, ProfileParams{}, nil, nil, func(d string) { deltas = append(deltas, d) }, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if text != "你好，世界" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
	if usage.Total != 15 || usage.Estimated {
		t.Fatalf("usage = %+v", usage)
	}
	if strings.Join(deltas, "") != "你好，世界" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestCompleteStreamToolCallAssembly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		// 第一个 chunk：给 id/name/空 arguments；后续按 index 分片 arguments
		seq := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x.go\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		}
		for _, c := range seq {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			fl.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	cfg := Settings{BaseURL: srv.URL, Model: "test-model"}
	text, calls, _, err := completeStream(context.Background(), cfg, []Message{}, ProfileParams{}, builtinTools[:1], nil, func(string) {}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.ID != "call_1" || c.Function.Name != "read_file" {
		t.Fatalf("call = %+v", c)
	}
	if c.Function.Arguments != `{"path":"x.go"}` {
		t.Fatalf("arguments = %q", c.Function.Arguments)
	}
	if text != "" {
		t.Fatalf("text should be empty when only tool calls: %q", text)
	}
}

func TestCompleteStreamNonSSEFallback(t *testing.T) {
	// 上游返回完整 JSON（非 event-stream）→ 走回退路径
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fallback","tool_calls":[]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	cfg := Settings{BaseURL: srv.URL, Model: "test-model"}
	text, _, _, err := completeStream(context.Background(), cfg, []Message{}, ProfileParams{}, nil, nil, func(string) {}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if text != "fallback" {
		t.Fatalf("text = %q", text)
	}
}

func TestCompleteStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	cfg := Settings{BaseURL: srv.URL, Model: "test-model"}
	_, _, _, err := completeStream(context.Background(), cfg, []Message{}, ProfileParams{}, nil, nil, func(string) {}, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}
