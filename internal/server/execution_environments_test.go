package server

import (
	"aide/internal/execsandbox"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func environmentFixture(t *testing.T, capacity int) (*App, *Task, *executionPool) {
	t.Helper()
	a := testApp(t)
	task := planTask(a, capacity)
	task.Mode = "chat"
	task.WorkspaceRev = a.wsRevision
	task.WorkspaceMode = "local"
	task.Prompt = "Edit and validate assigned scratch files; merge as pending proposals."
	p, err := a.newExecutionPool(context.Background(), task, Settings{Model: "fixture"}, ProfileParams{MaxTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.close)
	return a, task, p
}

func TestExecutionEnvironmentLazyCapacityAndExchange(t *testing.T) {
	for _, capacity := range []int{1, 2, 10} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			a, task, p := environmentFixture(t, capacity)
			if p.root != "" || len(p.jobs) != 0 || p.workerLimitLocked() != capacity-1 || task.Environments.Capacity != capacity {
				t.Fatal("eager allocation or wrong X semantics")
			}
			ctx := context.WithValue(context.Background(), executionPoolKey{}, p)
			out := a.executeToolCall(ctx, readCall("environment_exchange", `{"key":"fact","value":"source A","expectedVersion":0}`), task, nil)
			if !strings.Contains(out, `"author":"main"`) || !strings.Contains(out, `"verified":false`) {
				t.Fatal(out)
			}
			out = a.executeToolCall(ctx, readCall("environment_exchange", `{"key":"fact","value":"overwrite","expectedVersion":0}`), task, nil)
			if !strings.Contains(out, "version conflict") {
				t.Fatal("stale shared write accepted", out)
			}
			if capacity == 1 {
				if _, err := p.submit("work", nil); err == nil {
					t.Fatal("1X allocated a worker")
				}
			}
			if _, err := p.submit("work", []string{"missing"}); err == nil {
				t.Fatal("unknown dependency accepted")
			}
			p.close()
			if p.exchange != nil {
				t.Fatal("exchange leaked")
			}
		})
	}
}

