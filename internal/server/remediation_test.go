package server

// 整改回归测试（R01/R02/R03/R04/R06）：将审查附件中的缺陷观察测试改写为
// 断言正确行为的回归测试——用例 PASS 表示缺陷已修复，而非缺陷存在。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// R01a：模型/压缩调用期间全局锁被其他请求占用时，压缩必须能完成（不再死锁）。
func TestRemediationAutoCompactNotBlockedByHeldMutex(t *testing.T) {
	a := testApp(t)
	// 构造超阈值会话（>48KB）
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID] // 使用映射内真实指针
	big := []Message{}
	for i := 0; i < 10; i++ {
		big = append(big, Message{Role: "user", Content: strings.Repeat("x", 8000)}, Message{Role: "assistant", Content: strings.Repeat("y", 8000)})
	}
	s.Messages = big
	_ = a.save(s)
	a.mu.Unlock()
	summaryProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"t","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer summaryProvider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: summaryProvider.URL, Model: "test"}
	a.mu.Unlock()
	// 模拟：压缩期间外部持续持锁请求（如 config 轮询）
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				a.mu.Lock()
				time.Sleep(time.Millisecond)
				a.mu.Unlock()
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- func() error { _, e := a.compactForTest(s); return e }() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("compaction must complete under lock contention: %v", err)
		}
	case <-time.After(10 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatal("compaction deadlocked while app mutex was contended")
	}
	close(stop)
	wg.Wait()
	a.mu.Lock()
	after := len(a.sessions[s.ID].Messages)
	compact := a.sessions[s.ID].Compact
	a.mu.Unlock()
	if after >= len(big) || compact == "" {
		t.Fatalf("compaction did not fold messages: after=%d compact=%q", after, compact)
	}
}

func TestContextOverflowCompactsHistoryBeforeRetry(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"keep goal","decisions":["keep decision"],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	s = a.sessions[s.ID]
	for i := 0; i < 10; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("u", 6000)}, Message{Role: "assistant", Content: strings.Repeat("a", 6000)})
	}
	_ = a.save(s)
	before := a.buildContextPreview(s, "continue", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 1000}, true)
	a.mu.Unlock()
	if !before.OverLimit {
		t.Fatalf("fixture should exceed its context window: %+v", before)
	}
	if err := a.compactSessionForContext(context.Background(), s.ID, Settings{BaseURL: provider.URL, Model: "test"}, 1000); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	after := a.buildContextPreview(a.sessions[s.ID], "continue", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 1000}, true)
	folded := a.sessions[s.ID].CompactedMessages
	a.mu.Unlock()
	if folded == 0 || after.OverLimit {
		t.Fatalf("history not compacted enough: folded=%d preview=%+v", folded, after)
	}
}

func TestAutoCompactionPublishesObservableLifecycle(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"keep continuity","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	a.mu.Lock()
	s = a.sessions[s.ID]
	for i := 0; i < 12; i++ {
		s.Messages = append(s.Messages,
			Message{Role: "user", Content: strings.Repeat("user context ", 900)},
			Message{Role: "assistant", Content: strings.Repeat("assistant context ", 900)},
		)
	}
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.settings = cfg
	a.mu.Unlock()

	before := sessionContextTokens(s)
	if before <= compactAutoTokens {
		t.Fatalf("fixture must cross auto-compaction threshold: %d <= %d", before, compactAutoTokens)
	}
	ch, cancel := a.subscribeStream("compaction-observation-test")
	defer cancel()
	a.maybeAutoCompact(context.Background(), s, cfg, "compaction-observation-test")

	var events []streamEvent
	for len(ch) > 0 {
		events = append(events, <-ch)
	}
	var phases []string
	var sawMapProgress bool
	var startedAt int64
	for _, event := range events {
		if event.Event != "compaction" {
			continue
		}
		if event.SessionID != s.ID {
			t.Fatalf("event leaked or lost session identity: got %q want %q", event.SessionID, s.ID)
		}
		if len(phases) == 0 || phases[len(phases)-1] != event.Phase {
			phases = append(phases, event.Phase)
		}
		if event.StartedAt == 0 {
			t.Fatalf("automatic compaction event must retain its start time: %+v", event)
		}
		if startedAt == 0 {
			startedAt = event.StartedAt
		} else if event.StartedAt != startedAt {
			t.Fatalf("automatic compaction start time changed: got %d want %d", event.StartedAt, startedAt)
		}
		if event.Phase == "started" && event.BatchStage == "map" && event.Batch > 0 && event.Batches > 0 {
			sawMapProgress = true
		}
		if event.Folded <= 0 || event.BeforeTokens < before {
			t.Fatalf("event must report actual folded count and pre-compaction estimate: %+v, before=%d", event, before)
		}
		if event.Phase == "completed" {
			if event.AfterTokens <= 0 || event.AfterTokens >= event.BeforeTokens {
				t.Fatalf("completed event must report a lower post-compaction estimate: %+v", event)
			}
		}
	}
	if !reflect.DeepEqual(phases, []string{"started", "folding", "completed"}) {
		t.Fatalf("unexpected compaction lifecycle: %v", phases)
	}
	if !sawMapProgress {
		t.Fatal("automatic compaction did not publish map batch progress")
	}
	if got := sessionContextTokens(s); got != events[len(events)-1].AfterTokens {
		t.Fatalf("completed estimate differs from persisted session state: event=%d session=%d", events[len(events)-1].AfterTokens, got)
	}
}

