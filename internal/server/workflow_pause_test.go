package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolLoopCheckpointResumesAfterCompletedTool(t *testing.T) {
	a := testApp(t)
	var requests atomic.Int32
	var resumedMessagesMu sync.Mutex
	var resumedMessages []Message
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		switch requests.Add(1) {
		case 1:
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant", "content": "", "tool_calls": []ToolCall{readCall("tool_that_does_not_exist", `{}`)},
			}}}})
		case 2:
			resumedMessagesMu.Lock()
			resumedMessages = append([]Message(nil), body.Messages...)
			resumedMessagesMu.Unlock()
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "continued from checkpoint"}}}})
		case 3:
			if !strings.Contains(body.Messages[len(body.Messages)-1].Content, "任务收尾自检") {
				t.Error("missing completion review")
			}
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "continued from checkpoint"}}}})
		default:
			t.Errorf("unexpected provider request %d", requests.Load())
			jsonOut(w, 500, map[string]string{"error": "unexpected request"})
		}
	}))
	defer provider.Close()

	a.settings = Settings{BaseURL: provider.URL, Model: "test", ToolMaxRounds: 4}
	task := &Task{ID: "pause-test", Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
	initial := []Message{{Role: "user", Content: "continue this work"}}
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint []Message
	checkpointFn := func(_ string, chain []Message) {
		checkpoint = append([]Message(nil), chain...)
		if len(chain) > 0 && chain[len(chain)-1].Role == "tool" {
			cancel()
		}
	}
	registerHarnessFixtureTask(t, a, task)
	_, chain, err := a.toolLoop(ctx, Settings{BaseURL: provider.URL, Model: "test"}, initial, ProfileParams{}, nil, task, nil, 0, checkpointFn)
	if err != context.Canceled {
		t.Fatalf("first toolLoop error = %v, want context.Canceled", err)
	}
	if len(checkpoint) != 3 || checkpoint[1].Role != "assistant" || len(checkpoint[1].ToolCalls) != 1 || checkpoint[2].Role != "tool" {
		t.Fatalf("checkpoint does not contain a closed tool-call round: %#v", checkpoint)
	}
	if len(chain) != len(checkpoint) {
		t.Fatalf("returned chain length %d differs from checkpoint %d", len(chain), len(checkpoint))
	}
	if len(task.ToolUses) != 1 {
		t.Fatalf("completed tool call recorded %d times before resume, want 1", len(task.ToolUses))
	}

	registerHarnessFixtureTask(t, a, task)
	out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, checkpoint, ProfileParams{}, nil, task, nil, 0)
	if err != nil || out != "continued from checkpoint" {
		t.Fatalf("resume output = %q, err = %v", out, err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("provider requests = %d, want original tool round + resume + one completion review", got)
	}
	if len(task.ToolUses) != 1 {
		t.Fatalf("resume repeated a completed tool call: got %d tool uses", len(task.ToolUses))
	}
	resumedMessagesMu.Lock()
	gotResumedMessages := append([]Message(nil), resumedMessages...)
	resumedMessagesMu.Unlock()
	if len(gotResumedMessages) != 3 || gotResumedMessages[1].Role != "assistant" || gotResumedMessages[2].Role != "tool" || !strings.Contains(gotResumedMessages[2].Content, "未知工具") {
		t.Fatalf("resume request did not reuse tool result checkpoint: %#v", gotResumedMessages)
	}
}

func TestInterruptedTaskWithCheckpointIsResumableAfterRestart(t *testing.T) {
	root := t.TempDir()
	work, reference, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	a, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{ID: "pause-restart", Runs: []*Task{{
		ID: "run-1", Mode: "chat", Prompt: "continue", Status: "running", CanResume: true,
		CheckpointStep: "chat", CheckpointMessages: []Message{{Role: "user", Content: "continue"}},
	}}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		a.Close()
		t.Fatal(err)
	}
	a.mu.Unlock()
	a.Close()

	reloaded, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	got := reloaded.sessions[s.ID].Runs[0]
	if got.Status != "interrupted" || !got.CanResume || got.CheckpointStep != "chat" || len(got.CheckpointMessages) != 1 {
		t.Fatalf("restart did not retain resumable checkpoint: %+v", got)
	}
}