func TestExecutionEnvironmentScratchAndMergeConflicts(t *testing.T) {
	a, task, p := environmentFixture(t, 3)
	if err := a.workspace.WriteFile("one.txt", []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		j := &executionJob{result: ExecutionEnvironmentResult{ID: id, Status: "completed", Released: true, Changes: []Change{{Path: "one.txt", Content: id, BaseHash: hash([]byte("original"))}}}, done: make(chan struct{})}
		close(j.done)
		p.jobs[id] = j
	}
	if _, err := p.collect(context.Background(), "first", true, true, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := a.workspace.ReadFile("one.txt"); string(b) != "original" {
		t.Fatal("merge modified main files")
	}
	if len(task.Files) != 1 || task.Files[0].Content != "first" || task.Files[0].Applied {
		t.Fatal("ordinary proposal missing")
	}
	if _, err := p.collect(context.Background(), "second", true, true, nil); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatal("overlapping proposal overwritten", err)
	}
	a.mu.Lock()
	task.Files = nil
	a.mu.Unlock()
	if err := a.workspace.WriteFile("one.txt", []byte("user edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.collect(context.Background(), "second", true, true, nil); err == nil {
		t.Fatal("stale source merged")
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if _, err := p.collect(context.Background(), "second", true, true, nil); err == nil {
		t.Fatal("project switch merged")
	}
}

func TestExecutionEnvironmentSnapshotDoesNotFollowLinks(t *testing.T) {
	base := t.TempDir()
	out := t.TempDir()
	other := t.TempDir()
	_ = os.WriteFile(filepath.Join(base, "source.txt"), []byte("uncommitted"), 0600)
	_ = os.WriteFile(filepath.Join(other, "private.txt"), []byte("other project"), 0600)
	_ = os.Symlink(other, filepath.Join(base, "other"))
	_ = os.MkdirAll(filepath.Join(base, ".git"), 0700)
	_ = os.WriteFile(filepath.Join(base, ".git", "config"), []byte("private"), 0600)
	hashes, omitted, err := copyExecutionTree(context.Background(), base, out, true)
	if err != nil || hashes["source.txt"] != hash([]byte("uncommitted")) || len(omitted) != 1 {
		t.Fatal(hashes, omitted, err)
	}
	if copied, readErr := os.ReadFile(filepath.Join(out, "source.txt")); readErr != nil || string(copied) != "uncommitted" {
		t.Fatal("snapshot contents changed", string(copied), readErr)
	}
	if _, err := os.Stat(filepath.Join(out, "other")); !os.IsNotExist(err) {
		t.Fatal("symlink copied")
	}
	if _, err := os.Stat(filepath.Join(out, ".git")); !os.IsNotExist(err) {
		t.Fatal("git metadata copied")
	}
}

func TestExecutionEnvironmentWorkerToolLoopAndRelease(t *testing.T) {
	a, task, p := environmentFixture(t, 3)
	_ = a.workspace.WriteFile("one.txt", []byte("original"), 0600)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		step := 0
		for _, m := range body.Messages {
			if m.Role == "tool" {
				step++
			}
		}
		var call ToolCall
		switch step {
		case 0:
			call = readCall("read_file", `{"path":"one.txt"}`)
		case 1:
			call = readCall("write_file", `{"path":"one.txt","content":"candidate"}`)
		case 2:
			call = readCall("read_file", `{"path":"one.txt"}`)
		case 3:
			call = readCall("environment_exchange", `{"key":"proof","value":"scratch readback","expectedVersion":0}`)
		default:
			parallelReply(w, "Scratch file read back as candidate. No tests run.")
			return
		}
		call.ID = newID()
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", ToolCalls: []ToolCall{call}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	}))
	defer provider.Close()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	id, err := p.submit("Edit one.txt to candidate then read it back and publish proof in exchange.", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := p.collect(ctx, id, true, true, nil)
	if err != nil || result.Status != "completed" {
		t.Fatalf("%+v %v", result, err)
	}
	if !result.Released || !result.Merged || len(result.Tools) != 4 || len(result.Changes) != 1 {
		t.Fatalf("missing environment execution evidence: %+v", result)
	}
	if b, _ := a.workspace.ReadFile("one.txt"); string(b) != "original" {
		t.Fatal("worker touched main workspace")
	}
	if len(task.Files) != 1 || task.Files[0].Content != "candidate" {
		t.Fatal("merge did not register candidate")
	}
	if _, err := os.Stat(filepath.Join(p.root, id)); !os.IsNotExist(err) {
		t.Fatal("private runtime not removed")
	}
	if _, err := os.Stat(filepath.Join(result.ArtifactDirectory, "result.json")); err != nil {
		t.Fatal("evidence lost", err)
	}
	if p.exchange["proof"].Author != id {
		t.Fatal("exchange provenance lost")
	}
}

func TestExecutionEnvironmentQueueCancellationAndDependency(t *testing.T) {
	a, _, p := environmentFixture(t, 2)
	var active, peak atomic.Int32
	stop := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer func() { close(stop); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	id, _ := p.submit("wait", nil)
	dependent, _ := p.submit("dependent", []string{id})
	_, _ = p.submit("queued", nil)
	planAwait(t, "one admitted provider", func() bool { return active.Load() == 1 })
	if peak.Load() != 1 {
		t.Fatal("capacity exceeded")
	}
	p.close()
	if len(p.slots) != 0 || a.workspace.Name() != p.source {
		t.Fatal("cleanup changed parent")
	}
	for _, j := range p.jobs {
		select {
		case <-j.done:
		default:
			t.Fatal("worker not joined")
		}
	}
	if p.jobs[dependent].result.Status == "completed" {
		t.Fatal("failed dependency ran")
	}
	if _, err := os.Stat(p.root); !os.IsNotExist(err) {
		t.Fatal("runtime tree leaked")
	}
}

func TestExecutionEnvironmentPrivatePluginAndSandbox(t *testing.T) {
	if err := execsandbox.Available(); err != nil {
		t.Skip(err)
	}
	a, task, p := environmentFixture(t, 2)
	outside := filepath.Join(t.TempDir(), "other-project.txt")
	_ = os.WriteFile(outside, []byte("preserve"), 0600)
	quoted, _ := json.Marshal(outside)
	code := `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'environment_probe',handler:(args,api)=>{let blocked=false;try{fs.writeFileSync(OUTSIDE,'bad')}catch(e){blocked=true}let count=path.join(__dirname,'counter');let n=fs.existsSync(count)?Number(fs.readFileSync(count,'utf8')):0;fs.writeFileSync(count,String(n+1));return {blocked,count:n+1,root:api.workspaceRoot}}})}};`
	code = strings.ReplaceAll(code, "OUTSIDE", string(quoted))
	planInstall(t, a, "private-probe", code)
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0700)
	j := &executionJob{result: ExecutionEnvironmentResult{ID: "probe", Assignment: "probe"}}
	child, worker, _, err := p.newRuntime(dir, work, j)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if child.workPath == a.workPath || child.dataPath == a.dataPath {
		t.Fatal("runtime alias")
	}
	if err := child.loadEnvironmentPlugin(context.Background(), worker, "private-probe"); err != nil {
		t.Fatal(err)
	}
	raw, err := child.callPluginToolContext(context.Background(), "private-probe", "environment_probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(raw)
	if !strings.Contains(string(b), `"blocked":true`) || !strings.Contains(string(b), `"count":1`) {
		t.Fatal(string(b))
	}
	if b, _ := os.ReadFile(outside); string(b) != "preserve" {
		t.Fatal("plugin escaped private runtime")
	}
	if _, err := os.Stat(filepath.Join(a.pluginsPath, "private-probe", "counter")); !os.IsNotExist(err) {
		t.Fatal("plugin state leaked")
	}
	if task.Environments.Capacity != 2 {
		t.Fatal("parent changed")
	}
}

func TestExecutionEnvironmentToolDisclosureAndRecovery(t *testing.T) {
	a, task, p := environmentFixture(t, 1)
	a.mu.Lock()
	normal := a.contextTools()
	one := a.executionToolsLocked(task, append([]any(nil), normal...))
	a.mu.Unlock()
	if len(one) != len(normal) {
		t.Fatal("1X paid an extra schema cost")
	}
	task.Parallelism = 10
	a.mu.Lock()
	many := a.executionToolsLocked(task, append([]any(nil), normal...))
	a.mu.Unlock()
	if len(many) != len(normal)+7 {
		t.Fatal("wrong scheduler capabilities", len(many)-len(normal))
	}
	for _, schema := range many {
		if pluginSchemaName(schema) == "environment_run_queue" {
			t.Fatal("ordinary multi-X chat exposed the blocking experiment")
		}
	}
	task.AgentToolsSet, task.AgentTools = true, []string{"environment_run_queue"}
	a.mu.Lock()
	explicit := a.executionToolsLocked(task, nil)
	a.mu.Unlock()
	found := false
	for _, schema := range filterTaskTools(task, explicit) {
		found = found || pluginSchemaName(schema) == "environment_run_queue"
	}
	if !found {
		t.Fatal("explicit scoped blocking alternative was removed")
	}
	task.AgentToolsSet, task.AgentTools = false, nil
	for _, schema := range executionEnvironmentTools {
		b, _ := json.Marshal(schema)
		if strings.Contains(string(b), `"required":null`) {
			t.Fatal("invalid provider schema")
		}
	}
	p.close()
	task.Environments.Results = []ExecutionEnvironmentResult{{ID: "prior", Status: "running", Assignment: "do not replay"}, {ID: "done", Status: "completed", Output: "prior evidence", Released: true, Collected: true}}
	restored, err := a.newExecutionPool(context.Background(), task, Settings{}, ProfileParams{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if restored.root != "" || restored.jobs["prior"].result.Status != "interrupted" || restored.jobs["done"].result.Output != "prior evidence" {
		t.Fatal("recovery replayed or lost evidence")
	}
	if ids := restored.uncollected(); len(ids) != 1 || ids[0] != "prior" {
		t.Fatal("unknown outcome silently completed", ids)
	}
}

func TestExecutionEnvironmentApprovalQueueUsesOriginalCWD(t *testing.T) {
	a, task, p := environmentFixture(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &App{executionEnvironment: &executionEnvironment{pool: p, id: "first"}}
	second := &App{executionEnvironment: &executionEnvironment{pool: p, id: "second"}}
	answers := make(chan string, 2)
	q := json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"go test ./...","executionIntent":{"cwd":"/private/environment/work"}}`)
	go func() { answers <- first.environmentQuestion(ctx, q) }()
	planAwait(t, "first environment approval", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return task.AnswerCh != nil })
	go func() { answers <- second.environmentQuestion(ctx, q) }()
	a.mu.Lock()
	question := append(json.RawMessage(nil), task.PendingQuestion...)
	a.mu.Unlock()
	if !strings.Contains(string(question), `"environmentId":"first"`) || !strings.Contains(string(question), "/private/environment/work") {
		t.Fatal("approval retargeted", string(question))
	}
	var pending map[string]any
	_ = json.Unmarshal(question, &pending)
	requireStatus(t, request(a, "POST", "/api/sessions/plan-session/runs/plan-task/answer", map[string]any{"answer": "确认", "round": pending["approvalRound"], "questionId": pending["questionId"]}), 200)
	select {
	case got := <-answers:
		if got != "确认" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("approval not delivered")
	}
	planAwait(t, "second approval", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return strings.Contains(string(task.PendingQuestion), `"environmentId":"second"`)
	})
	cancel()
	select {
	case <-answers:
	case <-time.After(3 * time.Second):
		t.Fatal("queued approval cancellation stuck")
	}
	a.mu.Lock()
	pendingAfter := len(task.PendingQuestion)
	a.mu.Unlock()
	if pendingAfter != 0 {
		t.Fatal("cancelled question remained active")
	}
}

func TestExecutionEnvironmentSingleCancelDoesNotCancelPool(t *testing.T) {
	_, task, p := environmentFixture(t, 2)
	stop := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer func() { close(stop); provider.Close() }()
	p.cfg = Settings{Model: "fixture", BaseURL: provider.URL}
	id, err := p.submit("wait", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.jobs[id].cancel()
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := p.collect(ctx, id, true, false, nil)
	if err != nil || result.Status != "cancelled" || !result.Released || p.ctx.Err() != nil {
		t.Fatal(result, err)
	}
	if task.Environments.Active != 0 {
		t.Fatal("slot leaked")
	}
}

func TestExecutionEnvironmentDaemonInstancesArePrivate(t *testing.T) {
	if err := execsandbox.Available(); err != nil {
		t.Skip(err)
	}
	a, _, p := environmentFixture(t, 3)
	installMockDaemon(t, a, "environment-daemon", true)
	var pids []int
	for _, id := range []string{"left", "right"} {
		dir := t.TempDir()
		work := filepath.Join(dir, "work")
		_ = os.MkdirAll(work, 0700)
		child, worker, _, err := p.newRuntime(dir, work, &executionJob{result: ExecutionEnvironmentResult{ID: id}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(child.Close)
		if err := child.loadEnvironmentPlugin(context.Background(), worker, "environment-daemon"); err != nil {
			t.Fatal(err)
		}
		raw, err := child.callPluginToolContext(context.Background(), "environment-daemon", "echo", map[string]any{"msg": id})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(raw)
		if !strings.Contains(string(encoded), id) {
			t.Fatal(string(encoded))
		}
		child.daemons.mu.Lock()
		proc := child.daemons.procs["environment-daemon"]
		child.daemons.mu.Unlock()
		proc.mu.Lock()
		pids = append(pids, proc.cmd.Process.Pid)
		proc.mu.Unlock()
	}
	if pids[0] == pids[1] {
		t.Fatal("shared daemon process")
	}
	for _, status := range a.daemons.List() {
		if status.Status != "stopped" {
			t.Fatal("worker started a parent daemon", status)
		}
	}
}
