package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sseRaw 写出一段 OpenAI 兼容 SSE（含 [DONE]）。
func sseRaw(w http.ResponseWriter, fl http.Flusher, chunk string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
	fl.Flush()
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	fl.Flush()
}

// emptyChunk 上游正常结束但既无正文也无工具调用（d 类空响应）。
const emptyChunk = `{"choices":[{"delta":{},"finish_reason":"stop"}]}`

// TestCompleteStreamEmptyIsSentinel 验证空响应被识别为可兜底的哨兵错误，而非笼统字符串。
func TestCompleteStreamEmptyIsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		sseRaw(w, fl, emptyChunk)
	}))
	defer srv.Close()

	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	_, _, _, _, err := completeStream(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}, ProfileParams{}, nil, nil, func(string) {}, nil)
	if !isEmptyCompletionErr(err) {
		t.Fatalf("err = %v, want emptyCompletionErr", err)
	}
	if !strings.Contains(err.Error(), "没有返回文本内容") {
		t.Fatalf("err message = %v", err)
	}
}

// TestCompleteStreamEmptyCarriesFinishReason 验证 length 截断类空响应把 finish_reason 带进错误。
func TestCompleteStreamEmptyCarriesFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		sseRaw(w, fl, `{"choices":[{"delta":{},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()

	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	_, _, _, _, err := completeStream(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}, ProfileParams{}, nil, nil, func(string) {}, nil)
	if !isEmptyCompletionErr(err) {
		t.Fatalf("err = %v, want emptyCompletionErr", err)
	}
	if !strings.Contains(err.Error(), "length") {
		t.Fatalf("err = %v, should carry finish_reason=length", err)
	}
}

// TestToolLoopEmptyFallbackRecovers 第一轮空响应后，注入提示再问，第二轮模型给出结论：
// 不再“未返回回答”，而是拿到最终正文，且提示进入对话链。
func TestToolLoopEmptyFallbackRecovers(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if streaming, _ := body["stream"].(bool); !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		fl, _ := w.(http.Flusher)
		switch n.Add(1) {
		case 1:
			sseRaw(w, fl, emptyChunk) // 空响应
		default:
			sseRaw(w, fl, `{"choices":[{"delta":{"content":"最终结论"}}]}`)
		}
	}))
	defer srv.Close()

	a := testApp(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "画个户型"}}
	out, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatalf("toolLoop err = %v", err)
	}
	if out != "最终结论" {
		t.Fatalf("out = %q, want 最终结论（兜底续接应成功）", out)
	}
	nudge := false
	for _, m := range chain {
		if m.Role == "user" && strings.Contains(m.Content, "系统提示") {
			nudge = true
		}
	}
	if !nudge {
		t.Fatalf("chain 缺少续接提示: %+v", chain)
	}
}

// TestToolLoopEmptyFallbackExhausted 连续空响应耗尽兜底次数后：
// 不判失败、保留对话链，返回可操作的明确状态（含“没有生成正文”与工具计数）。
func TestToolLoopEmptyFallbackExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if streaming, _ := body["stream"].(bool); !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		fl, _ := w.(http.Flusher)
		sseRaw(w, fl, emptyChunk)
	}))
	defer srv.Close()

	a := testApp(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "画个户型"}}
	out, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatalf("耗尽兜底不应返回错误: %v", err)
	}
	if !strings.Contains(out, "没有生成正文") {
		t.Fatalf("out = %q, want 明确的空响应状态说明", out)
	}
	if len(chain) < 1 {
		t.Fatal("对话链必须保留，不能像旧版那样丢弃")
	}
	nudges := 0
	for _, m := range chain {
		if m.Role == "user" && strings.Contains(m.Content, "系统提示") {
			nudges++
		}
	}
	if nudges != 2 {
		t.Fatalf("nudges = %d, want 2（最多自动续接 2 次）", nudges)
	}
}

// TestToolLoopUpstreamErrorPreservesChain 上游 500：返回真实错误（含状态码），
// 但对话链不再被丢弃（旧版 return "", nil, err 会丢掉全部工具历史）。
func TestToolLoopUpstreamErrorPreservesChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	a := testApp(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "hi"}}
	_, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want 含 500 的真实上游错误", err)
	}
	if chain == nil || len(chain) == 0 {
		t.Fatal("上游错误也必须保留对话链")
	}
}

// TestAnalyzeShellFailureDwgHint 验证 dwg/apt 失败被翻译成“换 dxf/ezdxf”引导，
// 阻止模型在 dwg 闭源格式上无效空转。
func TestAnalyzeShellFailureDwgHint(t *testing.T) {
	out := analyzeShellFailure("dwgwrite -o /workspace/x.dwg /workspace/x.dxf", "", 127, nil)
	if !strings.Contains(out, "ezdxf") || !strings.Contains(out, ".dxf") {
		t.Fatalf("dwg 失败未引导换 dxf: %s", out)
	}
	out2 := analyzeShellFailure("apt-get install libredwg-dev", "permission denied", 1, nil)
	if !strings.Contains(out2, "apt") && !strings.Contains(out2, "root") {
		t.Fatalf("apt 失败未提示无 root: %s", out2)
	}
}
