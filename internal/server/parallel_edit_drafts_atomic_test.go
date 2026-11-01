package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type cancelOnSecondCheck struct {
	context.Context
	checks int
}

func (c *cancelOnSecondCheck) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func TestParallelEditDraftsRollbackWholeBatchOnCancellation(t *testing.T) {
	a, _, task := parallelFixture(t, 2)
	task.WorkspaceMode = "local"
	task.WorkspaceID = a.wsID()
	task.WorkspaceRev = a.wsRevision
	task.Parallel = &ParallelProgress{}
	targets := map[string]string{"one": "one.go", "two": "two.go"}
	parts := make([]ParallelPart, 0, len(targets))
	for _, title := range []string{"one", "two"} {
		p := targets[title]
		if err := a.workspace.WriteFile(p, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		a.rememberWorkspaceRead(task, p, []byte("original"))
		draft, _ := json.Marshal(parallelEditDraft{Path: p, Content: "candidate"})
		parts = append(parts, ParallelPart{Title: title, Status: "completed", Scope: "current", DraftPath: p, Content: string(draft)})
	}
	_, bases := a.parallelEditBases(task, targets)
	ctx := context.WithValue(&cancelOnSecondCheck{Context: context.Background()}, parallelEditBudgetKey{}, &parallelEditBudget{Remaining: 2})
	chain, registered, err := a.registerParallelEditDrafts(ctx, task, Settings{Model: "fixture"}, nil, parts, targets, bases, "current")
	if !errors.Is(err, context.Canceled) || registered {
		t.Fatalf("want cancelled unregistered batch, got registered=%v err=%v", registered, err)
	}
	if len(task.Files) != 0 || task.Parallel.DraftProposals != 0 {
		t.Fatalf("partial proposal escaped cancellation rollback: files=%d drafts=%d", len(task.Files), task.Parallel.DraftProposals)
	}
	toolResults := 0
	for _, message := range chain {
		if message.Role == "tool" {
			toolResults++
		}
	}
	if toolResults != 2 {
		t.Fatalf("cancelled batch left dangling tool calls: results=%d", toolResults)
	}
	if len(task.ToolUses) != 1 {
		t.Fatalf("expected an audit record for the actual first dispatch, got %d", len(task.ToolUses))
	}
}
