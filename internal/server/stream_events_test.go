package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseEventFromLines 从一行行读到的 SSE 文本中解析 event/data 序列。
func sseEventFromLines(lines []string) (events []streamEvent, sawDone bool) {
	var ev *streamEvent
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "event: "):
			ev = &streamEvent{Event: strings.TrimSpace(strings.TrimPrefix(line, "event: "))}
		case strings.HasPrefix(line, "data: "):
			if ev != nil {
				var d streamEvent
				if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &d) == nil {
					if ev.Event == "" {
						ev.Event = d.Event
					}
					ev.Step, ev.Status, ev.Text, ev.Tool, ev.Preview, ev.Error, ev.Round = d.Step, d.Status, d.Text, d.Tool, d.Preview, d.Error, d.Round
				}
			}
		case line == "" && ev != nil:
			events = append(events, *ev)
			if ev.Event == "done" {
				sawDone = true
			}
			ev = nil
		}
	}
	return events, sawDone
}

// readSSE 读取 events 响应直到服务端关闭（done 后 runEvents 会返回）；返回收集到的事件序列。
// http.Client 10s 超时用于暴露“晚订阅永久挂起”回归：旧实现会让连接一直不关闭。
func readSSE(t *testing.T, url string) []streamEvent {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events status %d", resp.StatusCode)
	}
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	events, _ := sseEventFromLines(lines)
	return events
}

// streamMock 提供 stream:true 时逐 chunk 输出 SSE、否则返回普通 JSON 的测试提供商。
func streamMock(t *testing.T, chunks []string, usage bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		streaming, _ := body["stream"].(bool)
		if !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "测试主题"}}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		time.Sleep(150 * time.Millisecond) // 给 events 订阅留出先行的窗口，保证实时测试确定性
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			fl.Flush()
		}
		if usage {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\n"))
			fl.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
}

// TestRunEventsLiveStreamingChat 端到端：chat 任务流式运行期间，events 端点按
// step → delta → status → done 顺序推送，文本与最终持久化一致。
func TestRunEventsLiveStreamingChat(t *testing.T) {
	provider := streamMock(t, []string{
		`{"choices":[{"delta":{"content":"你好"}}]}`,
		`{"choices":[{"delta":{"content":"，"}}]}`,
		`{"choices":[{"delta":{"content":"世界"}}]}`,
	}, true)
	defer provider.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()

	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"mode": "chat", "prompt": "hi"})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)

	events := readSSE(t, ts.URL+"/api/sessions/"+s.ID+"/runs/"+task.ID+"/events?access_token="+a.token)
	var text strings.Builder
	var kinds []string
	var finalStatus string
	for _, ev := range events {
		kinds = append(kinds, ev.Event)
		if ev.Event == "delta" {
			text.WriteString(ev.Text)
		}
		if ev.Event == "status" {
			finalStatus = ev.Status
		}
	}
	if text.String() != "你好，世界" {
		t.Fatalf("streamed text = %q", text.String())
	}
	if finalStatus != "completed" {
		t.Fatalf("final status = %q", finalStatus)
	}
	joined := strings.Join(kinds, ",")
	for _, want := range []string{"step", "delta", "status", "done"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing event %q in %q", want, joined)
		}
	}
	// 最终持久化状态与流式文本一致（轮询兜底的真相源）
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	if len(s.Messages) != 2 || s.Messages[1].Content != "你好，世界" {
		t.Fatalf("persisted answer = %+v", s.Messages)
	}
}

// TestRunEventsFinishedTaskCloses 任务结束后订阅：立即收到 status+done 并关闭，
// 不因晚订阅永久挂起。
func TestRunEventsFinishedTaskCloses(t *testing.T) {
	provider := streamMock(t, []string{`{"choices":[{"delta":{"content":"完毕"}}]}`}, false)
	defer provider.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()

	w := request(a, "POST", "/api/sessions", map[string]string{})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"mode": "chat", "prompt": "hi"})
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Runs[0].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Runs[0].Status != "completed" {
		t.Fatalf("task status = %q", s.Runs[0].Status)
	}
	// 任务已结束后再订阅：http.Client 10s 超时会暴露“永久挂起”回归
	events := readSSE(t, ts.URL+"/api/sessions/"+s.ID+"/runs/"+task.ID+"/events?access_token="+a.token)
	var gotStatus, gotDone bool
	for _, ev := range events {
		if ev.Event == "status" {
			gotStatus = true
		}
		if ev.Event == "done" {
			gotDone = true
		}
	}
	if !gotStatus || !gotDone {
		t.Fatalf("finished-task events missing status/done: %+v", events)
	}
}

