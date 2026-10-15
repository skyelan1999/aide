package server

import (
	"context"
	"encoding/json"
	"io"
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

func TestStopEndpointIsolationAndDurableIntent(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "stop-a", Status: "running", PauseRequested: true, CanResume: true, CheckpointMessages: []Message{{Role: "user", Content: "A"}}}
	other := &Task{ID: "stop-b", Status: "running"}
	one, two := &Session{ID: newID(), Runs: []*Task{task}}, &Session{ID: newID(), Runs: []*Task{other}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	a.mu.Lock()
	a.sessions[one.ID], a.sessions[two.ID] = one, two
	a.cancels[task.ID] = func() {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		cancel()
	}
	a.mu.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+task.ID+"/cancel", map[string]any{})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel was not reached")
	}
	// The target cancellation is deliberately blocked; unrelated API access and
	// a duplicate stop must still complete without cancelling another session.
	probe := make(chan *httptest.ResponseRecorder, 1)
	go func() { probe <- request(a, "GET", "/api/sessions/"+two.ID, nil) }()
	select {
	case w := <-probe:
		requireStatus(t, w, 200)
	case <-time.After(time.Second):
		t.Fatal("stop holds global session mutex")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+task.ID+"/cancel", map[string]any{}), 200)
	requireStatus(t, request(a, "POST", "/api/sessions/"+one.ID+"/runs/"+task.ID+"/pause", map[string]any{}), 409)
	requireStatus(t, request(a, "POST", "/api/sessions/"+two.ID+"/runs/"+task.ID+"/cancel", map[string]any{}), 404)
	b, err := os.ReadFile(SessionPath(a.dataPath, one.ID, sessionBucketFor(one)))
	if err != nil {
		t.Fatal(err)
	}
	var stored Session
	if json.Unmarshal(b, &stored) != nil || !stored.Runs[0].StopRequested || stored.Runs[0].PauseRequested {
		t.Fatal("stop must supersede pause durably before cancellation")
	}
	a.mu.Lock()
	unchanged := other.Status == "running" && !other.StopRequested
	a.mu.Unlock()
	if !unchanged {
		t.Fatal("other session changed")
	}
	unblock()
	select {
	case w := <-done:
		requireStatus(t, w, 200)
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
	if calls.Load() != 1 || ctx.Err() != context.Canceled {
		t.Fatal("stop was not single flight")
	}
}

func TestStopEndpointLateTerminalIsNoop(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "done", Status: "completed"}
	s := &Session{ID: newID(), Runs: []*Task{task}}
	var calls atomic.Int32
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.cancels[task.ID] = func() { calls.Add(1) }
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/cancel", map[string]any{}), 200)
	if calls.Load() != 0 || task.StopRequested || task.Status != "completed" {
		t.Fatal("late stop changed completed result")
	}
}

func TestStopIntentSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	work, ref, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{ID: "stop-restart", Runs: []*Task{{ID: "r", Mode: "chat", Status: "running", StopRequested: true, CanResume: true, CheckpointStep: "chat", CheckpointMessages: []Message{{Role: "user", Content: "old"}}, Steps: []Step{{Name: "chat", Content: "partial output"}}}}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	err = a.save(s)
	a.mu.Unlock()
	a.Close()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.mu.Lock()
	defer restored.mu.Unlock()
	got := restored.sessions[s.ID].Runs[0]
	if got.Status != "cancelled" || got.CanResume || len(got.CheckpointMessages) != 0 || got.Steps[0].Content != "partial output" {
		t.Fatalf("stopped run revived or output lost: %+v", got)
	}
}

func TestRunEventsReconnectWhileAwaitingAnswer(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "waiting-stream", Status: "awaiting_clarification"}
	s := &Session{ID: newID(), Runs: []*Task{task}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/sessions/"+s.ID+"/runs/"+task.ID+"/events?access_token="+a.token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	a.publishStream(task.ID, streamEvent{Event: "clarification-resolved"})
	a.publishStream(task.ID, streamEvent{Event: "delta", Text: "continued"})
	a.finishStream(task.ID, "completed", "")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "clarification-resolved") || !strings.Contains(string(body), "continued") {
		t.Fatalf("waiting reconnect closed before continuation: %s", body)
	}
}

func TestStopEndpointSaveFailureKeepsPauseIntent(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "save-failure", Status: "running", PauseRequested: true}
	s := &Session{ID: newID(), Runs: []*Task{task}}
	var calls atomic.Int32
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.cancels[task.ID] = func() { calls.Add(1) }
	a.mu.Unlock()
	original := a.dataPath
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	a.dataPath = blocked
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/cancel", map[string]any{})
	a.dataPath = original
	requireStatus(t, w, 500)
	if calls.Load() != 0 || task.StopRequested || !task.PauseRequested {
		t.Fatal("failed stop save cancelled or changed the pending pause")
	}
}

func TestStopIntentWinsLateProviderSuccess(t *testing.T) {
	a := testApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 && strings.Contains(body.Messages[0].Content, "主题短语") {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "测试"}}}})
			return
		}
		close(entered)
		<-release
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "late result kept"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	w := request(a, "POST", "/api/sessions", map[string]any{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "write a short plain answer"})
	requireStatus(t, w, 202)
	var run Task
	_ = json.Unmarshal(w.Body.Bytes(), &run)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
	// Simulate cancellation acknowledgement racing a provider that has already
	// delivered its complete answer and cannot observe context cancellation.
	a.mu.Lock()
	cancel := a.cancels[run.ID]
	a.cancels[run.ID] = func() {}
	a.mu.Unlock()
	defer cancel()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+run.ID+"/cancel", map[string]any{}), 200)
	unblock()
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	got := a.sessions[s.ID].Runs[0]
	status, canResume, content := got.Status, got.CanResume, got.Steps[0].Content
	a.mu.Unlock()
	if status != "cancelled" || canResume || content != "late result kept" {
		t.Fatalf("late success won stop: %s resume=%t content=%q", status, canResume, content)
	}
}

func TestStopPendingRejectsAnswerAndSteer(t *testing.T) {
	for _, status := range []string{"running", "awaiting_clarification"} {
		t.Run(status, func(t *testing.T) {
			a := testApp(t)
			task := &Task{ID: "stopping", Status: status, StopRequested: true, Steer: make(chan string, 1), AnswerCh: make(chan string, 1)}
			s := &Session{ID: newID(), Runs: []*Task{task}}
			a.mu.Lock()
			a.sessions[s.ID] = s
			a.mu.Unlock()
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "new input"}), 409)
			if status == "awaiting_clarification" {
				requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "yes"}), 409)
			}
			if len(s.Messages) != 0 || len(task.Steer) != 0 || len(task.AnswerCh) != 0 {
				t.Fatal("stopping task accepted more input")
			}
		})
	}
}
