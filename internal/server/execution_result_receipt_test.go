package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cancelAfterEnvironmentProposal struct {
	context.Context
	task *Task
}

func (c cancelAfterEnvironmentProposal) Err() error {
	if len(c.task.Files) > 1 {
		return context.Canceled
	}
	return nil
}

type failEnvironmentCheckpoint struct {
	context.Context
	task         *Task
	breakJournal func()
	fired        bool
}

func (c *failEnvironmentCheckpoint) Err() error {
	if len(c.task.Files) > 1 && !c.fired {
		c.fired = true
		c.breakJournal()
	}
	return nil
}

func TestExecutionCollectPartialFailureRetainsIdempotentReceipts(t *testing.T) {
	for _, mode := range []string{"cancel", "after-hook", "checkpoint", "receipt-write"} {
		t.Run(mode, func(t *testing.T) {
			a, task, p := environmentFixture(t, 5)
			prior := Change{Path: "prior.txt", Content: "user's pending proposal"}
			task.Files = []Change{prior}
			j := &executionJob{result: ExecutionEnvironmentResult{ID: "finished", Status: "completed", Changes: []Change{{Path: "one.txt", Content: "one"}, {Path: "two.txt", Content: "two"}}}, done: make(chan struct{})}
			close(j.done)
			p.jobs[j.result.ID] = j
			ctx := context.Context(context.Background())
			switch mode {
			case "cancel":
				ctx = cancelAfterEnvironmentProposal{ctx, task}
			case "after-hook":
				// Shell denial makes the actual after-tool hook fail without running
				// any command or requiring a synthetic approval response.
				task.AgentToolsSet, task.AgentTools = true, []string{"write_file"}
				task.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Name: "blocked", Event: "after_tool", Tool: "write_file", Command: "pwd", Enabled: true}}}
			case "checkpoint":
				ctx = &failEnvironmentCheckpoint{Context: ctx, task: task, breakJournal: func() {
					journal := filepath.Join(a.dataPath, "execution-events", task.ID+".jsonl")
					if err := os.Rename(journal, journal+".saved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(journal, 0700); err != nil {
						t.Fatal(err)
					}
				}}
			case "receipt-write":
				j.result.ArtifactDirectory = filepath.Join(t.TempDir(), "blocked")
				if err := os.WriteFile(j.result.ArtifactDirectory, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, err := p.collect(ctx, j.result.ID, true, true, nil)
			if err == nil || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("missing injected failure: %v", err)
			}
			if len(task.Files) != 2 || task.Files[0] != prior || r.Merged || r.Collected || len(r.MergedPaths) != 1 || r.MergedPaths[0] != "one.txt" || len(j.result.MergedPaths) != 1 {
				t.Fatalf("partial receipt lost: files=%v result=%+v", task.Files, r)
			}
			// The authoritative session persists proposals and receipts together.
			s := a.sessions["plan-session"]
			b, err := os.ReadFile(SessionPath(a.dataPath, s.ID, sessionBucketFor(s)))
			if err != nil {
				t.Fatal(err)
			}
			var saved Session
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			if len(saved.Runs[0].Files) != 2 || len(saved.Runs[0].Environments.Results[0].MergedPaths) != 1 {
				t.Fatal("session persistence split proposals and receipt")
			}
			if mode == "receipt-write" {
				if err := os.Remove(j.result.ArtifactDirectory); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(j.result.ArtifactDirectory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "checkpoint" {
				journal := filepath.Join(a.dataPath, "execution-events", task.ID+".jsonl")
				if err := os.Remove(journal); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(journal+".saved", journal); err != nil {
					t.Fatal(err)
				}
			}
			task.HarnessConfig = nil
			// Restore into a fresh pool using the same recovery path as a new turn.
			p.close()
			task.Environments = saved.Runs[0].Environments
			resumed, err := a.newExecutionPool(context.Background(), task, Settings{Model: "fixture"}, ProfileParams{MaxTokens: 1024})
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.close()
			r, err = resumed.collect(context.Background(), j.result.ID, true, true, nil)
			if err != nil || !r.Merged || !r.Collected || len(r.MergedPaths) != 2 || len(task.Files) != 3 || len(task.ToolUses) != 2 {
				t.Fatalf("retry duplicated or conflicted: result=%+v files=%v err=%v", r, task.Files, err)
			}
			for _, file := range task.Files {
				if file.Applied {
					t.Fatal("merge bypassed approval")
				}
				if _, err := a.workspace.ReadFile(file.Path); !os.IsNotExist(err) {
					t.Fatal("merge applied a pending proposal", file.Path, err)
				}
			}
		})
	}
}

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
