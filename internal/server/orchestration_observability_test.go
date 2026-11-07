package server

import (
	"context"
	"testing"
	"time"
)

func TestOrchestrationEventsContinueAfterSnapshotLimit(t *testing.T) {
	a := &App{}
	task := &Task{SnapshotsTruncated: true}
	start := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxOrchestrationEvents+2; i++ {
		a.appendOrchestrationEventLocked(task, "model_request", "chat", start, start.Add(time.Millisecond), "completed")
	}
	if len(task.OrchestrationEvents) != maxOrchestrationEvents || task.OrchestrationDropped != 2 {
		t.Fatalf("bounded ledger: retained=%d dropped=%d", len(task.OrchestrationEvents), task.OrchestrationDropped)
	}
	if task.OrchestrationEvents[0].ElapsedMillis != 1 || len(task.RequestSnapshots) != 0 {
		t.Fatalf("timing event or body snapshot changed: %+v", task.OrchestrationEvents[0])
	}
}

func TestRecentOrchestrationCostsAreBounded(t *testing.T) {
	task := &Task{}
	for i := int64(1); i <= 10; i++ {
		task.OrchestrationEvents = append(task.OrchestrationEvents, OrchestrationEvent{Kind: "snapshot_copy", ElapsedMillis: i})
	}
	cost := recentOrchestrationCosts(task)["snapshot_copy"]
	if cost.Samples != 8 || cost.AverageMillis != 6 || cost.MaximumMillis != 10 {
		t.Fatalf("recent cost includes stale samples or wrong maximum: %+v", cost)
	}
}

func TestOrchestrationStageCannotLeakUntrustedName(t *testing.T) {
	if got := orchestrationStage("tool_call", "SECRET user supplied function name"); got != "other" {
		t.Fatalf("untrusted name entered timing ledger: %q", got)
	}
	if got := orchestrationStage("model_request", "parallel/wave-2/part-4"); got != "parallel_worker" {
		t.Fatalf("parallel phase lost: %q", got)
	}
}

func TestOrchestrationReceiptsDoNotPromoteWorkerClaims(t *testing.T) {
	task := &Task{
		Parallel:     &ParallelProgress{DraftProposals: 1, Parts: []ParallelPart{{Title: "draft", Status: "completed", DraftPath: "a.go", ElapsedMillis: 42}}},
		Files:        []Change{{Path: "a.go"}},
		Environments: &ExecutionEnvironmentProgress{Results: []ExecutionEnvironmentResult{{Status: "completed", Collected: true, MergedPaths: []string{"a.go"}, Changes: []Change{{Path: "a.go"}}, Tools: []ToolUse{{Tool: "run_shell"}}}}},
	}
	receipts := orchestrationReceipts(task)
	if len(receipts) != 2 || receipts[0].Lane != "analysis" || receipts[1].Lane != "environment" {
		t.Fatalf("parallel paths not normalized: %+v", receipts)
	}
	if !receipts[0].Proposed || !receipts[1].Proposed || !receipts[1].Collected || receipts[0].Verified || receipts[1].Verified {
		t.Fatalf("worker result promoted to verified: %+v", receipts)
	}
	if receipts[1].ToolCount != 1 || receipts[1].ChangeCount != 1 {
		t.Fatalf("environment evidence counts missing: %+v", receipts[1])
	}
}

func TestParallelSmallTaskGate(t *testing.T) {
	if !parallelSmallTask("你好", []Message{{Role: "user", Content: "你好"}}) {
		t.Fatal("single short request should not pay planner overhead")
	}
	if parallelSmallTask("比较 A 和 B", nil) || parallelSmallTask("Compare A and B", nil) {
		t.Fatal("multiple independent targets must remain eligible")
	}
	if parallelSmallTask("请写一个ROS2的文档", nil) {
		t.Fatal("short wording can still describe a substantial deliverable")
	}
	if parallelSmallTask("你好", []Message{{Role: "tool", Content: "new evidence"}}) {
		t.Fatal("new tool evidence should allow reevaluation")
	}
}

func TestShortParallelRequestSkipsPlannerRequest(t *testing.T) {
	a, session, task := parallelFixture(t, 5)
	task.Prompt = "你好"
	input := []Message{{Role: "user", Content: task.Prompt}}
	got, err := a.prepareParallel(context.Background(), session, task, Settings{BaseURL: "http://127.0.0.1:1", Model: "fixture"}, input, ProfileParams{MaxTokens: 512})
	if err != nil || len(got) != 1 || len(task.RequestSnapshots) != 0 {
		t.Fatalf("short request unexpectedly called planner: input=%d snapshots=%d err=%v", len(got), len(task.RequestSnapshots), err)
	}
	if task.Parallel.Phase != "fallback" || !task.ParallelPrepared {
		t.Fatalf("short request fallback not checkpointed: %+v", task.Parallel)
	}
}