// TestRunEventsAuthScoping ?access_token= 只对 events 路由生效；其余 API 不接受 URL 凭据。
func TestRunEventsAuthScoping(t *testing.T) {
	a := testApp(t)
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()
	// 无凭据 → 401
	resp, err := http.Get(ts.URL + "/api/sessions/x/runs/y/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("no-token events status = %d", resp.StatusCode)
	}
	// query token 通过鉴权，但任务不存在 → 404（证明 query 凭据在 events 生效）
	resp, err = http.Get(ts.URL + "/api/sessions/x/runs/y/events?access_token=" + a.token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("query-token events status = %d", resp.StatusCode)
	}
	// 非 events 路由不接受 query 凭据
	resp, err = http.Get(ts.URL + "/api/config?access_token=" + a.token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("query-token config status = %d", resp.StatusCode)
	}
}

// TestRunEventsCancelMidStream 运行中取消：SSE 流收到 cancelled 终态并关闭，不悬挂。
func TestRunEventsCancelMidStream(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		streaming, _ := body["stream"].(bool)
		if !streaming {
			// summarizeTopic 等轻量非流式调用：立即返回普通 JSON
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "测试主题"}}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n"))
		fl.Flush()
		once.Do(func() { close(entered) })
		<-r.Context().Done() // 等待客户端取消
	}))
	defer provider.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()

	w := request(a, "POST", "/api/sessions", map[string]string{})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"mode": "chat", "prompt": "hi"})
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)

	// 订阅 SSE，读到第一个 delta 后取消任务
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ts.URL + "/api/sessions/" + s.ID + "/runs/" + task.ID + "/events?access_token=" + a.token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sawDelta := false
	for sc.Scan() {
		if strings.Contains(sc.Text(), `"event":"delta"`) {
			sawDelta = true
			break
		}
	}
	if !sawDelta {
		t.Fatal("no delta event received")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not entered")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/cancel", map[string]any{}), 200)

	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	events, _ := sseEventFromLines(lines)
	var cancelled, done bool
	for _, ev := range events {
		if ev.Event == "status" && ev.Status == "cancelled" {
			cancelled = true
		}
		if ev.Event == "done" {
			done = true
		}
	}
	if !cancelled || !done {
		t.Fatalf("cancel events = %+v", events)
	}
}

// TestStreamHubCloseIsTerminal 终态经 channel close 送达：finish 后订阅关闭，
// 后续 publish 为安全 no-op（不 panic、不发送）。
func TestStreamHubCloseIsTerminal(t *testing.T) {
	a := testApp(t)
	drainClosed := func(c <-chan streamEvent) {
		for {
			if _, open := <-c; !open {
				return
			}
		}
	}
	ch, _ := a.subscribeStream("t1")
	a.publishStream("t1", streamEvent{Event: "delta", Text: "x"})
	a.finishStream("t1", "completed", "")
	drainClosed(ch)
	a.publishStream("t1", streamEvent{Event: "delta", Text: "y"}) // 无订阅者，安全 no-op
	drainClosed(ch)
	// 新订阅在 finish 之后：channel 不会收到事件，由 runEvents 的状态复查兜底发终态
	ch2, _ := a.subscribeStream("t1")
	a.finishStream("t1", "failed", "boom")
	drainClosed(ch2)
}

// TestCompleteStreamRetryWithoutStreamOptions 严格网关拒绝 stream_options 时，
// 自动去掉该字段重试一次并成功；请求快照记录最终生效的请求体。
func TestCompleteStreamRetryWithoutStreamOptions(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		attempts++
		mu.Unlock()
		if _, ok := body["stream_options"]; ok {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"stream_options is not supported"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"重试成功\"}}]}\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		fl.Flush()
	}))
	defer provider.Close()

	var recorded [][]byte
	var recMu sync.Mutex
	text, _, _, err := completeStream(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{}, ProfileParams{}, nil,
		func(b []byte) { recMu.Lock(); recorded = append(recorded, b); recMu.Unlock() },
		func(string) {}, nil)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if text != "重试成功" {
		t.Fatalf("text = %q", text)
	}
	mu.Lock()
	n := attempts
	mu.Unlock()
	if n != 2 {
		t.Fatalf("attempts = %d, want 2", n)
	}
	recMu.Lock()
	last := string(recorded[len(recorded)-1])
	recMu.Unlock()
	if strings.Contains(last, "stream_options") {
		t.Fatalf("retried body still has stream_options: %s", last)
	}
}

// TestCompleteStreamCancelMidStream 流式读取期间取消 ctx：返回取消错误而非半成品。
func TestCompleteStreamCancelMidStream(t *testing.T) {
	entered := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半\"}}]}\n\n"))
		fl.Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-entered
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _, _, err := completeStream(ctx, Settings{BaseURL: provider.URL, Model: "test"}, []Message{}, ProfileParams{}, nil, nil, func(string) {}, nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