func TestAutoCompactionCancellationPublishesTerminalAndPreservesHistory(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	requestStarted := make(chan struct{}, 1)
	providerCancelled := make(chan struct{}, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requestStarted <- struct{}{}
		<-r.Context().Done()
		providerCancelled <- struct{}{}
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	a.mu.Lock()
	s = a.sessions[s.ID]
	for i := 0; i < 12; i++ {
		s.Messages = append(s.Messages,
			Message{Role: "user", Content: strings.Repeat("user context ", 900)},
			Message{Role: "assistant", Content: strings.Repeat("assistant context ", 900)},
		)
	}
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.settings = cfg
	original := cloneCompactMessages(s.Messages)
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, stop := a.subscribeStream("compaction-cancel-test")
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.maybeAutoCompact(ctx, s, cfg, "compaction-cancel-test")
	}()
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic compaction did not start its summary request")
	}
	cancel()
	select {
	case <-providerCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the task did not cancel the in-flight provider request")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic compaction did not leave its running state after cancellation")
	}

	var phases []string
	for len(ch) > 0 {
		event := <-ch
		if event.Event == "compaction" {
			phases = append(phases, event.Phase)
			if event.Phase == "cancelled" && !strings.Contains(event.Text, "原会话内容已保留") {
				t.Fatalf("cancellation should explain history preservation: %+v", event)
			}
		}
	}
	if len(phases) < 2 || phases[0] != "started" || phases[len(phases)-1] != "cancelled" {
		t.Fatalf("expected progress followed by an explicit cancelled terminal state, got %v", phases)
	}
	a.mu.Lock()
	got := cloneCompactMessages(a.sessions[s.ID].Messages)
	_, stillCompacting := a.compactingSessions[s.ID]
	a.mu.Unlock()
	if !compactSnapshotMatches(got, original) || stillCompacting {
		t.Fatalf("cancellation must preserve source history and release session lock: messages=%d/%d compacting=%t", len(got), len(original), stillCompacting)
	}
}

func TestProviderCompleteHonorsRequestCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
		close(cancelled)
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "test"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _, _ = complete(ctx, cfg, []Message{{Role: "user", Content: "summarize"}}, ProfileParams{}, nil, nil)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request context was not cancelled")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("complete did not return after cancellation")
	}
}

