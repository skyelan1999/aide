package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutionQueueDrainAllTenSlotsWithoutManager(t *testing.T) {
	a, task, p := environmentFixture(t, 10)
	gate, started := make(chan struct{}), make(chan struct{}, 50)
	var once sync.Once
	var active, peak, requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if queueAssignment(r) == "" {
			t.Error("non-worker inference during drain")
		}
		n := active.Add(1)
		defer active.Add(-1)
		requests.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		parallelReply(w, "Scoped result; no test claims.")
	}))
	defer func() { once.Do(func() { close(gate) }); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	specs := make([]executionAssignment, 50)
	for i := range specs {
		specs[i] = executionAssignment{Assignment: "one independent task"}
	}
	done := make(chan error, 1)
	var receipt map[string]any
	go func() {
		var err error
		receipt, err = p.runQueue(context.Background(), specs, "random", false, nil)
		done <- err
	}()
	for i := 0; i < 10; i++ {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("did not use all ten slots")
		}
	}
	a.mu.Lock()
	paused, capacity, running := task.Environments.MainPaused, task.Environments.WorkerCapacity, task.Environments.Active
	a.mu.Unlock()
	if !paused || capacity != 10 || running != 10 || peak.Load() != 10 {
		t.Fatal("reserved manager or oversubscribed", paused, capacity, running, peak.Load())
	}
	once.Do(func() { close(gate) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("drain did not finish")
	}
	if requests.Load() != 50 || peak.Load() > 10 || receipt["collectedJobs"] != 50 || receipt["uncollectedJobs"] != 0 || receipt["previewTruncated"] != true {
		t.Fatal(requests.Load(), peak.Load(), receipt)
	}
	status, _ := p.queueStatus("", 100)
	if status["mainPaused"] != false || status["workerCapacity"] != 9 || status["active"] != 0 {
		t.Fatal("borrowed slot not returned", status)
	}
	for _, r := range task.Environments.Results {
		if !r.Released || !r.Collected {
			t.Fatal("worker retained runtime or lost evidence", r.ID)
		}
	}
}

func TestExecutionQueueDrainCancelsAndJoinsBeforeResumingMain(t *testing.T) {
	_, task, p := environmentFixture(t, 5)
	stop, started := make(chan struct{}), make(chan struct{}, 10)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = queueAssignment(r)
		started <- struct{}{}
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	defer func() { close(stop); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	specs := make([]executionAssignment, 20)
	for i := range specs {
		specs[i].Assignment = "cancel this bounded job"
	}
	go func() { _, err := p.runQueue(ctx, specs, "random", false, nil); done <- err }()
	for i := 0; i < 5; i++ {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("not all slots working")
		}
	}
	if _, err := p.runQueue(context.Background(), nil, "random", false, nil); err == nil {
		t.Fatal("nested drain admitted")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation hidden")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel failed to join")
	}
	if len(p.slots) != 0 || len(p.queue) != 0 || task.Environments.MainPaused {
		t.Fatal("returned while borrowed-slot work survived")
	}
	for _, j := range p.jobs {
		if j.result.Status == "running" || j.result.Status == "queued" {
			t.Fatal("leaked job", j.result.ID)
		}
	}
}

func TestExecutionQueueDrainValidationDependenciesAndConflict(t *testing.T) {
	a, task, p := environmentFixture(t, 3)
	ctx := context.WithValue(context.Background(), executionPoolKey{}, p)
	blocked := a.executeToolCall(ctx, readCall("environment_run_queue", `{}`), task, nil)
	if !strings.Contains(blocked, "blocking queue is not enabled") || p.draining || len(p.jobs) != 0 {
		t.Fatal("default chat silently used blocking scheduling", blocked)
	}
	task.AgentToolsSet, task.AgentTools = true, []string{"environment_run_queue", "write_file"}
	out := a.executeToolCall(ctx, readCall("environment_run_queue", `{"jobs":[{"assignment":"never","dependsOn":["missing"]}]}`), task, nil)
	if !strings.Contains(out, "unknown dependency") || p.draining || len(p.jobs) != 0 {
		t.Fatal("invalid drain changed state", out)
	}
	if _, err := p.runQueue(context.Background(), nil, "unknown", false, nil); err == nil {
		t.Fatal("unknown queue order")
	}
	_ = a.workspace.WriteFile("same.txt", []byte("original"), 0600)
	for _, id := range []string{"a", "b"} {
		j := &executionJob{done: make(chan struct{}), result: ExecutionEnvironmentResult{ID: id, Status: "completed", Changes: []Change{{Path: "same.txt", BaseHash: hash([]byte("original")), Content: id}}}}
		close(j.done)
		p.jobs[id] = j
	}
	r, err := p.runQueue(context.Background(), nil, "fifo", true, nil)
	if err != nil || r["totalCollectionErrors"] != 1 || r["uncollectedJobs"] != 1 || len(task.Files) != 1 {
		t.Fatal("conflict hidden or endlessly retried", r, err)
	}
	if b, _ := a.workspace.ReadFile("same.txt"); string(b) != "original" {
		t.Fatal("drain applied a candidate")
	}
	child := &App{executionEnvironment: &executionEnvironment{pool: p, id: "worker"}}
	out = child.executeEnvironmentTool(ctx, readCall("environment_run_queue", `{}`), task, nil)
	if !strings.Contains(out, "only main") || environmentLocalTool("environment_run_queue") {
		t.Fatal("worker borrowed another slot", out)
	}
	_, _, one := environmentFixture(t, 1)
	if _, err := one.runQueue(context.Background(), nil, "random", false, nil); err == nil || one.root != "" {
		t.Fatal("1X created a worker")
	}
}

func TestExecutionQueueDrainRandomReadyRespectsDependencies(t *testing.T) {
	_, _, p := environmentFixture(t, 5)
	var parentDone atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch queueAssignment(r) {
		case "prerequisite":
			parentDone.Store(true)
		case "dependent":
			if !parentDone.Load() {
				t.Error("random dispatched blocked dependency")
			}
		default:
			t.Error("failed dependency was executed")
		}
		parallelReply(w, "done")
	}))
	defer provider.Close()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	r, err := p.runQueue(context.Background(), []executionAssignment{{Key: "child", Assignment: "dependent", DependsOn: []string{"parent"}}, {Key: "parent", Assignment: "prerequisite"}}, "random", false, nil)
	if err != nil || r["collectedJobs"] != 2 {
		t.Fatal(r, err)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "Scoped result") {
		t.Fatal("full outputs in compact manifest")
	}
}
