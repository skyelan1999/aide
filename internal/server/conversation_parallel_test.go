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

func TestConversationParallelPlan(t *testing.T) {
	for _, text := range []string{`{"tasks":["a","b"]}`, "```json\n{\"tasks\":[\"a\",\"b\"]}\n```"} {
		parts, err := parseParallelPlan(text, 2)
		if err != nil || len(parts) != 2 {
			t.Fatalf("parse %q: %v", text, err)
		}
	}
	for _, text := range []string{`garbage`, `{"tasks":["a","a"]}`, `{"tasks":[""]}`, `{"tasks":["a","b","c"]}`} {
		if _, err := parseParallelPlan(text, 2); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
func parallelFixture(t *testing.T, limit int) (*App, *Session, *Task) {
	t.Helper()
	a := testApp(t)
	task := &Task{ID: "parallel-fixture", Mode: "chat", Parallelism: limit, Status: "running", Steps: []Step{{Name: "chat", Status: "running"}}}
	s := &Session{ID: "parallel-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	return a, s, task
}
func parallelReply(w http.ResponseWriter, text string) {
	jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
}
func TestConversationParallelFanout(t *testing.T) {
	a, s, task := parallelFixture(t, 5)
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) > 0 {
			t.Error("analysis must not have tools")
		}
		last := body.Messages[len(body.Messages)-1].Content
		if strings.HasPrefix(last, "[parallel planner]") {
			parallelReply(w, `{"tasks":["one","two","three","four","five"]}`)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		parallelReply(w, "analysis-"+strings.Split(last, "\n")[1])
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	input := []Message{{Role: "user", Content: "Analyze five independent sections."}}
	done := make(chan error, 1)
	var merged []Message
	go func() {
		var err error
		merged, err = a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < 5; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("workers did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 5 {
		t.Fatalf("maximum %d", maximum.Load())
	}
	if len(merged) != 2 || !strings.Contains(merged[1].Content, "analysis-five") || !strings.Contains(merged[1].Content, "ONE coherent final answer") {
		t.Fatalf("bad merge: %+v", merged)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.Usage.Total != 120 || len(task.RequestSnapshots) != 6 || task.Parallel.Phase != "merging" || !task.ParallelPrepared {
		t.Fatalf("missing accounting/progress: %+v", task)
	}
	for _, part := range task.Parallel.Parts {
		if part.Status != "completed" {
			t.Fatal(part)
		}
	}
}
func TestConversationParallelFallback(t *testing.T) {
	for _, plan := range []string{`{"tasks":[]}`, `bad json`, `{"tasks":["a","b","c"]}`} {
		t.Run(plan, func(t *testing.T) {
			a, s, task := parallelFixture(t, 2)
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); parallelReply(w, plan) }))
			defer provider.Close()
			input := []Message{{Role: "user", Content: "simple"}}
			merged, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{MaxTokens: 4096})
			if err != nil || len(merged) != 1 || calls.Load() != 1 || task.Parallel.Phase != "fallback" {
				t.Fatalf("fallback failed %v %+v", err, task.Parallel)
			}
		})
	}
}
func TestConversationParallelCancel(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	started := make(chan struct{}, 2)
	var exited atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			parallelReply(w, `{"tasks":["a","b"]}`)
			return
		}
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(4 * time.Second):
		}
		exited.Add(1)
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.prepareParallel(ctx, s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "request"}}, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(4 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop fanout")
	}
	if task.Parallel.Phase != "interrupted" || task.ParallelPrepared {
		t.Fatalf("incorrect cancellation %+v", task.Parallel)
	}
	_ = exited.Load()
}
func TestConversationParallelValidation(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	for _, value := range []int{-1, 6, 100} {
		w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "request", "parallelism": value})
		if w.Code != 400 || !strings.Contains(w.Body.String(), "parallelism") {
			t.Fatal(fmt.Sprint(w.Code, w.Body.String()))
		}
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "workflow", "prompt": "request", "parallelism": 2})
	requireStatus(t, w, 400)
}

func TestConversationParallelEndToEnd(t *testing.T) {
	a := testApp(t)
	var analyses, merged atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		switch {
		case strings.HasPrefix(last, "[parallel planner]"):
			parallelReply(w, `{"tasks":["section-a","section-b","section-c"]}`)
		case strings.HasPrefix(last, "[parallel analysis subtask]"):
			analyses.Add(1)
			if len(body.Tools) > 0 {
				t.Error("worker has tools")
			}
			if strings.Contains(last, "section-b") {
				http.Error(w, "mock failure", 500)
				return
			}
			parallelReply(w, "candidate "+strings.Split(last, "\n")[1])
		case strings.HasPrefix(last, "[parallel analysis results"):
			merged.Add(1)
			if !strings.Contains(last, "candidate section-a") || !strings.Contains(last, "Status: failed") {
				t.Error("missing partial failure evidence")
			}
			parallelReply(w, "One reconciled final answer.")
		default:
			parallelReply(w, "topic")
		}
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Compare independent sections in supplied text.", "parallelism": 3}), 202)
	waitTaskDone(t, a, s.ID)
	response := request(a, "GET", "/api/sessions/"+s.ID, nil)
	var saved Session
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runs) != 1 || analyses.Load() != 3 || merged.Load() != 1 {
		t.Fatalf("unexpected fanout/merge %d/%d %+v", analyses.Load(), merged.Load(), saved)
	}
	task := saved.Runs[0]
	if task.Status != "completed" || task.Parallel.Phase != "completed" || task.Parallel.Parts[1].Status != "failed" || task.Steps[0].Content != "One reconciled final answer." {
		t.Fatalf("bad final task %+v", task)
	}
	// Retry preserves the selected analysis limit, but starts a fresh plan.
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/retry", nil), 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[s.ID].Runs[1].Parallelism != 3 {
		t.Fatal("retry lost parallel limit")
	}
}
func TestConversationParallelBudgetAndResume(t *testing.T) {
	a, s, task := parallelFixture(t, 5)
	a.settings.Models = []ModelRef{{ID: "fixture", ContextWindow: 32}}
	// Too little context must reject planning locally without any network request.
	input := []Message{{Role: "user", Content: "request"}}
	got, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: "http://127.0.0.1:1", Model: "fixture"}, input, ProfileParams{MaxTokens: 4096})
	if err != nil || len(got) != 1 || len(task.RequestSnapshots) != 0 || task.Parallel.Phase != "fallback" {
		t.Fatalf("budget fallback %+v %v", task, err)
	}
	// A persisted prepared briefing must never start a second planner on resume.
	got, err = a.prepareParallel(context.Background(), s, task, Settings{}, input, ProfileParams{})
	if err != nil || len(got) != 1 || len(task.RequestSnapshots) != 0 {
		t.Fatal("prepared checkpoint replayed model requests")
	}
}
