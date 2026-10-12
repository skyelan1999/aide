package server

import (
	"testing"
	"time"
)

func TestTaskExecutionBudgetDefaults(t *testing.T) {
	policy := defaultExecutionPolicy(60)
	if got := time.Duration(policy.ChatBudgetSec) * time.Second; got != 2*time.Hour {
		t.Fatalf("chat budget default = %s, want 2h", got)
	}
	if got := time.Duration(policy.WorkflowBudgetSec) * time.Second; got != 15*time.Minute {
		t.Fatalf("workflow budget default = %s, want 15m", got)
	}
	if got := taskExecutionTimeout(&Task{Mode: "chat", ExecutionPolicy: &policy}); got != 2*time.Hour {
		t.Fatalf("chat task timeout = %s, want 2h", got)
	}
	if got := taskExecutionTimeout(&Task{Mode: "workflow", ExecutionPolicy: &policy}); got != 15*time.Minute {
		t.Fatalf("workflow task timeout = %s, want 15m", got)
	}
	if got := taskExecutionTimeout(&Task{Mode: "chat", ExecutionPolicy: &ExecutionPolicy{Version: 1, ChatBudgetSec: 360}}); got != 6*time.Minute {
		t.Fatalf("an existing task policy snapshot should remain unchanged, got %s", got)
	}
	if providerStreamTimeout != 2*time.Hour {
		t.Fatalf("provider stream timeout = %s, want 2h", providerStreamTimeout)
	}
}

func TestTaskResumeRefreshesOnlyModeBudget(t *testing.T) {
	original := ExecutionPolicy{Version: 1, ChatBudgetSec: 360, WorkflowBudgetSec: 900, ToolMaxRounds: 42, CompletionReviews: 2, ResearchCorrections: 1}
	current := ExecutionPolicy{Version: 1, ChatBudgetSec: 7200, WorkflowBudgetSec: 1200, ToolMaxRounds: 60, CompletionReviews: 3, ResearchCorrections: 2}

	chat := taskExecutionPolicyForResume(&Task{Mode: "chat", ExecutionPolicy: &original}, current)
	if chat.ChatBudgetSec != 7200 || chat.WorkflowBudgetSec != 900 || chat.ToolMaxRounds != 42 || chat.CompletionReviews != 2 || chat.ResearchCorrections != 1 {
		t.Fatalf("chat resume policy = %+v; want only chat budget refreshed", chat)
	}
	workflow := taskExecutionPolicyForResume(&Task{Mode: "workflow", ExecutionPolicy: &original}, current)
	if workflow.WorkflowBudgetSec != 1200 || workflow.ChatBudgetSec != 360 || workflow.ToolMaxRounds != 42 || workflow.CompletionReviews != 2 || workflow.ResearchCorrections != 1 {
		t.Fatalf("workflow resume policy = %+v; want only workflow budget refreshed", workflow)
	}
}