func TestManualCompactionPublishesLiveProgressAndTokenEstimates(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	requestStarted := make(chan struct{}, 1)
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseRequest) })
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-releaseRequest
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"keep manual progress","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s = a.sessions[s.ID]
	for i := 0; i < 40; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: fmt.Sprintf("history %d %s", i, strings.Repeat("continuity evidence ", 260))})
	}
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()

	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}) }()
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("manual compaction did not reach its model request")
	}
	progress := request(a, "GET", "/api/sessions/"+s.ID+"/compaction-progress", nil)
	requireStatus(t, progress, 200)
	var running struct {
		Active bool        `json:"active"`
		Event  streamEvent `json:"event"`
	}
	if err := json.Unmarshal(progress.Body.Bytes(), &running); err != nil {
		t.Fatal(err)
	}
	if !running.Active || running.Event.Phase != "started" || running.Event.SessionID != s.ID || running.Event.Batch < 1 || running.Event.Batches < running.Event.Batch || running.Event.BatchStage != "map" || running.Event.Folded <= 0 || running.Event.BeforeTokens <= 0 {
		t.Fatalf("manual compaction progress is not observable during summarization: %+v", running)
	}
	releaseOnce.Do(func() { close(releaseRequest) })
	select {
	case response := <-result:
		requireStatus(t, response, 200)
	case <-time.After(10 * time.Second):
		t.Fatal("manual compaction did not finish after the provider resumed")
	}
	progress = request(a, "GET", "/api/sessions/"+s.ID+"/compaction-progress", nil)
	var completed struct {
		Active bool        `json:"active"`
		Event  streamEvent `json:"event"`
	}
	if err := json.Unmarshal(progress.Body.Bytes(), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Active || completed.Event.Phase != "completed" || completed.Event.Folded <= 0 || completed.Event.AfterTokens <= 0 || completed.Event.AfterTokens >= completed.Event.BeforeTokens {
		t.Fatalf("manual compaction completion lacks persisted before/after evidence: %+v", completed)
	}
}

func TestCompactionRetainedHistoryStaysWithinBudget(t *testing.T) {
	a := testApp(t)
	s := &Session{}
	for i := 0; i < 6; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("a", 6000)})
	}
	budget := contextMessageTokens(s.Messages[0]) + 100
	_, split := a.snapshotForCompactBudget(s, budget)
	retained := 0
	for _, message := range s.Messages[split:] {
		retained += contextMessageTokens(message)
	}
	if split != 5 || retained > budget {
		t.Fatalf("retained history exceeds budget: split=%d tokens=%d budget=%d", split, retained, budget)
	}
}

func TestStartTaskCompactsOverBudgetHistoryBeforeProviderCall(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"continue","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", ActiveModel: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	s = a.sessions[s.ID]
	for i := 0; i < 10; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("u", 6000)}, Message{Role: "assistant", Content: strings.Repeat("a", 6000)})
	}
	_ = a.save(s)
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "继续当前工作"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	stored := a.sessions[s.ID]
	folded, compact, messageCount := stored.CompactedMessages, stored.Compact, len(stored.Messages)
	a.mu.Unlock()
	if folded == 0 || compact == "" || messageCount >= 20 {
		t.Fatalf("超预算新请求没有先压缩历史: folded=%d summary=%q messages=%d", folded, compact, messageCount)
	}
}

// compactForTest 模拟自动压缩路径（模型调用在锁外）。
func (a *App) compactForTest(s *Session) (int, error) {
	a.mu.Lock()
	snap, split := a.snapshotForCompact(s)
	cfg := a.settings
	baseLen := len(s.Messages)
	a.mu.Unlock()
	if split == 0 {
		return 0, nil
	}
	summary, err := a.buildCompactionSummary(context.Background(), snap.messages, cfg, snap.prevCompact)
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(s.Messages) != baseLen {
		return 0, nil
	}
	s.Compact = summary
	s.CompactedMessages = snap.prevCount + split
	s.CompactedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.Messages = append([]Message{}, s.Messages[split:]...)
	return split, a.save(s)
}

// R01b：两个并发手动压缩：一个成功、另一个 409 或无害返回；全局锁必须释放，API 保持响应。
func TestRemediationConcurrentCompactionNoPanicNoDeadlock(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID] // 使用映射内真实指针
	big := []Message{}
	for i := 0; i < 20; i++ {
		big = append(big, Message{Role: "user", Content: strings.Repeat("x", 8000)})
	}
	s.Messages = big
	_ = a.save(s)
	a.mu.Unlock()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"t","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer slow.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: slow.URL, Model: "test"}
	a.mu.Unlock()
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{})
			results <- w.Code
		}()
	}
	codes := []int{<-results, <-results}
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			conflict++
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if ok == 0 {
		t.Fatalf("至少一个压缩应成功: %v", codes)
	}
	// 锁必须已释放：主要 API 立即响应
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := request(a, "GET", "/api/config", nil)
		if w.Code == 200 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("API unresponsive after concurrent compaction")
}

