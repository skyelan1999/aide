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

// roundMock 每次流式请求返回 “回答N”（N 为流式请求序号），非流式返回 “主题”。
func roundMock(t *testing.T) *httptest.Server {
	t.Helper()
	var rounds atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		streaming, _ := body["stream"].(bool)
		if !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题"}}}})
			return
		}
		n := rounds.Add(1)
		content := fmt.Sprintf("回答%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + mustJSON(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}}}}) + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestToolLoopSteerAndQueue 确定性地验证 toolLoop 的插话/排队续轮：
// 预置插话消息在 Steer 通道、排队消息在 Queue，三轮依次回答，最终答案=第三轮，
// 队列与通道都被消费，插话优先级高于排队。
func TestToolLoopSteerAndQueue(t *testing.T) {
	provider := roundMock(t)
	defer provider.Close()
	a := testApp(t)
	cfg := Settings{BaseURL: provider.URL, Model: "test"}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Queue: []string{"排队问题"}, Steps: []Step{{Name: "chat", Status: "running"}}}
	task.Steer <- "插话问题"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := []Message{{Role: "user", Content: "第一个问题"}}
	out, chain, err := a.toolLoop(ctx, cfg, input, ProfileParams{}, nil, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatalf("toolLoop: %v", err)
	}
	if out != "回答3" {
		t.Fatalf("final out = %q, want 回答3（插话先于排队，各续一轮）", out)
	}
	if len(task.Queue) != 0 {
		t.Fatalf("queue not drained: %v", task.Queue)
	}
	select {
	case steer := <-task.Steer:
		t.Fatalf("steer channel still has %q", steer)
	default:
	}
	// 对话链以最后一轮的用户消息收尾（最终 assistant 由 execute 在链外追加）
	var roles []string
	for _, m := range chain {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,user,assistant,user" {
		t.Fatalf("chain roles = %v", roles)
	}
	tail := chain[len(chain)-1].Content
	if !strings.Contains(tail, "排队问题") || !strings.HasPrefix(tail, "【你回答过程中用户插话】") {
		t.Fatalf("chain tail = %q, should be wrapped steer containing 排队问题", tail)
	}
}

// TestStartTaskSteerAndQueueBranch 运行中提交新任务：排队/插话分支、429 满通道、Steers 记录与持久化。
func TestStartTaskSteerAndQueueBranch(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{BaseURL: "http://unused", Model: "test"}
	s := &Session{ID: newID(), Title: "s", Messages: []Message{{Role: "user", Content: "第一个问题"}}}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	s.Runs = append(s.Runs, task)
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	url := "/api/sessions/" + s.ID + "/runs"
	// 排队
	w := request(a, "POST", url, map[string]any{"prompt": "排队问题", "mode": "chat", "queued": true})
	requireStatus(t, w, 202)
	if !strings.Contains(w.Body.String(), `"queued":true`) {
		t.Fatalf("queue response: %s", w.Body.String())
	}
	if len(task.Queue) != 1 || task.Queue[0] != "排队问题" {
		t.Fatalf("queue = %v", task.Queue)
	}
	// 插话
	w = request(a, "POST", url, map[string]any{"prompt": "插话问题", "mode": "chat"})
	requireStatus(t, w, 202)
	select {
	case steer := <-task.Steer:
		if steer != "插话问题" {
			t.Fatalf("steer = %q", steer)
		}
	case <-time.After(time.Second):
		t.Fatal("steer not delivered")
	}
	// 通道满 → 429
	for i := 0; i < 4; i++ {
		task.Steer <- fmt.Sprintf("占位%d", i)
	}
	w = request(a, "POST", url, map[string]any{"prompt": "溢出", "mode": "chat"})
	requireStatus(t, w, 429)
	for _, m := range s.Messages {
		if m.Content == "溢出" {
			t.Fatal("429 拒绝的插话不应记入会话消息")
		}
	}
	// Steers 记录与持久化
	if len(task.Steers) != 2 || task.Steers[0].Queued != true || task.Steers[1].Queued != false {
		t.Fatalf("steers = %+v", task.Steers)
	}
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	loaded := a2.sessions[s.ID].Runs[0]
	if len(loaded.Queue) != 1 || loaded.Queue[0] != "排队问题" {
		t.Fatalf("persisted queue = %v", loaded.Queue)
	}
	if len(loaded.Steers) != 2 {
		t.Fatalf("persisted steers = %+v", loaded.Steers)
	}
}

// TestQueueUpdateActions 队列管理：delete / edit / steer 及越界、非运行态拒绝。
func TestQueueUpdateActions(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: newID(), Title: "s"}
	task := &Task{ID: newID(), Status: "running", Steer: make(chan string, 4), Queue: []string{"a", "b", "c"},
		Steers: []SteerMsg{{Content: "a", Queued: true}, {Content: "b", Queued: true}, {Content: "c", Queued: true}}}
	s.Runs = append(s.Runs, task)
	a.sessions[s.ID] = s
	base := "/api/sessions/" + s.ID + "/runs/" + task.ID + "/queue/"
	// edit b → B
	requireStatus(t, request(a, "POST", base+"1", map[string]any{"action": "edit", "content": "B"}), 200)
	if task.Queue[1] != "B" || task.Steers[1].Content != "B" {
		t.Fatalf("edit: queue=%v steers=%+v", task.Queue, task.Steers)
	}
	// delete b（index 1）
	requireStatus(t, request(a, "POST", base+"1", map[string]any{"action": "delete"}), 200)
	if len(task.Queue) != 2 || task.Queue[0] != "a" || task.Queue[1] != "c" {
		t.Fatalf("delete: queue = %v", task.Queue)
	}
	if len(task.Steers) != 2 || task.Steers[0].Content != "a" || task.Steers[1].Content != "c" {
		t.Fatalf("delete: steers = %+v", task.Steers)
	}
	// steer c（index 1）→ 插队通道
	requireStatus(t, request(a, "POST", base+"1", map[string]any{"action": "steer"}), 200)
	if len(task.Queue) != 1 || task.Queue[0] != "a" {
		t.Fatalf("steer: queue = %v", task.Queue)
	}
	select {
	case steer := <-task.Steer:
		if steer != "c" {
			t.Fatalf("steer content = %q", steer)
		}
	default:
		t.Fatal("steer not delivered")
	}
	if task.Steers[1].Queued {
		t.Fatal("steered entry should be marked queued=false")
	}
	// 越界
	requireStatus(t, request(a, "POST", base+"5", map[string]any{"action": "delete"}), 400)
	// 未知操作
	requireStatus(t, request(a, "POST", base+"0", map[string]any{"action": "nope"}), 400)
	// 非运行态拒绝
	task.Status = "completed"
	requireStatus(t, request(a, "POST", base+"0", map[string]any{"action": "delete"}), 404)
}
