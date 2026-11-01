package server

import (
	"context"
	"strings"
	"testing"
)

func TestExecutionCollectCompactRetainsExplicitDetail(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	content := strings.Repeat("private candidate implementation\n", 300)
	j := &executionJob{result: ExecutionEnvironmentResult{ID: "finished", Status: "completed", Output: "Worker says checks passed", Changes: []Change{{Path: "module.js", Content: content}}, Tools: []ToolUse{{Tool: "write_file", Args: content}, {Tool: "read_file", Result: content}, {Tool: "bench_check", Result: `{"passed":5,"total":5}`}}}, done: make(chan struct{})}
	close(j.done)
	p.jobs[j.result.ID] = j
	ctx := context.WithValue(context.Background(), executionPoolKey{}, p)
	compact := a.executeToolCall(ctx, readCall("environment_collect", `{"id":"finished"}`), task, nil)
	if strings.Contains(compact, content[:100]) || !strings.Contains(compact, `"contentHash"`) || !strings.Contains(compact, `"verified":false`) || !strings.Contains(compact, "bench_check") {
		t.Fatal("compact collection lost evidence or replayed source", compact)
	}
	detail := a.executeToolCall(ctx, readCall("environment_collect", `{"id":"finished","detail":true}`), task, nil)
	if len(detail) < len(compact)*3 || !strings.Contains(detail, "private candidate implementation") || j.result.Changes[0].Content != content || len(j.result.Tools) != 3 {
		t.Fatal("compact receipt destroyed retained detail")
	}
}

func TestExecutionCollectSelectedPathsPreservesConflictAndApproval(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	for _, name := range []string{"module.js", "REPORT.md"} {
		if e := a.workspace.WriteFile(name, []byte("original"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	task.Files = []Change{{Path: "REPORT.md", Content: "main report", BaseHash: hash([]byte("original"))}}
	j := &executionJob{result: ExecutionEnvironmentResult{ID: "finished", Status: "completed", Changes: []Change{{Path: "module.js", Content: "worker code", BaseHash: hash([]byte("original"))}, {Path: "REPORT.md", Content: "worker report", BaseHash: hash([]byte("original"))}}}, done: make(chan struct{})}
	close(j.done)
	p.jobs[j.result.ID] = j
	if _, e := p.collect(context.Background(), j.result.ID, true, true, nil); e == nil || len(task.Files) != 1 {
		t.Fatal("default batch merged around a conflict", e)
	}
	if _, e := p.collectPaths(context.Background(), j.result.ID, true, true, nil, []string{"outside.js"}); e == nil || len(task.Files) != 1 {
		t.Fatal("unknown candidate was accepted", e)
	}
	r, e := p.collectPaths(context.Background(), j.result.ID, true, true, nil, []string{"module.js"})
	if e != nil || r.Merged || len(r.MergedPaths) != 1 || len(task.Files) != 2 || task.Files[1].Applied {
		t.Fatal("selected candidate lost normal pending semantics", r, e)
	}
	if _, e = p.collectPaths(context.Background(), j.result.ID, true, true, nil, []string{"module.js"}); e != nil || len(task.Files) != 2 {
		t.Fatal("selected candidate replayed", e)
	}
	if _, e = p.collect(context.Background(), j.result.ID, true, true, nil); e == nil || !strings.Contains(e.Error(), "conflict") {
		t.Fatal("remaining report conflict was bypassed", e)
	}
	for _, name := range []string{"module.js", "REPORT.md"} {
		if b, _ := a.workspace.ReadFile(name); string(b) != "original" {
			t.Fatal("collection applied a candidate", name)
		}
	}
}
