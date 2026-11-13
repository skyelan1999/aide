package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func flowCreateFixture(t *testing.T, a *App, tools map[string][]string) *Session {
	t.Helper()
	s := planSession(a, "flow-session", "Business fixture")
	w := request(a, "GET", "/api/sessions/flow-session/plan/flows", nil)
	requireStatus(t, w, 200)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/plan/flows", map[string]any{"workspaceId": v["workspaceId"], "snapshot": v["snapshot"], "recipe": "software", "brief": "Repair a fixture and provide verified delivery", "stageTools": tools}), 201)
	return s
}
func acceptFlowRun(a *App, s *Session, index int, id string) *Task {
	item := &s.Plan[index]
	r := &Task{ID: id, PlanTaskID: item.ID, Status: "completed", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, Steps: []Step{{Name: "chat", Status: "completed", Content: "Accepted calculation result: 42"}}}
	s.Runs = append(s.Runs, r)
	item.RunID = r.ID
	for i := range item.Contract.Criteria {
		item.Contract.Criteria[i].Review = &GoalReview{State: "passed", Source: "operator_observation", RunID: r.ID, Fingerprint: goalRunFingerprint(r), Note: "Inspected fixture"}
	}
	a.syncPlanOutcomeLocked(s, r)
	return r
}
func TestBusinessFlowStageHandoffAndTransitiveInvalidation(t *testing.T) {
	a := testApp(t)
	s := flowCreateFixture(t, a, nil)
	if len(s.Plan) != 5 || len(s.Plan[0].DependsOn) != 0 || s.Plan[1].DependsOn[0] != s.Plan[0].ID {
		t.Fatal("recipe dependencies")
	}
	a.mu.Lock()
	if _, err := a.collectFlowInputsLocked(s, &s.Plan[1]); err == nil {
		t.Fatal("unaccepted predecessor allowed")
	}
	first := acceptFlowRun(a, s, 0, "first")
	inputs, err := a.collectFlowInputsLocked(s, &s.Plan[1])
	if err != nil || len(inputs) != 1 || inputs[0].Output != "Accepted calculation result: 42" {
		t.Fatal("handoff", inputs, err)
	}
	second := acceptFlowRun(a, s, 1, "second")
	second.FlowInputs = inputs
	for i := range s.Plan[1].Contract.Criteria {
		s.Plan[1].Contract.Criteria[i].Review.Fingerprint = goalRunFingerprint(second)
	}
	a.syncPlanOutcomeLocked(s, second)
	thirdInputs, err := a.collectFlowInputsLocked(s, &s.Plan[2])
	if err != nil {
		t.Fatal(err)
	}
	third := acceptFlowRun(a, s, 2, "third")
	third.FlowInputs = thirdInputs
	for i := range s.Plan[2].Contract.Criteria {
		s.Plan[2].Contract.Criteria[i].Review.Fingerprint = goalRunFingerprint(third)
	}
	a.syncPlanOutcomeLocked(s, third)
	if !goalAccepted(&s.Plan[1], second) || !goalAccepted(&s.Plan[2], third) {
		t.Fatal("accepted flow")
	}
	s.Plan[0].Contract.Criteria[0].Review.Note = "Changed source observation"
	a.syncPlanOutcomeLocked(s, first)
	if !second.GoalInputsStale || !third.GoalInputsStale || goalAccepted(&s.Plan[1], second) || s.Plan[1].Status != "blocked" {
		t.Fatal("downstream acceptance survived changed predecessor")
	}
	if !goalAccepted(&s.Plan[0], first) {
		t.Fatal("predecessor result lost")
	}
	a.mu.Unlock()
	if !strings.Contains(a.workflowInputs(second), `"stale":true`) {
		t.Fatal("model not told input is stale")
	}
}
func TestBusinessFlowRealPluginEvidenceAndVersionAdmission(t *testing.T) {
	a := testApp(t)
	planInstall(t, a, "flow-fixture", `module.exports={apply(ctx){ctx.tool({name:'flow_value',handler:()=>({text:'calculation evidence 42'})});}};`)
	s := flowCreateFixture(t, a, map[string][]string{"understand": {"flow_value"}})
	a.mu.Lock()
	r := acceptFlowRun(a, s, 0, "plugin-flow-run")
	r.FlowTools = s.Plan[0].Flow.Tools
	if goalExecutionReady(r) {
		t.Fatal("selected tool can be skipped")
	}
	r.Status = "running"
	r.WorkspaceMode = a.workspaceMode()
	a.mu.Unlock()
	call := planCall(t, "flow-real-plugin", []pluginPlanNodeSpec{{ID: "read", Tool: "flow_value", Arguments: map[string]any{}}})
	out := a.executePluginPlan(context.Background(), call, r, map[string]Change{})
	if !strings.Contains(out, "calculation evidence 42") {
		t.Fatal("real plugin did not execute", out)
	}
	a.mu.Lock()
	r.Status = "completed"
	r.Steps = []Step{{Name: "chat", Status: "completed", Content: "Plugin-backed fixture result"}}
	for i := range s.Plan[0].Contract.Criteria {
		s.Plan[0].Contract.Criteria[i].Review.Fingerprint = goalRunFingerprint(r)
	}
	a.syncPlanOutcomeLocked(s, r)
	inputs, err := a.collectFlowInputsLocked(s, &s.Plan[1])
	if err != nil || len(inputs[0].Receipts) != 1 || !strings.Contains(inputs[0].Receipts[0].Result, "calculation evidence 42") {
		t.Fatal("actual plugin receipt not handed off", inputs, err)
	}
	// Changed registered package identity cannot silently satisfy a selected stage.
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "flow-fixture" {
			a.pluginRegistry.Plugins[i].Version = "changed"
		}
	}
	_, err = a.collectFlowInputsLocked(s, &s.Plan[0])
	a.mu.Unlock()
	if err == nil {
		t.Fatal("changed plugin admitted")
	}
	if err := a.flowToolError(r, "flow_value"); err == nil {
		t.Fatal("changed plugin tool dispatch admitted")
	}
	w := request(a, "GET", "/api/sessions/flow-session/plan/flows", nil)
	var view map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	payload := map[string]any{"action": "configure", "workspaceId": view["workspaceId"], "snapshot": view["snapshot"], "itemId": s.Plan[0].ID, "tools": []string{"flow_value"}}
	// A failed durable write cannot leave new tool bindings or stale flags.
	file := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
	if err := os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/plan/flows", payload), 500)
	if r.GoalInputsStale {
		t.Fatal("failed save leaked invalidation")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".backup", file); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/plan/flows", payload), 200)
	if !r.GoalInputsStale || s.Plan[0].Status != "blocked" {
		t.Fatal("reconfigured stage retained old sign-off")
	}
}