// R02a：A 工作区的旧提案不得写入 B 工作区。
func TestRemediationOldProposalRejectedOnNewWorkspace(t *testing.T) {
	a := testApp(t)
	dirA := filepath.Join(a.workPath, "projA")
	dirB := filepath.Join(a.workPath, "projB")
	_ = os.MkdirAll(dirA, 0755)
	_ = os.MkdirAll(dirB, 0755)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projA"}}), 200)
	s := createSession(t, a)
	// 通过 write_file 工具在 A 生成新文件提案（走任务）
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) {
			return "", []ToolCall{readCall("write_file", `{"path":"new.txt","content":"from-A"}`)}
		},
		func() (string, []ToolCall) { return "完成。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "创建文件"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	task := sess.Runs[0]
	if task.Status != "awaiting_approval" {
		t.Fatalf("proposal not created: %+v", task)
	}
	// 切换到 B 后应用 → 必须 409，且 B 不得出现该文件
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projB"}}), 200)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{})
	requireStatus(t, w, 409)
	if _, err := os.Stat(filepath.Join(dirB, "new.txt")); err == nil {
		t.Fatal("proposal must not be written to new workspace")
	}
	// 切回 A 后应用成功
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projA"}}), 200)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), 200)
	if b, err := os.ReadFile(filepath.Join(dirA, "new.txt")); err != nil || string(b) != "from-A" {
		t.Fatalf("apply in original workspace: %s %v", b, err)
	}
}

// R02b：命令 cwd 跟随当前工作区（切换后 pwd 指向 B 而非 A）。
func TestRemediationCommandFollowsCurrentWorkspace(t *testing.T) {
	a := testApp(t)
	dirB := filepath.Join(a.workPath, "projB")
	_ = os.MkdirAll(dirB, 0755)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projB"}}), 200)
	w := request(a, "POST", "/api/command", map[string]string{"command": "pwd"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), filepath.Base(dirB)) {
		t.Fatalf("command must run in current workspace: %s", w.Body.String())
	}
}

// R03a：虚拟根路径穿越必须被拒绝（/workspace/../data、/workspaceXYZ 前缀）。
func TestRemediationVirtualRootTraversalRejected(t *testing.T) {
	a := testApp(t)
	for _, p := range []string{"/workspace/../data", "/workspaceXYZ/etc"} {
		w := request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": p}})
		requireStatus(t, w, 400)
	}
	// 相对路径 .. 同样拒绝
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "../escape"}}), 400)
}

// R04：连续压缩必须携带上一版摘要（早期唯一约束不丢失）。
func TestRemediationCompactionChainRetainsPreviousSummary(t *testing.T) {
	a := testApp(t)
	var capturedMu sync.Mutex
	var captured []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		dec := json.NewDecoder(r.Body)
		_ = dec.Decode(&body)
		var input strings.Builder
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			for _, raw := range msgs {
				if msg, ok := raw.(map[string]any); ok {
					if content, ok := msg["content"].(string); ok {
						input.WriteString(content)
						input.WriteByte('\n')
					}
				}
			}
		}
		capturedMu.Lock()
		captured = append(captured, input.String())
		capturedMu.Unlock()
		constraints := `[]`
		if strings.Contains(input.String(), "唯一约束") {
			constraints = `["唯一约束: 必须使用绿色 UI"]`
		}
		result := `{"goal":"t","constraints":` + constraints + `,"decisions":[],"files":[],"facts":[],"pending":[]}`
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": result}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	a.mu.Unlock()
	s := createSession(t, a)
	// 第一轮压缩：历史含唯一约束标记
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Messages = []Message{{Role: "user", Content: "唯一约束: 必须使用绿色 UI"}, {Role: "assistant", Content: strings.Repeat("z", 30000)}}
	s.Compact = ""
	s.CompactedMessages = 0
	_ = a.save(s)
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 200)
	// 再灌入新历史，进行第二轮压缩：请求必须包含上一版摘要与唯一约束
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("n", 30000)})
	_ = a.save(s)
	prevCompact := s.Compact
	a.mu.Unlock()
	capturedMu.Lock()
	firstRoundCalls := len(captured)
	capturedMu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 200)
	capturedMu.Lock()
	secondRoundInputs := append([]string(nil), captured[firstRoundCalls:]...)
	capturedMu.Unlock()
	if !strings.Contains(prevCompact, "唯一约束") || !strings.Contains(strings.Join(secondRoundInputs, "\n"), "唯一约束") {
		t.Fatalf("second compaction round must receive the previous summary's unique constraint; previous=%q inputs=%q", prevCompact, secondRoundInputs)
	}
	a.mu.Lock()
	after := s.Compact
	a.mu.Unlock()
	if prevCompact == "" || after == "" {
		t.Fatal("compaction chain broken")
	}
}