func TestResumeEndpointContinuesSavedMessages(t *testing.T) {
	a := testApp(t)
	var receivedMu sync.Mutex
	var received []Message
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		receivedMu.Lock()
		received = append([]Message(nil), body.Messages...)
		receivedMu.Unlock()
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "continued"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}

	s := &Session{ID: newID(), Runs: []*Task{{
		ID: "paused-run", Mode: "chat", Prompt: "continue this work", Status: "paused", CanResume: true,
		Steps: []Step{{Name: "chat", Status: "paused"}}, CheckpointStep: "chat",
		CheckpointMessages: []Message{{Role: "user", Content: "continue this work"}, {Role: "assistant", Content: "already reasoned"}},
	}}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()

	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/paused-run/resume", map[string]any{})
	requireStatus(t, w, 202)
	var resumed Task
	if err := json.Unmarshal(w.Body.Bytes(), &resumed); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		status := ""
		if len(s.Runs) == 2 {
			status = s.Runs[1].Status
		}
		a.mu.Unlock()
		if status == "completed" {
			break
		}
		if status == "failed" {
			a.mu.Lock()
			failed := *s.Runs[1]
			a.mu.Unlock()
			t.Fatalf("resumed task failed: %+v", failed)
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(s.Runs) != 2 || s.Runs[0].Status != "resumed" || s.Runs[1].ID != resumed.ID || s.Runs[1].Status != "completed" {
		t.Fatalf("unexpected resume lifecycle: %#v", s.Runs)
	}
	receivedMu.Lock()
	gotReceived := append([]Message(nil), received...)
	receivedMu.Unlock()
	if len(gotReceived) != 2 || gotReceived[1].Content != "already reasoned" {
		t.Fatalf("provider did not receive saved reasoning context: %#v", gotReceived)
	}
}

func TestPauseEndpointCancelsRunAndPersistsRequest(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "running-run", Mode: "chat", Prompt: "pause me", Status: "running", CanResume: true, CheckpointMessages: []Message{{Role: "user", Content: "pause me"}}}
	s := &Session{ID: newID(), Runs: []*Task{task}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.cancels[task.ID] = cancel
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()

	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/pause", map[string]any{})
	requireStatus(t, w, 202)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("pause endpoint did not cancel the active run")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !task.PauseRequested || task.Status != "running" {
		t.Fatalf("pause request state = status %q, requested %t", task.Status, task.PauseRequested)
	}
	stored, err := os.ReadFile(SessionPath(a.dataPath, s.ID, sessionBucketFor(s)))
	if err != nil {
		t.Fatal(err)
	}
	var persisted Session
	if err := json.Unmarshal(stored, &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.Runs[0].PauseRequested || !persisted.Runs[0].CanResume {
		t.Fatalf("pause request was not persisted: %+v", persisted.Runs[0])
	}
}

// A slow cancellation must not monopolize the mutex used by every chat/API.
func TestPauseEndpointDoesNotBlockOtherSessions(t *testing.T) {
	a := testApp(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	taskA := &Task{ID: "pause-isolated-a", Status: "running", CanResume: true, CheckpointMessages: []Message{{Role: "user", Content: "A"}}}
	taskB := &Task{ID: "pause-isolated-b", Status: "running", CanResume: true, CheckpointMessages: []Message{{Role: "user", Content: "B"}}}
	one, two := &Session{ID: newID(), Runs: []*Task{taskA}}, &Session{ID: newID(), Runs: []*Task{taskB}}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var cancellations atomic.Int32
	a.mu.Lock()
	a.sessions[one.ID], a.sessions[two.ID] = one, two
	a.cancels[taskA.ID] = func() {
		if cancellations.Add(1) == 1 {
			close(entered)
		}
		<-release
		cancelA()
	}
	a.cancels[taskB.ID] = cancelB
	a.mu.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+taskA.ID+"/pause", map[string]any{})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("pause did not reach cancellation")
	}
	probe := make(chan *httptest.ResponseRecorder, 1)
	go func() { probe <- request(a, "GET", "/api/sessions/"+two.ID, nil) }()
	select {
	case w := <-probe:
		requireStatus(t, w, 200)
	case <-time.After(time.Second):
		t.Fatal("pausing A blocked B's session API")
	}
	// Duplicate clicks/another window do not invoke the same cancellation again.
	requireStatus(t, request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+taskA.ID+"/pause", map[string]any{}), 202)
	requireStatus(t, request(a, "POST", "/api/sessions/"+two.ID+"/runs/"+taskA.ID+"/pause", map[string]any{}), 404)
	select {
	case <-ctxB.Done():
		t.Fatal("pausing A cancelled B")
	default:
	}
	a.mu.Lock()
	if taskB.PauseRequested || taskB.Status != "running" {
		t.Error("B state changed")
	}
	stored, err := os.ReadFile(SessionPath(a.dataPath, one.ID, sessionBucketFor(one)))
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var persisted Session
	if json.Unmarshal(stored, &persisted) != nil || !persisted.Runs[0].PauseRequested {
		t.Fatal("pause was not durable before cancellation")
	}
	unblock()
	select {
	case w := <-done:
		requireStatus(t, w, 202)
	case <-time.After(time.Second):
		t.Fatal("pause did not finish")
	}
	if ctxA.Err() != context.Canceled || cancellations.Load() != 1 {
		t.Fatal("target cancellation was not isolated/idempotent")
	}
}