func TestBusinessFlowReadToolsBoundsAndProjectIsolation(t *testing.T) {
	a := testApp(t)
	r := &Task{ID: "input-read", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision}
	input := FlowInput{PlanID: "plan", RunID: "source", Output: strings.Repeat("汉", 4000)}
	for n := 0; n < 12; n++ {
		input.Files = append(input.Files, FlowFile{Path: "source.txt"})
		input.Receipts = append(input.Receipts, FlowReceipt{Tool: "fixture", Result: strings.Repeat("测", 1000)})
	}
	r.FlowInputs = []FlowInput{input}
	var page map[string]any
	if err := json.Unmarshal([]byte(a.workflowInputs(r)), &page); err != nil {
		t.Fatal(err)
	}
	if page["nextOutputOffset"] != float64(1000) || page["nextFileOffset"] != float64(5) || page["nextReceiptOffset"] != float64(2) {
		t.Fatal("unbounded/missing continuation", page)
	}
	if len(a.workflowInputs(r)) > 64<<10 {
		t.Fatal("unbounded input result")
	}
	if !strings.Contains(a.workflowInputs(r, map[string]any{"outputOffset": float64(1000), "fileOffset": float64(5), "receiptOffset": float64(2)}), `"nextFileOffset":10`) {
		t.Fatal("input page continuation")
	}
	r.WorkspaceID = "foreign"
	if strings.Contains(a.workflowInputs(r), "source.txt") {
		t.Fatal("foreign inputs leaked")
	}
}
func TestBusinessFlowImportResetsIdentityReviewsAndRejectsCycles(t *testing.T) {
	a := testApp(t)
	s := flowCreateFixture(t, a, nil)
	a.mu.Lock()
	acceptFlowRun(a, s, 0, "source")
	a.mu.Unlock()
	w := request(a, "GET", "/api/sessions/flow-session/plan/export", nil)
	requireStatus(t, w, 200)
	var doc planDocument
	_ = json.Unmarshal(w.Body.Bytes(), &doc)
	dst := planSession(a, "flow-target", "Imported business fixture")
	requireStatus(t, request(a, "POST", "/api/sessions/flow-target/plan/import", doc), 200)
	if len(dst.Plan) != 5 || dst.Plan[0].Flow.ID == s.Plan[0].Flow.ID || dst.Plan[0].Contract.Criteria[0].Review != nil || dst.Plan[1].DependsOn[0] != dst.Plan[0].ID {
		t.Fatal("import reused stale execution/identity")
	}
	doc.Tasks[0].DependsOn = []string{doc.Tasks[4].ID}
	requireStatus(t, request(a, "POST", "/api/sessions/flow-target/plan/import", doc), 400)
	before := goalRunFingerprint(&Task{ID: "legacy"})
	legacy := recoveryHash([]any{"legacy", "", "", uint64(0), []Change(nil), (*TaskDelivery)(nil), []ToolUse(nil), uint64(0)})
	if before != legacy {
		t.Fatal("additive upgrade invalidated non-flow sign-off")
	}
}
func TestBusinessFlowAdmissionCASAndFailedStagePreservesCompleted(t *testing.T) {
	a := testApp(t)
	s := flowCreateFixture(t, a, nil)
	a.mu.Lock()
	first := acceptFlowRun(a, s, 0, "done")
	inputs, err := a.collectFlowInputsLocked(s, &s.Plan[1])
	if err != nil {
		t.Fatal(err)
	}
	failed := &Task{ID: "failed", PlanTaskID: s.Plan[1].ID, Status: "failed", WorkspaceID: a.wsID(), FlowInputs: inputs}
	s.Runs = append(s.Runs, failed)
	a.syncPlanOutcomeLocked(s, failed)
	if !goalAccepted(&s.Plan[0], first) || s.Plan[0].Status != "completed" || s.Plan[1].Status != "failed" {
		t.Fatal("failed stage discarded completed work")
	}
	again, err := a.collectFlowInputsLocked(s, &s.Plan[1])
	a.mu.Unlock()
	if err != nil || again[0].RunID != "done" {
		t.Fatal("cannot continue gap", again, err)
	}
	w := request(a, "GET", "/api/sessions/flow-session/plan/flows", nil)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/plan/flows", map[string]any{"workspaceId": "other", "snapshot": v["snapshot"], "recipe": "engineering", "brief": "fixture"}), 409)
}

