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

// sseSeq 依次写出多段 OpenAI 兼容 SSE chunk，最后 [DONE]。
func sseSeq(w http.ResponseWriter, fl http.Flusher, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	for _, c := range chunks {
		_, _ = w.Write([]byte("data: " + c + "\n\n"))
		fl.Flush()
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	fl.Flush()
}

// ── 单元：incompleteToolCallIndex ──

func TestIncompleteToolCallIndex(t *testing.T) {
	// 全部闭合 → -1
	if i := incompleteToolCallIndex([]ToolCall{readCall("read_file", `{"path":"a.go"}`)}); i != -1 {
		t.Fatalf("闭合 call 应返回 -1, got %d", i)
	}
	// 最后一个 call arguments 未闭合 → 返回其下标
	calls := []ToolCall{
		readCall("read_file", `{"path":"a.go"}`),
		readCall("run_shell", `{"command":"echo hel`), // 截断
	}
	if i := incompleteToolCallIndex(calls); i != 1 {
		t.Fatalf("未闭合 call 下标 = %d, want 1", i)
	}
	// 空 arguments（刚开口就截断）也算未闭合
	calls2 := []ToolCall{readCall("run_shell", "")}
	if i := incompleteToolCallIndex(calls2); i != 0 {
		t.Fatalf("空 arguments 下标 = %d, want 0", i)
	}
}

func TestStripCodeFence(t *testing.T) {
	if got := stripCodeFence("```json\nlo\"}\n```"); got != `lo"}` {
		t.Fatalf("stripCodeFence = %q", got)
	}
	if got := stripCodeFence(`lo"}`); got != `lo"}` {
		t.Fatalf("no fence = %q", got)
	}
}

// ── 端到端：length 时未闭合 tool_call → 自动续写补全 → 执行 ──
//
// 序列（streaming）：
//  1. toolLoop round0：吐出 run_shell 的 arguments 前缀 `{"command":"echo hel`，finish_reason=length
//  2. patchTruncatedCall（请求不带 tools）：模型纯文本吐出剩余 `lo"}`
//  3. toolLoop round1：执行完 echo 后，模型给最终正文 done
func TestToolLoopLengthTruncatedToolCallAutoPatches(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		var body struct {
			Stream bool     `json:"stream"`
			Tools  []any    `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		step := n.Add(1)
		switch step {
		case 1:
			// 第一轮：未闭合的 run_shell tool call，length 截断
			sseSeq(w, fl, []string{
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"run_shell","arguments":""}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"echo hel"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
			})
		case 2:
			// patch 续写调用：只回纯文本 JSON 剩余部分
			sseSeq(w, fl, []string{
				`{"choices":[{"delta":{"content":"lo\"}"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			})
		default:
			// 工具执行后，模型给最终结论
			sseSeq(w, fl, []string{
				`{"choices":[{"delta":{"content":"done"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			})
		}
	}))
	defer srv.Close()

	a := testApp(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "生成 dxf"}}
	out, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatalf("toolLoop err = %v", err)
	}
	if out != "done" {
		t.Fatalf("out = %q, want done（续写补全后应执行工具并收尾）", out)
	}
	// run_shell 必须被真正执行过一次（参数已补全为 echo hello）
	a.mu.Lock()
	ran := false
	for _, tu := range task.ToolUses {
		if tu.Tool == "run_shell" && strings.Contains(tu.Args, "echo hello") {
			ran = true
		}
	}
	a.mu.Unlock()
	if !ran {
		t.Fatalf("补全后的 run_shell(echo hello) 未执行: %+v", task.ToolUses)
	}
	// 绝不能出现“基于工具结果总结”的误导文案（此时工具结果刚刚产生）
	for _, m := range chain {
		if strings.Contains(m.Content, "基于这些工具结果直接给出最终结论") {
			t.Fatalf("不应出现基于工具结果总结的误导文案: %+v", chain)
		}
	}
}

// ── 端到端：纯正文 length → 保留已输出正文，续写补正文（不回退既有流式保留） ──
func TestToolLoopLengthPureTextContinues(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		switch n.Add(1) {
		case 1:
			// 正文被截断，无工具调用
			sseSeq(w, fl, []string{
				`{"choices":[{"delta":{"content":"前半段"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
			})
		default:
			// 续写完成
			sseSeq(w, fl, []string{
				`{"choices":[{"delta":{"content":"后半段"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			})
		}
	}))
	defer srv.Close()

	a := testApp(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "写一段长说明"}}
	out, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatalf("toolLoop err = %v", err)
	}
	if out != "后半段" {
		t.Fatalf("out = %q, want 后半段（纯正文 length 应续写后收尾）", out)
	}
	// 对话链里应同时保留已截断的 assistant 正文与续写提示
	kept := false
	for _, m := range chain {
		if m.Role == "assistant" && m.Content == "前半段" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("已流式输出的截断正文必须保留进对话链: %+v", chain)
	}
}