// R06：write_file 必须逐字节保留正文空白（前导空格、缩进与末尾换行）。
func TestRemediationWriteToolPreservesWhitespace(t *testing.T) {
	a := testApp(t)
	const content = "  indented first line\nsecond\tline\n\n"
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) {
			return "", []ToolCall{readCall("write_file", `{"path":"ws.txt","content":"  indented first line\nsecond\tline\n\n"}`)}
		},
		func() (string, []ToolCall) { return "完成。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "写入"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	task := sess.Runs[0]
	if len(task.Files) != 1 || task.Files[0].Content != content {
		t.Fatalf("content must be byte-preserved: %+v", task.Files[0].Content)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "ws.txt"))
	if err != nil || string(b) != content {
		t.Fatalf("applied content differs: %q %v", b, err)
	}
}

// R07（后端部分）：任务响应结束后，会话 API 状态一致（前端序号逻辑由 Node 审计验证）。
func TestRemediationSessionAPIStableDuringRun(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi"}), 202)
	// 运行期间并发读取会话必须成功（锁未被长期占用）
	deadline := time.Now().Add(3 * time.Second)
	reads := 0
	for time.Now().Before(deadline) {
		w := request(a, "GET", "/api/sessions/"+s.ID, nil)
		if w.Code != 200 {
			t.Fatalf("session read failed during run: %d", w.Code)
		}
		reads++
		var got Session
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if len(got.Runs) > 0 && got.Runs[0].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reads == 0 {
		t.Fatal("no session reads completed")
	}
}

var _ = context.Background

// R08-01：0 费率必须是真实 0（非缺省回退），负值必须拒绝，留空语义由前端显式拒绝。
func TestR08ZeroPricingIsRealZero(t *testing.T) {
	a := testApp(t)
	a.settings.Model = "local-free"
	w := request(a, "GET", "/api/token-pricing", nil)
	requireStatus(t, w, 200)
	var def Pricing
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil || def.PriceIn <= 0 || def.PriceOut <= 0 {
		t.Fatalf("default pricing should be positive: %v %v", w.Body.String(), err)
	}
	w = request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: -1, PriceOut: 0})
	requireStatus(t, w, 400)
	w = request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: 0, PriceOut: 0})
	requireStatus(t, w, 200)
	a.recordTokenUsage(TokenUsage{Prompt: 2000000, Completion: 300000, Total: 2300000, Model: "local-free", Provider: "http://mock"})
	a.tokenStatsMu.Lock()
	cost := 0.0
	for _, c := range a.tokenCalls {
		cost += c.Cost
		if c.Defaulted {
			t.Fatal("explicitly configured 0-rate call must not be defaulted")
		}
	}
	day := a.tokenStats[time.Now().UTC().Format("2006-01-02")]
	a.tokenStatsMu.Unlock()
	if cost != 0 {
		t.Fatalf("0-price call cost %v, want 0", cost)
	}
	if !day.Priced {
		t.Fatal("0-price call still has a price snapshot and must be priced")
	}
	w = request(a, "GET", "/api/token-pricing", nil)
	requireStatus(t, w, 200)
	var back Pricing
	_ = json.Unmarshal(w.Body.Bytes(), &back)
	if back.PriceIn != 0 || back.PriceOut != 0 {
		t.Fatalf("0 price did not round-trip: %+v", back)
	}
}