func TestBusinessFlowPausedResumeRetainsStageAndInputs(t *testing.T) {
	a := testApp(t)
	provider := newMockProvider(t, func(map[string]any) string { return "Resumed fixture stage result" })
	defer provider.Close()
	s := flowCreateFixture(t, a, nil)
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	acceptFlowRun(a, s, 0, "accepted-source")
	inputs, err := a.collectFlowInputsLocked(s, &s.Plan[1])
	if err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	r := &Task{ID: "flow-paused", PlanTaskID: s.Plan[1].ID, Mode: "chat", Prompt: "Continue fixture", Status: "paused", CanResume: true, CheckpointStep: "chat", CheckpointMessages: []Message{{Role: "user", Content: "Continue fixture"}}, WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: a.workspaceMode(), FlowInputs: inputs}
	s.Plan[1].RunID = r.ID
	s.Plan[1].Status = "blocked"
	s.Runs = append(s.Runs, r)
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/runs/flow-paused/retry", nil), 409)
	w := request(a, "POST", "/api/sessions/flow-session/runs/flow-paused/resume", nil)
	requireStatus(t, w, 202)
	var resumed Task
	if err := json.Unmarshal(w.Body.Bytes(), &resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.PlanTaskID != s.Plan[1].ID || resumed.FlowInputs[0].RunID != "accepted-source" {
		t.Fatal("resume lost flow contract")
	}
	planAwait(t, "resumed stage", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return s.Runs[len(s.Runs)-1].Status != "running" })
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.Plan[1].RunID != resumed.ID || s.Plan[1].Status != "awaiting_acceptance" || s.Plan[0].Status != "completed" {
		t.Fatal("resume did not preserve goal identity or completed stage", s.Plan)
	}
}

func TestBusinessFlowStartSaveFailureLeavesNoRunningStage(t *testing.T) {
	a := testApp(t)
	s := flowCreateFixture(t, a, nil)
	provider := newMockProvider(t, func(map[string]any) string { return "not reached" })
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	file := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
	if err := os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(file); _ = os.Rename(file+".backup", file) }()
	oldMessages, oldRuns, oldUpdated := len(s.Messages), len(s.Runs), s.Updated
	requireStatus(t, request(a, "POST", "/api/sessions/flow-session/runs", map[string]any{"mode": "chat", "prompt": "Fixture stage", "planTaskId": s.Plan[0].ID}), 500)
	if len(s.Runs) != oldRuns || len(s.Messages) != oldMessages || s.Plan[0].RunID != "" || s.Plan[0].Status != "pending" || s.Updated != oldUpdated {
		t.Fatalf("failed save state: runs=%d/%d messages=%d/%d planRun=%q status=%q updated=%q/%q", len(s.Runs), oldRuns, len(s.Messages), oldMessages, s.Plan[0].RunID, s.Plan[0].Status, s.Updated, oldUpdated)
	}
}

func TestBusinessFlowGoalSaveFailureDoesNotRetainReview(t *testing.T) {
	a := testApp(t)
	s, item, _ := acceptanceGoal(t, a)
	view := goalView(t, a, item)
	file := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
	if err := os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(file); _ = os.Rename(file+".backup", file) }()
	requireStatus(t, request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": view["snapshot"], "criterionId": item.Contract.Criteria[0].ID, "state": "passed", "note": "Actual fixture observation"}), 500)
	current := a.findPlanItemLocked(s, item.ID)
	if current.Contract.Criteria[0].Review != nil {
		t.Fatal("failed save retained a new user sign-off in memory")
	}
}
