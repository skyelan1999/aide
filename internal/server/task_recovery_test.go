package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const recoveryURL = "/api/sessions/effect-session/runs/effect-run"

func recoveryFixture(t *testing.T) (*App, *Task, ToolCall) {
	a, task, _ := effectCheckFixture(t)
	call := planCall(t, "effect-plan", []pluginPlanNodeSpec{{ID: "effect-node", Tool: "recovery_probe", Arguments: map[string]any{"marker": "old"}}})
	task.Mode = "chat"
	task.Prompt = "Continue fixture work without replay"
	task.CanResume = true
	task.CheckpointStep = "chat"
	task.CheckpointMessages = []Message{{Role: "user", Content: task.Prompt}, {Role: "assistant", ToolCalls: []ToolCall{call}}}
	task.ExecutionSequence = 2
	task.PluginPlans[0].Digest = hash([]byte(call.Function.Arguments))
	task.PluginPlans[0].Nodes[0].Tool = "recovery_probe"
	task.PluginPlans[0].Nodes[0].Arguments = map[string]any{"marker": "old"}
	task.ToolExecutionIntents = []ToolExecutionIntent{{CallID: "effect-plan/effect-node", Tool: "recovery_probe", State: "dispatched"}}
	return a, task, call
}
func recoveryCheck(t *testing.T, a *App, task *Task) {
	x := effectCheckInput(len(task.PluginPlans[0].Nodes[0].EffectChecks))
	x["planDigest"] = task.PluginPlans[0].Digest
	requireStatus(t, request(a, "POST", effectCheckURL, x), 200)
}
func recoveryRequest(review taskRecoveryReview) map[string]any {
	return map[string]any{"recovery": map[string]any{"snapshot": review.Snapshot, "decision": "continue_without_replay", "note": "private-recovery-note: operator checked fixture and permits only future work"}}
}
func recoveryWait(t *testing.T, a *App, id string) *Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		s := a.sessions["effect-session"]
		var task *Task
		for _, r := range s.Runs {
			if r.ID == id {
				b, _ := json.Marshal(r)
				_ = json.Unmarshal(b, &task)
			}
		}
		a.mu.Unlock()
		if task != nil && task.Status != "running" {
			if task.Status != "completed" {
				t.Fatalf("successor failed: %+v", task)
			}
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("successor did not finish")
	return nil
}

func TestTaskRecoveryPreviewAndGuardedResume(t *testing.T) {
	a, task, _ := recoveryFixture(t)
	w := request(a, "GET", recoveryURL+"/recovery", nil)
	requireStatus(t, w, 200)
	var review taskRecoveryReview
	_ = json.Unmarshal(w.Body.Bytes(), &review)
	if !review.Required || review.Ready || !review.HookWarning || len(review.Plans) != 1 {
		t.Fatal(review)
	}
	if strings.Contains(w.Body.String(), "secret-argument") || strings.Contains(w.Body.String(), "private-check-evidence") || strings.Contains(w.Body.String(), "arguments") {
		t.Fatal("preview leaked private data")
	}
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", nil), 409)
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(review)), 409)
	recoveryCheck(t, a, task)
	ready := a.taskRecoveryReviewLocked(task)
	if !ready.Ready {
		t.Fatal(ready)
	}
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(review)), 409)
	recoveryCheck(t, a, task)
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(ready)), 409)
	latest := a.taskRecoveryReviewLocked(task)
	a.wsRevision++
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(latest)), 409)
	latest = a.taskRecoveryReviewLocked(task)
	task.WorkspaceID = "different-workspace"
	requireStatus(t, request(a, "GET", recoveryURL+"/recovery", nil), 409)
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(latest)), 409)
	task.WorkspaceID = ""
	bad := recoveryRequest(latest)
	bad["source"] = "system_verified"
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", bad), 400)
	bad = recoveryRequest(latest)
	bad["recovery"].(map[string]any)["note"] = ""
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", bad), 400)
	requireStatus(t, request(a, "GET", strings.Replace(recoveryURL, "effect-session", "other", 1)+"/recovery", nil), 404)
	a.cancels[task.ID] = func() {}
	requireStatus(t, request(a, "GET", recoveryURL+"/recovery", nil), 409)
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(latest)), 409)
	delete(a.cancels, task.ID)
	requireStatus(t, request(a, "POST", recoveryURL+"/retry", nil), 409)
	if task.Status != "interrupted" || len(a.sessions["effect-session"].Runs) != 1 {
		t.Fatal("rejected review changed original")
	}
}