// R08-02：按模型费率快照；改价不得覆盖历史，新调用按新价。
func TestR08PriceSnapshotStable(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "model-a", "priceIn": 2, "priceOut": 8}), 200)
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "model-b", "priceIn": 10, "priceOut": 40}), 200)
	a.recordTokenUsage(TokenUsage{Prompt: 1000000, Completion: 250000, Total: 1250000, Model: "model-a"})
	a.recordTokenUsage(TokenUsage{Prompt: 500000, Completion: 125000, Total: 625000, Model: "model-b"})
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "model-a", "priceIn": 20, "priceOut": 80}), 200)
	a.recordTokenUsage(TokenUsage{Prompt: 1000000, Completion: 250000, Total: 1250000, Model: "model-a"})
	a.tokenStatsMu.Lock()
	defer a.tokenStatsMu.Unlock()
	if len(a.tokenCalls) != 3 {
		t.Fatalf("want 3 calls, got %d", len(a.tokenCalls))
	}
	if a.tokenCalls[0].Cost != 4 || a.tokenCalls[1].Cost != 10 || a.tokenCalls[2].Cost != 40 {
		t.Fatalf("snapshot costs wrong: %+v", a.tokenCalls)
	}
	if a.tokenCalls[0].PriceIn != 2 || a.tokenCalls[1].PriceIn != 10 || a.tokenCalls[2].PriceIn != 20 {
		t.Fatal("price change rewrote historical snapshots")
	}
	if a.tokenCalls[0].Defaulted || a.tokenCalls[1].Defaulted || a.tokenCalls[2].Defaulted {
		t.Fatal("explicitly configured models must not be defaulted")
	}
}

// R08-03：旧版汇总迁移保留用量与 estimated 标记；费用未知（不虚构 0 或现价），重启不重复累加。
func TestR08LegacyMigration(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"days":{"2026-09-20":{"prompt":1200,"completion":300,"total":1500,"calls":2,"estimated":true},"2026-09-21":{"prompt":2400,"completion":600,"total":3000,"calls":3,"estimated":false}}}`
	if err := os.WriteFile(filepath.Join(data, "token-stats.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	check := func(a *App) {
		t.Helper()
		w := request(a, "GET", "/api/token-stats", nil)
		requireStatus(t, w, 200)
		var out struct {
			Totals   TokenDay            `json:"totals"`
			Unpriced TokenDay            `json:"unpricedTotals"`
			Days     map[string]TokenDay `json:"days"`
			Cost     float64             `json:"cost"`
			Calls    int                 `json:"calls"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Totals.Total != 4500 || out.Totals.Calls != 5 {
			t.Fatalf("legacy totals wrong: %+v", out.Totals)
		}
		if out.Unpriced.Total != 4500 || out.Unpriced.Calls != 5 {
			t.Fatalf("legacy days must be unpriced: %+v", out.Unpriced)
		}
		if out.Cost != 0 || out.Calls != 0 {
			t.Fatalf("no fabricated cost/calls allowed: cost=%v calls=%d", out.Cost, out.Calls)
		}
		if !out.Days["2026-09-20"].Estimated || out.Days["2026-09-21"].Estimated {
			t.Fatalf("estimated flags not preserved: %+v", out.Days)
		}
		if out.Days["2026-09-20"].Priced || out.Days["2026-09-21"].Priced {
			t.Fatal("legacy days must not be marked priced")
		}
	}
	a1, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), data)
	if err != nil {
		t.Fatal(err)
	}
	check(a1)
	a1.Close()
	a2, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), data)
	if err != nil {
		t.Fatal(err)
	}
	check(a2) // 重启不重复累加、不清零
	a2.Close()
}

