package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// steerInterruptMock 第一轮流式请求先输出一段正文后挂起，等待请求上下文被取消；
// 第二轮立即返回新回答。用于验证插话能在生成过程中真正打断本轮。
func steerInterruptMock(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var rounds atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if streaming, _ := body["stream"].(bool); !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		n := rounds.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		if n == 1 {
			_, _ = w.Write([]byte("data: " + mustJSON(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "被打断前的部分"}}}}) + "\n\n"))
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("data: " + mustJSON(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "新方向的回答"}}}}) + "\n\n"))
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	return server, &rounds
}

// TestSteerInterruptsInFlightRound 插话登记后必须立即取消当前流式请求，
// 保留已生成的部分正文，并在下一轮携带插话继续，而不是等整段回答结束。
func TestSteerInterruptsInFlightRound(t *testing.T) {
	provider, rounds := steerInterruptMock(t)
	defer provider.Close()
	a := testApp(t)
	cfg := Settings{BaseURL: provider.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	registerHarnessFixtureTask(t, a, task)

	go func() {
		time.Sleep(250 * time.Millisecond)
		a.mu.Lock()
		task.Steer <- "换个方向"
		cancelRoundLocked(task)
		a.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	started := time.Now()
	out, chain, err := a.toolLoop(ctx, cfg, []Message{{Role: "user", Content: "第一个问题"}}, ProfileParams{}, nil, task, map[string]Change{}, 0)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("toolLoop: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("steer did not interrupt the in-flight round; elapsed=%s", elapsed)
	}
	if out != "新方向的回答" {
		t.Fatalf("final out = %q, want 新方向的回答", out)
	}
	if got := rounds.Load(); got != 2 {
		t.Fatalf("provider rounds = %d, want 2（打断后重发一轮）", got)
	}
	if len(task.Steps[0].OutputSegments) != 2 {
		t.Fatalf("output segments = %d, want 2（打断前的正文 + 新回答各一段）", len(task.Steps[0].OutputSegments))
	}
	if !strings.Contains(task.Steps[0].OutputSegments[0].Content, "被打断前的部分") {
		t.Fatalf("first segment = %q, must keep the partial answer generated before the steer", task.Steps[0].OutputSegments[0].Content)
	}
	tail := chain[len(chain)-1].Content
	if !strings.Contains(tail, "换个方向") || !strings.HasPrefix(tail, "【你回答过程中用户插话】") {
		t.Fatalf("chain tail = %q, must carry the wrapped steer", tail)
	}
	select {
	case steer := <-task.Steer:
		t.Fatalf("steer channel still holds %q", steer)
	default:
	}
}