func TestTaskRecoveryContinuesNewGraphNeverReplaysOld(t *testing.T) {
	a, task, old := recoveryFixture(t)
	planInstall(t, a, "recovery-fixture", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'recovery_probe',handler:args=>{fs.appendFileSync(path.join(__dirname,args.marker+'.marker'),'called\n');return 'new work returned';}})}};`)
	recoveryCheck(t, a, task)
	review := a.taskRecoveryReviewLocked(task)
	newCall := planCall(t, "new-plan", []pluginPlanNodeSpec{{ID: "fresh", Tool: "recovery_probe", Arguments: map[string]any{"marker": "new"}}})
	provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) { return "", []ToolCall{old} }, func() (string, []ToolCall) { return "", []ToolCall{newCall} }, func() (string, []ToolCall) { return "New work complete; old effects remain unverified.", nil }})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	w := request(a, "POST", recoveryURL+"/resume", recoveryRequest(review))
	requireStatus(t, w, 202)
	var result Task
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	done := recoveryWait(t, a, result.ID)
	root := filepath.Join(a.pluginsPath, "recovery-fixture")
	if planExists(filepath.Join(root, "old.marker")) {
		t.Fatal("old handler replayed")
	}
	b, err := os.ReadFile(filepath.Join(root, "new.marker"))
	if err != nil || string(b) != "called\n" {
		diagnostic, _ := json.Marshal(map[string]any{"plans": done.PluginPlans, "uses": done.ToolUses, "steps": done.Steps})
		t.Fatal("new graph did not execute exactly once", string(b), err, string(diagnostic))
	}
	if len(done.PluginPlans) != 2 || done.PluginPlans[0].Nodes[0].State != "uncertain" || done.PluginPlans[1].Nodes[0].State != "succeeded" {
		t.Fatal("old/new truth mixed", done.PluginPlans)
	}
	if !recoveryReceiptValid(done.RecoveryContinuation) || done.HookRecoveryRequired || done.ResumedFrom != task.ID || !task.HookRecoveryRequired {
		t.Fatal("recovery receipt or original modified")
	}
	for _, messages := range provider.snapshotRequests() {
		for _, msg := range messages {
			if strings.Contains(msg.Content, "private-recovery-note") || strings.Contains(msg.Content, "private-check-evidence") {
				t.Fatal("private note/evidence sent to model")
			}
		}
	}
	requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(review)), 409)
	out := request(a, "GET", "/api/sessions/effect-session/runs/"+done.ID+"/outcome", nil)
	requireStatus(t, out, 200)
	if strings.Contains(out.Body.String(), "private-recovery-note") || !strings.Contains(out.Body.String(), "operator_decision") {
		t.Fatal("outcome consent provenance/privacy", out.Body.String())
	}
	var projection struct {
		Summary struct {
			Unknown int `json:"unknownOutcomes"`
		} `json:"summary"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &projection)
	if projection.Summary.Unknown != 1 {
		t.Fatal("authorization invented prior tool success")
	}
	restarted, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	saved := restarted.sessions["effect-session"].Runs[1]
	if !pluginPlanContinued(saved, saved.PluginPlans[0]) || taskRecoveryPreview(saved).Required {
		t.Fatal("consent not durable")
	}
	saved.PluginPlans[0].Nodes[0].Error = "changed after consent"
	if pluginPlanContinued(saved, saved.PluginPlans[0]) {
		t.Fatal("changed plan inherited consent")
	}
}

