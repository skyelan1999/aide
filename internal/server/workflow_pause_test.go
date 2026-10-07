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