func TestPauseSessionIsolationLiveRuns(t *testing.T) {
	a := testApp(t)
	enteredA, enteredB := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var seenA, seenB atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad body", 400)
			return
		}
		if len(body.Messages) > 0 && strings.Contains(body.Messages[0].Content, "唯一主题") {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "test topic"}}}})
			return
		}
		isA := false
		for _, m := range body.Messages {
			if m.Role == "user" && strings.Contains(m.Content, "hold-A") {
				isA = true
			}
		}
		if isA {
			if seenA.CompareAndSwap(false, true) {
				close(enteredA)
			}
		} else {
			if seenB.CompareAndSwap(false, true) {
				close(enteredB)
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "finished B"}}}})
	}))
	defer provider.Close()
	defer unblock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	one, two := createSession(t, a), createSession(t, a)
	start := func(id, prompt string) Task {
		w := request(a, "POST", "/api/sessions/"+id+"/runs", map[string]any{"prompt": prompt, "mode": "chat"})
		requireStatus(t, w, 202)
		var run Task
		if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
			t.Fatal(err)
		}
		return run
	}
	first, second := start(one.ID, "hold-A"), start(two.ID, "hold-B")
	for _, ch := range []chan struct{}{enteredA, enteredB} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			unblock()
			t.Fatal("both runs did not reach provider")
		}
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+first.ID+"/pause", map[string]any{}), 202)
	deadline := time.Now().Add(3 * time.Second)
	paused := false
	for time.Now().Before(deadline) {
		a.mu.Lock()
		paused = a.sessions[one.ID].Runs[0].Status == "paused"
		other := a.sessions[two.ID].Runs[0]
		running := other.Status == "running" && !other.PauseRequested
		a.mu.Unlock()
		if !running {
			unblock()
			t.Fatal("pausing A changed B")
		}
		if paused {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !paused {
		unblock()
		t.Fatal("A did not pause")
	}
	requireStatus(t, request(a, "GET", "/api/sessions/"+two.ID, nil), 200)
	unblock()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		status := a.sessions[two.ID].Runs[0].Status
		a.mu.Unlock()
		if status == "completed" {
			return
		}
		if status != "running" {
			t.Fatalf("B ended in %s (%s)", status, second.ID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("B did not finish independently")
}