func TestTaskRecoveryGenericCallsAndCurrentPolicy(t *testing.T) {
	call := readCall("run_shell", `{"command":"private old action"}`)
	call.ID = "generic-old"
	task := &Task{ID: "old", Status: "interrupted", ExecutionSequence: 2, CheckpointMessages: []Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}, ToolExecutionIntents: []ToolExecutionIntent{{CallID: call.ID, Tool: "run_shell", State: "dispatched"}}}
	review := taskRecoveryPreview(task)
	if !review.Required || !review.Ready || len(review.Calls) != 1 {
		t.Fatal(review)
	}
	recoverExecutionCheckpoint(task)
	applyRecoveryContinuation(task, "old", review, &taskRecoveryDecision{Snapshot: review.Snapshot, Decision: "continue_without_replay", Note: "private note"})
	if taskRecoveryPreview(task).Required {
		t.Fatal("reviewed synthetic result repeatedly blocks continuation")
	}
	task.ToolExecutionIntents = []ToolExecutionIntent{{CallID: call.ID, Tool: "run_shell", State: "dispatched"}}
	if !taskRecoveryPreview(task).Required {
		t.Fatal("new pending call bypassed review using old identity")
	}
	task.ToolExecutionIntents = nil
	task.HookRecoveryRequired = true
	if !taskRecoveryPreview(task).Required {
		t.Fatal("new hook warning inherited old consent")
	}
	a, graph, _ := recoveryFixture(t)
	recoveryCheck(t, a, graph)
	r := taskRecoveryPreview(graph)
	recoverExecutionCheckpoint(graph)
	applyRecoveryContinuation(graph, graph.ID, r, &taskRecoveryDecision{Snapshot: r.Snapshot, Decision: "continue_without_replay", Note: "checked"})
	planInstall(t, a, "recovery-policy", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'recovery_probe',handler:()=>{fs.writeFileSync(path.join(__dirname,'bad'),'1');return 'bad';}})}};`)
	a.settings.SandboxMode = "read-only"
	newCall := planCall(t, "policy-new", []pluginPlanNodeSpec{{ID: "fresh", Tool: "recovery_probe", Arguments: map[string]any{}}})
	result := a.executePluginPlan(context.Background(), newCall, graph, map[string]Change{})
	if planExists(filepath.Join(a.pluginsPath, "recovery-policy", "bad")) || !strings.Contains(result, "错误") {
		t.Fatal("continuation bypassed sandbox", result)
	}
}

func TestTaskRecoveryNewIDsCannotRepeatUncertainEffects(t *testing.T) {
	a, task, _ := recoveryFixture(t)
	recoveryCheck(t, a, task)
	review := a.taskRecoveryReviewLocked(task)
	recoverExecutionCheckpoint(task)
	applyRecoveryContinuation(task, task.ID, review, &taskRecoveryDecision{Snapshot: review.Snapshot, Decision: "continue_without_replay", Note: "new work only"})
	planInstall(t, a, "recovery-no-repeat", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'recovery_probe',handler:()=>{fs.writeFileSync(path.join(__dirname,'bad'),'1');return 'bad';}})}};`)
	direct := readCall("recovery_probe", `{"marker":"old"}`)
	direct.ID = "changed-direct-id"
	if result := a.executeToolCall(context.Background(), direct, task, map[string]Change{}); !strings.Contains(result, "previous unresolved") {
		t.Fatal("direct alias bypassed operation guard", result)
	}
	graph := planCall(t, "changed-plan-id", []pluginPlanNodeSpec{{ID: "again", Tool: "recovery_probe", Arguments: map[string]any{"marker": "old"}}})
	result := planDecode(t, a.executePluginPlan(context.Background(), graph, task, map[string]Change{}))
	if result.Nodes[0].State != "skipped" || planExists(filepath.Join(a.pluginsPath, "recovery-no-repeat", "bad")) {
		t.Fatal("new graph replayed unresolved effect", result)
	}
	if err := a.recoveryOperationError(task, "read_file", map[string]any{"path": "inspect.txt"}, nil); err != nil {
		t.Fatal("verification reads blocked", err)
	}
}

func TestTaskRecoverySaveFailureAndConcurrentDecision(t *testing.T) {
	for _, saveFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "save failure", false: "concurrent"}[saveFailure], func(t *testing.T) {
			a, task, _ := recoveryFixture(t)
			recoveryCheck(t, a, task)
			review := a.taskRecoveryReviewLocked(task)
			provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) { return "continued", nil }})
			defer provider.Close()
			a.settings = Settings{BaseURL: provider.URL, Model: "test"}
			if saveFailure {
				s := a.sessions["effect-session"]
				file := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(file, 0700); err != nil {
					t.Fatal(err)
				}
				requireStatus(t, request(a, "POST", recoveryURL+"/resume", recoveryRequest(review)), 500)
				if task.Status != "interrupted" || !task.CanResume || len(s.Runs) != 1 || len(a.cancels) != 0 || task.RecoveryContinuation != nil {
					t.Fatal("failed save started/changed task")
				}
				return
			}
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					codes <- request(a, "POST", recoveryURL+"/resume", recoveryRequest(review)).Code
				}()
			}
			wg.Wait()
			close(codes)
			counts := map[int]int{}
			for c := range codes {
				counts[c]++
			}
			if counts[202] != 1 || counts[409] != 1 {
				t.Fatal(counts)
			}
			a.mu.Lock()
			id := a.sessions["effect-session"].Runs[1].ID
			a.mu.Unlock()
			_ = recoveryWait(t, a, id)
		})
	}
}
