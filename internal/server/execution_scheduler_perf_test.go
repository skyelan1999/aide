package server

import (
	"strings"
	"testing"
)

func TestExecutionSchedulerInstructionUsesMeasuredGranularityGuidance(t *testing.T) {
	task := &Task{Mode: "chat", Parallelism: 5}
	instruction := executionSchedulerInstruction(task)
	for _, want := range []string{
		"5–10 substantial assignments for 5X capacity",
		"up to 4 private worker environments",
		"both return immediately",
		"Do not turn main into a polling-only manager",
		"wait=true only when no useful main work remains",
		"Group tiny related items",
		"not a cap or speed guarantee",
		"10X all-slot samples were slower",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("scheduler instruction missing %q", want)
		}
	}
}

func TestExecutionSchedulerAsyncDefaultAndExplicitBlockingAlternative(t *testing.T) {
	task := &Task{Mode: "chat", Parallelism: 10}
	if strings.Contains(executionSchedulerInstruction(task), "environment_run_queue") {
		t.Fatal("default instruction recommends the blocking experiment")
	}
	task.AgentTools = []string{"environment_run_queue"}
	if executionBlockingQueueEnabled(task) {
		t.Fatal("unscoped tool list opted in")
	}
	task.AgentToolsSet = true
	if !strings.Contains(executionSchedulerInstruction(task), "optional blocking alternative") {
		t.Fatal("explicit scoped compatibility path missing")
	}
	h := defaultHarnessConfig()
	h.DenyTools = []string{"environment_run_queue"}
	task.HarnessConfig = &h
	if executionBlockingQueueEnabled(task) {
		t.Fatal("explicit tool list bypassed harness denial")
	}
	for _, unsupported := range []*Task{nil, {Mode: "chat", Parallelism: 1}, {Mode: "chat", Parallelism: 5, WorkspaceMode: "ssh"}, {Mode: "workflow", Parallelism: 5}} {
		if executionSchedulerInstruction(unsupported) != "" {
			t.Fatal("ordinary execution path acquired scheduling instructions")
		}
	}
}

func TestExecutionSchedulerCurrentPolicyOnResumeWithoutPreviewDuplication(t *testing.T) {
	policy := executionSchedulerInstruction(&Task{Mode: "chat", Parallelism: 5})
	preview := []Message{{Role: "system", Content: "base policy\n" + policy + "\nother tool instructions"}}
	if got := withExecutionSchedulerInstruction(preview, policy); len(got) != 1 {
		t.Fatal("current preview policy duplicated")
	}
	old := []Message{{Role: "system", Content: "[execution environment scheduler] prefer synchronous queue"}, {Role: "user", Content: "continue"}}
	current := withExecutionSchedulerInstruction(old, policy)
	if len(current) != 3 || current[2].Content != policy || len(old) != 2 {
		t.Fatal("resumed checkpoint did not receive current async policy")
	}
	if len(withExecutionSchedulerInstruction(current, policy)) != 3 {
		t.Fatal("current policy appended repeatedly")
	}
	if got := withExecutionSchedulerInstruction(old, ""); len(got) != len(old) || &got[0] != &old[0] {
		t.Fatal("ordinary path changed")
	}
}
