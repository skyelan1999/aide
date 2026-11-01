package server

import (
	"context"
	"encoding/json"
	"fmt"
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

func queueAssignment(r *http.Request) string {
	var body struct {
		Messages []Message `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	for _, m := range body.Messages {
		if m.Role == "user" {
			if parts := strings.SplitN(m.Content, "Your assignment:\n", 2); len(parts) == 2 {
				return parts[1]
			}
		}
	}
	return ""
}

func TestExecutionWorkQueueFiftyJobsFillVacanciesBeforeCollection(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	_ = a.workspace.WriteFile("original.txt", []byte("parent project"), 0600)
	gate, firstStarted := make(chan struct{}), make(chan struct{})
	var release sync.Once
	var active, peak, fastDone atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if queueAssignment(r) == "job-00" {
			close(firstStarted)
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		} else {
			fastDone.Add(1)
		}
		parallelReply(w, "Scoped work done; no tests claimed.")
	}))
	defer func() { release.Do(func() { close(gate) }); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	specs := []executionAssignment{}
	for i := 0; i < 50; i++ {
		specs = append(specs, executionAssignment{Key: fmt.Sprint(i), Assignment: fmt.Sprintf("job-%02d", i)})
	}
	accepted, err := p.submitBatch(specs)
	if err != nil || len(accepted) != 50 {
		t.Fatal(len(accepted), err)
	}
	select {
	case <-firstStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("first job did not start")
	}
	planAwait(t, "49 jobs finish while first is still blocked; no main collect needed", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		done := 0
		for _, j := range p.jobs {
			if j.result.Status == "completed" {
				done++
			}
		}
		return done == 49
	})
	if fastDone.Load() != 49 || peak.Load() > 4 {
		t.Fatal("wrong occupancy", fastDone.Load(), peak.Load())
	}
	// The normal main executor remains usable while the slow worker is gated.
	ctx := context.WithValue(context.Background(), executionPoolKey{}, p)
	mainResult := a.executeToolCall(ctx, readCall("write_file", `{"path":"integration.txt","content":"main did useful work before all workers finished"}`), task, nil)
	if !strings.Contains(mainResult, "integration.txt") || len(task.Files) != 1 || task.Files[0].Applied {
		t.Fatal("main could not prepare its ordinary proposal during worker execution", mainResult)
	}
	p.mu.Lock()
	slowStillRunning := p.jobs[accepted[0].ID].result.Status == "running"
	p.mu.Unlock()
	if !slowStillRunning {
		t.Fatal("main work happened only after the worker batch ended")
	}
	status, err := p.queueStatus("", 7)
	if err != nil || status["total"] != 50 || len(status["jobs"].([]map[string]any)) != 7 || status["nextAfter"] == "" {
		t.Fatal(status, err)
	}
	page, err := p.queueStatus(status["nextAfter"].(string), 100)
	if err != nil || len(page["jobs"].([]map[string]any)) != 43 || page["nextAfter"] != "" {
		t.Fatal(page, err)
	}
	ready, err := p.collectReady(context.Background(), true, false, 100, nil)
	if err != nil || len(ready) != 49 {
		t.Fatal("waited for slow job", len(ready), err)
	}
	release.Do(func() { close(gate) })
	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r, err := p.collect(waitCtx, accepted[0].ID, true, false, nil); err != nil || r.Status != "completed" {
		t.Fatal(r.Status, err)
	}
	p.close()
	if len(p.uncollected()) != 0 || len(p.slots) != 0 || task.Environments.Peak > 4 {
		t.Fatal("unfinished queue or over capacity")
	}
	for _, r := range task.Environments.Results {
		if !r.Released {
			t.Fatal("runtime leaked", r.ID)
		}
	}
	if b, _ := a.workspace.ReadFile("original.txt"); string(b) != "parent project" {
		t.Fatal("parent project changed")
	}
}

func TestExecutionWorkQueueRejectsInvalidBatchAtomically(t *testing.T) {
	_, _, p := environmentFixture(t, 5)
	for _, batch := range [][]executionAssignment{
		{},
		{{Key: "a", Assignment: "ok"}, {Assignment: " "}},
		{{Key: "a", Assignment: "ok"}, {Key: "a", Assignment: "duplicate"}},
		{{Key: "a", Assignment: "ok", DependsOn: []string{"missing"}}},
		{{Key: "a", Assignment: "ok", DependsOn: []string{"a"}}},
		{{Key: "a", Assignment: "ok", DependsOn: []string{"b"}}, {Key: "b", Assignment: "ok", DependsOn: []string{"a"}}},
	} {
		if _, err := p.submitBatch(batch); err == nil {
			t.Fatal("invalid batch admitted", batch)
		}
		if p.root != "" || len(p.jobs) != 0 || len(p.queue) != 0 || len(p.slots) != 0 {
			t.Fatal("partial execution")
		}
	}
}

func TestExecutionWorkQueueDependencyFailureDoesNotBlockIndependentWork(t *testing.T) {
	_, _, p := environmentFixture(t, 2)
	gate, started := make(chan struct{}), make(chan string, 8)
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := queueAssignment(r)
		started <- name
		if name == "first" {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		parallelReply(w, "done")
	}))
	defer func() { once.Do(func() { close(gate) }); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	jobs, err := p.submitBatch([]executionAssignment{
		{Key: "downstream", Assignment: "must not run", DependsOn: []string{"cancel"}},
		{Key: "first", Assignment: "first"},
		{Key: "cancel", Assignment: "cancel queued"},
		{Key: "independent", Assignment: "independent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-started:
		if got != "first" {
			t.Fatal(got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("head-of-line blocked")
	}
	if err := p.cancelJob(jobs[2].ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r, e := p.collect(ctx, jobs[2].ID, true, false, nil); e != nil || r.Status != "cancelled" || r.StartedAt != "" {
		t.Fatal(r, e)
	}
	if r, e := p.collect(ctx, jobs[0].ID, true, false, nil); e != nil || r.Status != "failed" || r.StartedAt != "" {
		t.Fatal(r, e)
	}
	before := p.withSchedulerReceipt(nil)[0].Content
	if !strings.Contains(before, `"readyQueued":1`) || !strings.Contains(before, `"cancelled":1`) {
		t.Fatal(before)
	}
	once.Do(func() { close(gate) })
	if r, e := p.collect(ctx, jobs[3].ID, true, false, nil); e != nil || r.Status != "completed" {
		t.Fatal(r, e)
	}
	select {
	case got := <-started:
		if got != "independent" {
			t.Fatal(got)
		}
	default:
		t.Fatal("independent job absent")
	}
}

func TestExecutionWorkQueueForwardDependencyAndReadyWaitCancellation(t *testing.T) {
	_, _, p := environmentFixture(t, 3)
	gate, started := make(chan struct{}), make(chan string, 4)
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := queueAssignment(r)
		started <- name
		if name == "prerequisite" {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		parallelReply(w, "done")
	}))
	defer func() { once.Do(func() { close(gate) }); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	jobs, err := p.submitBatch([]executionAssignment{{Key: "child", Assignment: "dependent", DependsOn: []string{"parent"}}, {Key: "parent", Assignment: "prerequisite"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-started:
		if name != "prerequisite" {
			t.Fatal(name)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.collectReady(ctx, true, false, 1, nil); err == nil {
		t.Fatal("wait ignored cancellation")
	}
	status, _ := p.queueStatus("", 32)
	if status["active"] != 1 {
		t.Fatal("blocked dependency consumed a slot", status)
	}
	once.Do(func() { close(gate) })
	ctx2, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if r, e := p.collect(ctx2, jobs[0].ID, true, false, nil); e != nil || r.Status != "completed" {
		t.Fatal(r, e)
	}
	select {
	case name := <-started:
		if name != "dependent" {
			t.Fatal(name)
		}
	default:
		t.Fatal("dependency not dispatched")
	}
}

func TestExecutionWorkQueueReadyMergeKeepsOtherResultsOnConflict(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	_ = a.workspace.WriteFile("x.txt", []byte("before"), 0600)
	for _, id := range []string{"a", "b", "c"} {
		path := "x.txt"
		base := hash([]byte("before"))
		if id == "c" {
			path, base = "new.txt", ""
		}
		j := &executionJob{result: ExecutionEnvironmentResult{ID: id, Status: "completed", Changes: []Change{{Path: path, Content: id, BaseHash: base}}}, done: make(chan struct{})}
		close(j.done)
		p.jobs[id] = j
	}
	rows, err := p.collectReady(context.Background(), false, true, 10, nil)
	if err != nil || len(rows) != 3 || rows[1]["error"] == nil || len(task.Files) != 2 {
		t.Fatal(rows, task.Files, err)
	}
	if b, _ := os.ReadFile(filepath.Join(a.workspace.Name(), "x.txt")); string(b) != "before" {
		t.Fatal("applied pending merge")
	}
	if _, e := p.queueStatus("unknown", 2); e == nil {
		t.Fatal("unknown cursor accepted")
	}
}

func TestExecutionWorkQueueToolDispatchAndWorkerBoundary(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	ctx := context.WithValue(context.Background(), executionPoolKey{}, p)
	// Validation failure returns through the ordinary tool path, not partial jobs.
	out := a.executeToolCall(ctx, readCall("environment_submit_batch", `{"jobs":[{"key":"a","assignment":"x","dependsOn":["missing"]}]}`), task, nil)
	if !strings.Contains(out, "unknown dependency") || len(p.jobs) != 0 {
		t.Fatal(out)
	}
	out = a.executeToolCall(ctx, readCall("environment_queue", `{}`), task, nil)
	if !strings.Contains(out, `"total":0`) {
		t.Fatal(out)
	}
	out = a.executeToolCall(ctx, readCall("environment_collect_ready", `{"wait":true}`), task, nil)
	if strings.TrimSpace(out) != "[]" {
		t.Fatal(out)
	}
	child := &App{executionEnvironment: &executionEnvironment{pool: p, id: "worker"}}
	for _, name := range []string{"environment_submit_batch", "environment_queue", "environment_collect_ready"} {
		out = child.executeEnvironmentTool(ctx, readCall(name, `{"jobs":[{"assignment":"x"}]}`), task, nil)
		if !strings.Contains(out, "only main scheduler") || environmentLocalTool(name) {
			t.Fatal(name, out)
		}
	}
	_, _, one := environmentFixture(t, 1)
	if _, e := one.submitBatch([]executionAssignment{{Assignment: "x"}}); e == nil || one.root != "" {
		t.Fatal("1X created a queue runtime")
	}
}

func TestExecutionWorkQueueCheckpointFailureStartsNothing(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	oldPath := a.dataPath
	badPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badPath, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	a.dataPath = badPath
	defer func() { a.dataPath = oldPath }()
	if _, err := p.submitBatch([]executionAssignment{{Assignment: "never start"}, {Assignment: "also never start"}}); err == nil {
		t.Fatal("checkpoint failure ignored")
	}
	if len(p.jobs) != 0 || len(p.slots) != 0 || len(p.queue) != 0 || len(task.Environments.Results) != 0 {
		t.Fatal("batch partially accepted after failed checkpoint")
	}
}

func TestExecutionWorkQueueConcurrentSubmitAndClose(t *testing.T) {
	_, _, p := environmentFixture(t, 5)
	stop := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer func() { close(stop); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	if _, err := p.submit("initial", nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.submitBatch([]executionAssignment{{Assignment: "queued"}, {Assignment: "queued"}})
		}()
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.close() }()
	}
	wg.Wait()
	if len(p.slots) != 0 || len(p.queue) != 0 {
		t.Fatal("close leaked queue admission")
	}
	for _, j := range p.jobs {
		if j.result.Status == "queued" || j.result.Status == "running" {
			t.Fatal("unfinished accepted job", j.result)
		}
	}
}
