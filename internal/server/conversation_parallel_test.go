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
	task := &Task{ID: "parallel-fixture", Mode: "chat", Parallelism: limit, Prompt: "Analyze independent sections.", Status: "running", Steps: []Step{{Name: "chat", Status: "running"}}}
	s := &Session{ID: "parallel-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	return a, s, task
}
func parallelReply(w http.ResponseWriter, text string) {
	jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
}
func TestConversationParallelFanout(t *testing.T) {
	for _, limit := range []int{5, 10} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { parallelFanout(t, limit) })
	}
}
func parallelFanout(t *testing.T, limit int) {
	a, s, task := parallelFixture(t, limit)
	started := make(chan struct{}, limit)
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
			titles := make([]string, limit)
			for i := range titles {
				titles[i] = fmt.Sprintf("section-%d", i+1)
			}
			plan, _ := json.Marshal(map[string]any{"tasks": titles})
			parallelReply(w, string(plan))
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
	input := []Message{{Role: "user", Content: "Analyze independent sections."}}
	done := make(chan error, 1)
	var merged []Message
	go func() {
		var err error
		merged, err = a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < limit; i++ {
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
	if maximum.Load() != int32(limit) {
		t.Fatalf("maximum %d", maximum.Load())
	}
	if len(merged) != 2 || !strings.Contains(merged[1].Content, fmt.Sprintf("analysis-section-%d", limit)) || !strings.Contains(merged[1].Content, "ONE coherent final answer") {
		t.Fatalf("bad merge: %+v", merged)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.Usage.Total != (limit+1)*20 || len(task.RequestSnapshots) != limit+1 || task.Parallel.Phase != "merging" || !task.ParallelPrepared {
		t.Fatalf("missing accounting/progress: %+v", task)
	}
	if task.Parallel.PlannerContext == nil || task.Parallel.PlannerContext.Usage.Total != 20 || task.Parallel.MainContext == nil || task.Parallel.MainContext.InputEstimate != estimateProviderPromptTokens(merged, a.contextToolsFor(s)) {
		t.Fatal("missing independent planner/merge context")
	}
	for _, part := range task.Parallel.Parts {
		if part.Context == nil || part.Context.Usage.Total != 20 || part.Context.OutputReserve != 2048 || part.Context.ContextWindow <= 0 || part.Context.Usage.Prompt >= task.Usage.Prompt || part.Context.InputEstimate != estimateProviderPromptTokens(parallelWorkerMessages(input, part.Title), nil) {
			t.Fatalf("worker context confused with aggregate %+v", part.Context)
		}
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
	a, s, task := parallelFixture(t, 10)
	started := make(chan struct{}, 10)
	var exited atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			parallelReply(w, `{"tasks":["a","b","c","d","e","f","g","h","i","j"]}`)
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
	for i := 0; i < 10; i++ {
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
	for _, part := range task.Parallel.Parts {
		if part.Status != "cancelled" {
			t.Fatalf("cancel left unfinished worker %+v", part)
		}
	}
	_ = exited.Load()
}
func TestConversationParallelValidation(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	for _, value := range []int{-1, 6, 7, 8, 9, 11, 100} {
		w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "request", "parallelism": value})
		if w.Code != 400 || !strings.Contains(w.Body.String(), "parallelism") {
			t.Fatal(fmt.Sprint(w.Code, w.Body.String()))
		}
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "workflow", "prompt": "request", "parallelism": 2})
	requireStatus(t, w, 400)
}

func TestConversationParallelEndToEnd(t *testing.T) {
	for _, limit := range []int{3, 10} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { parallelEndToEnd(t, limit) })
	}
}
func parallelEndToEnd(t *testing.T, limit int) {
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
			titles := []string{"section-a", "section-b", "section-c"}
			for i := 4; i <= limit; i++ {
				titles = append(titles, fmt.Sprintf("section-%d", i))
			}
			plan, _ := json.Marshal(map[string]any{"tasks": titles})
			parallelReply(w, string(plan))
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
			if !strings.Contains(last, "[original user request]\nCompare independent sections in supplied text.") || !strings.Contains(last, "a completion acknowledgement") {
				t.Error("merge lost original deliverable instruction")
			}
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
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Compare independent sections in supplied text.", "parallelism": limit}), 202)
	waitTaskDone(t, a, s.ID)
	response := request(a, "GET", "/api/sessions/"+s.ID, nil)
	var saved Session
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runs) != 1 || analyses.Load() != int32(limit) || merged.Load() != 1 {
		t.Fatalf("unexpected fanout/merge %d/%d %+v", analyses.Load(), merged.Load(), saved)
	}
	task := saved.Runs[0]
	if task.Status != "completed" || task.Parallel.Phase != "completed" || task.Parallel.Parts[1].Status != "failed" || task.Steps[0].Content != "One reconciled final answer." {
		t.Fatalf("bad final task %+v", task)
	}
	if task.ContextAnchor == nil || task.ContextAnchor.Scope != "parallel_merge" || task.Parallel.MainContext == nil || task.Parallel.MainContext.Usage.Prompt != task.ContextAnchor.Usage.Prompt || task.Parallel.MainContext.Usage.Total >= task.Usage.Total {
		t.Fatal("main request context must be separate from cumulative usage")
	}
	// Retry preserves the selected analysis limit, but starts a fresh plan.
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/retry", nil), 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[s.ID].Runs[1].Parallelism != limit {
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

func TestConversationParallelROS2Document(t *testing.T) {
	a := testApp(t)
	var drafted, merged atomic.Int32
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		switch {
		case strings.HasPrefix(last, "[parallel planner]"):
			if !strings.Contains(last, "General technical knowledge MAY be used") || !strings.Contains(last, "ROS2") {
				t.Error("document planning still limited to supplied attachments")
			}
			parallelReply(w, `{"tasks":["ROS2 architecture","workspace setup","communication","implementation examples","testing and deployment"]}`)
		case strings.HasPrefix(last, "[parallel analysis subtask]"):
			if len(body.Tools) != 0 {
				t.Error("draft workers execute tools")
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			drafted.Add(1)
			parallelReply(w, "ROS2 section draft; distribution-specific commands require verification.")
		case strings.HasPrefix(last, "[parallel analysis results"):
			if !strings.Contains(last, "[original user request]\n请写一个ROS2的文档") || !strings.Contains(last, "a completion acknowledgement") {
				t.Error("merge lost original deliverable instruction")
			}
			merged.Add(1)
			if drafted.Load() != 5 {
				t.Error("merged before five drafts finished")
			}
			parallelReply(w, "# ROS2 技术文档\n\n架构、工作区、通信、示例、测试与部署。发行版相关命令待核实。")
		default:
			parallelReply(w, "ROS2 文档")
		}
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "请写一个ROS2的文档", "parallelism": 5}), 202)
	for i := 0; i < 5; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("ROS2 draft did not reach five simultaneous requests")
		}
	}
	close(release)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.sessions[s.ID].Runs[0]
	if task.Status != "completed" || task.Parallel.Phase != "completed" || len(task.Parallel.Parts) != 5 || drafted.Load() != 5 || merged.Load() != 1 {
		t.Fatalf("wrong ROS2 plan/fanout/merge: %+v drafts=%d merges=%d", task.Parallel, drafted.Load(), merged.Load())
	}
	if !strings.Contains(task.Steps[0].Content, "ROS2 技术文档") {
		t.Fatal("missing single final document")
	}
}