func TestCompactionSummaryUsesBoundedRequestsAndCarriesPriorSummary(t *testing.T) {
	a := testApp(t)
	priorMarker := "PRIOR_CONTINUITY_MARKER_20261012"
	var requests int
	priorSeen := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages  []Message      `json:"messages"`
			Thinking  map[string]any `json:"thinking"`
			MaxTokens int            `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		requests++
		if body.Thinking["type"] != "disabled" {
			t.Errorf("compaction should disable hidden reasoning to reserve output for the ledger, got %+v", body.Thinking)
		}
		if body.MaxTokens != compactSummaryOutput || body.MaxTokens < 1024 {
			t.Errorf("compaction max_tokens = %d, want bounded output reserve %d", body.MaxTokens, compactSummaryOutput)
		}
		tokens, joined := 0, strings.Builder{}
		for _, message := range body.Messages {
			tokens += contextMessageTokens(message)
			joined.WriteString(message.Content)
		}
		if tokens > compactSummaryInputBudget+400 {
			t.Errorf("compaction request exceeds bounded payload and prompt headroom: %d tokens", tokens)
		}
		if strings.Contains(joined.String(), priorMarker) {
			priorSeen = true
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"long project","constraints":["retain threshold"],"decisions":["keep design"],"files":[],"facts":["confirmed"],"evidence":["not verified"],"pending":["browser check"]}`}}}})
	}))
	defer provider.Close()
	messages := make([]Message, 0, 80)
	for i := 0; i < 80; i++ {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("message %d %s", i, strings.Repeat("长对话证据", 500))})
	}
	previous := priorMarker + strings.Repeat(" prior constraint and decision.", 500)
	var progressCalls int
	var sawMap, sawReduce bool
	summary, err := a.buildCompactionSummary(context.Background(), messages, Settings{BaseURL: provider.URL, Model: "test"}, previous, func(stage string, batch, batches int) {
		progressCalls++
		if batch < 1 || batches < batch {
			t.Errorf("invalid compaction progress: stage=%q batch=%d batches=%d", stage, batch, batches)
		}
		if stage == "map" {
			sawMap = true
		} else if strings.HasPrefix(stage, "reduce-") {
			sawReduce = true
		} else {
			t.Errorf("unexpected compaction progress stage %q", stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests < 2 {
		t.Fatalf("large history should use multiple bounded requests, got %d", requests)
	}
	if progressCalls != requests || !sawMap || !sawReduce {
		t.Fatalf("progress did not represent every map/reduce request: progress=%d requests=%d map=%t reduce=%t", progressCalls, requests, sawMap, sawReduce)
	}
	if !priorSeen {
		t.Fatal("previous continuity summary was not included in the bounded pipeline")
	}
	var record compactSummaryRecord
	if err := json.Unmarshal([]byte(summary), &record); err != nil {
		t.Fatalf("result is not canonical structured summary: %v", err)
	}
	if record.Goal == "" || len(record.Constraints) == 0 || len(record.Decisions) == 0 || len(record.Pending) == 0 {
		t.Fatalf("summary dropped required continuity fields: %+v", record)
	}
}

func TestMalformedCompactionSummaryAbortsWithoutChangingSource(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "not json"}}}})
	}))
	defer provider.Close()
	source := []Message{{Role: "user", Content: "keep this history"}}
	before := append([]Message(nil), source...)
	if _, err := a.buildCompactionSummary(context.Background(), source, Settings{BaseURL: provider.URL, Model: "test"}, ""); err == nil {
		t.Fatal("malformed model summary must abort")
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("failed summary mutated original messages")
	}
}

func TestCompactionSnapshotDetectsToolCallChanges(t *testing.T) {
	var call ToolCall
	call.ID = "tool-1"
	call.Function.Name = "read_file"
	call.Function.Arguments = `{"path":"before.md"}`
	before := []Message{{Role: "assistant", Content: "", ToolCalls: []ToolCall{call}}}
	snapshot := cloneCompactMessages(before)
	if !compactSnapshotMatches(before, snapshot) {
		t.Fatal("matching snapshot rejected")
	}
	before[0].ToolCalls[0].Function.Arguments = `{"path":"after.md"}`
	if compactSnapshotMatches(before, snapshot) {
		t.Fatal("snapshot ignored changed tool arguments")
	}
}
