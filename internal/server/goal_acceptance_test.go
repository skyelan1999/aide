package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func acceptanceGoal(t *testing.T, a *App) (*Session, *PlanItem, *Task) {
	t.Helper()
	a.mu.Lock()
	s := planSession(a, "goal-session", "Acceptance fixture")
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions/goal-session/plan", map[string]any{"title": "Deliver a tested change", "contract": map[string]any{"deliverables": []string{"patch", "test report"}, "criteria": []map[string]any{{"text": "Regression passes"}, {"text": "Output reviewed", "review": map[string]any{"state": "passed"}}}}})
	requireStatus(t, w, 201)
	var p PlanItem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	item := a.findPlanItemLocked(s, p.ID)
	run := &Task{ID: "goal-run", PlanTaskID: item.ID, Status: "completed", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: a.workspaceMode(), ToolUses: []ToolUse{{Tool: "run_shell", Args: `{"command":"test"}`, Result: "exit 0"}}}
	s.Runs = append(s.Runs, run)
	item.RunID = run.ID
	a.syncPlanOutcomeLocked(s, run)
	a.mu.Unlock()
	return s, item, run
}
func goalView(t *testing.T, a *App, item *PlanItem) map[string]any {
	t.Helper()
	w := request(a, "GET", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", nil)
	requireStatus(t, w, 200)
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func goalReview(t *testing.T, a *App, item *PlanItem, index int, state string) {
	t.Helper()
	view := goalView(t, a, item)
	w := request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": view["snapshot"], "criterionId": item.Contract.Criteria[index].ID, "state": state, "note": "Actual fixture observation", "toolIndex": 1})
	requireStatus(t, w, 200)
}
func TestGoalAcceptanceRequiresAllCriteriaAndCurrentEvidence(t *testing.T) {
	a := testApp(t)
	s, item, run := acceptanceGoal(t, a)
	if item.Status != "awaiting_acceptance" || item.Contract.Criteria[1].Review != nil {
		t.Fatal("execution/model-supplied review completed the goal")
	}
	goalReview(t, a, item, 0, "passed")
	if item.Status == "completed" {
		t.Fatal("partial acceptance completed goal")
	}
	stale := goalView(t, a, item)["snapshot"]
	goalReview(t, a, item, 1, "failed")
	if item.Status != "blocked" {
		t.Fatal("failure not retained")
	}
	w := request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": stale, "criterionId": item.Contract.Criteria[1].ID, "state": "passed", "note": "stale"})
	requireStatus(t, w, 409)
	goalReview(t, a, item, 1, "passed")
	if item.Status != "completed" || !goalAccepted(item, run) || !goalDependencyReady(s, item) {
		t.Fatal("accepted goal not completed")
	}
	run.ToolUses[0].Result = "exit 1"
	if goalAccepted(item, run) || goalDependencyReady(s, item) {
		t.Fatal("changed receipt reused")
	}
	a.syncPlanOutcomeLocked(s, run)
	if item.Status != "awaiting_acceptance" {
		t.Fatal("stale evidence not blocking")
	}
	rows := goalView(t, a, item)["criteria"].([]any)
	if rows[0].(map[string]any)["state"] != "needs_review" {
		t.Fatal(rows)
	}
	run.ID = "new-run"
	item.RunID = run.ID
	if goalAccepted(item, run) {
		t.Fatal("new run inherited old acceptance")
	}
	item.Status = "closed"
	if goalDependencyReady(s, item) {
		t.Fatal("closed unaccepted goal unlocked dependency")
	}
}
func TestGoalAcceptanceRejectsUnappliedAndUnfinishedWork(t *testing.T) {
	a := testApp(t)
	_, item, run := acceptanceGoal(t, a)
	for _, status := range []string{"running", "awaiting_approval", "failed", "interrupted"} {
		run.Status = status
		view := goalView(t, a, item)
		w := request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": view["snapshot"], "criterionId": item.Contract.Criteria[0].ID, "state": "passed", "note": "must reject"})
		requireStatus(t, w, 409)
	}
	run.Status = "completed"
	run.Files = []Change{{Path: "output.txt", Applied: false}}
	view := goalView(t, a, item)
	w := request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": view["snapshot"], "criterionId": item.Contract.Criteria[0].ID, "state": "passed", "note": "not applied"})
	requireStatus(t, w, 409)
	run.Files = nil
	run.Delivery = &TaskDelivery{State: "missing"}
	if goalExecutionReady(run) {
		t.Fatal("missing delivery accepted")
	}
}
func TestGoalContractImportPersistenceContextAndRevision(t *testing.T) {
	a := testApp(t)
	s, item, run := acceptanceGoal(t, a)
	goalReview(t, a, item, 0, "passed")
	goalReview(t, a, item, 1, "passed")
	w := request(a, "GET", "/api/sessions/goal-session/plan/export", nil)
	requireStatus(t, w, 200)
	var exported planDocument
	json.Unmarshal(w.Body.Bytes(), &exported)
	w = request(a, "POST", "/api/sessions/goal-session/plan/import", exported)
	requireStatus(t, w, 200)
	imported := s.Plan[len(s.Plan)-1]
	if imported.Contract == nil || imported.Contract.Criteria[0].Review != nil || imported.Contract.Criteria[0].ID == item.Contract.Criteria[0].ID {
		t.Fatal("import copied acceptance evidence")
	}
	// Import can reallocate the plan slice: reacquire its current owner.
	item = a.findPlanItemLocked(s, run.PlanTaskID)
	view := goalView(t, a, item)
	w = request(a, "PUT", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": view["snapshot"], "contract": map[string]any{"criteria": []map[string]any{{"text": "New requirement"}}}})
	requireStatus(t, w, 200)
	if item.Status != "awaiting_acceptance" || item.Contract.Criteria[0].Review != nil {
		t.Fatal("revision kept old acceptance")
	}
	if ctx := a.planContextLocked(s, item.ID, 1); !strings.Contains(ctx, "New requirement") || !strings.Contains(ctx, "验收条件") {
		t.Fatal(ctx)
	}
	a.Close()
	b, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	saved := b.sessions[s.ID]
	if saved == nil || a.findPlanItemLocked(saved, item.ID).Contract.Criteria[0].Text != "New requirement" {
		t.Fatal("contract lost across reload")
	}
}
func TestGoalContractBoundsAndLegacyPlans(t *testing.T) {
	if _, err := freshGoalContract(&GoalContract{Criteria: []GoalCriterion{{Text: strings.Repeat("x", 501)}}}); err == nil {
		t.Fatal("oversized criterion")
	}
	if _, err := freshGoalContract(&GoalContract{}); err == nil {
		t.Fatal("empty contract")
	}
	a := testApp(t)
	s := planSession(a, "legacy", "legacy")
	run := &Task{ID: "legacy-run", PlanTaskID: "legacy-goal", Status: "completed"}
	s.Plan = []PlanItem{{ID: "legacy-goal", Source: "user", Status: "in_progress"}}
	a.syncPlanOutcomeLocked(s, run)
	if s.Plan[0].Status != "completed" {
		t.Fatal("legacy execution behavior changed")
	}
	if !goalDependencyReady(s, &PlanItem{Status: "closed"}) {
		t.Fatal("legacy closed dependency changed")
	}
}

func TestGoalFocusContextAndReadOnlyFeedback(t *testing.T) {
	a := testApp(t)
	s, item, run := acceptanceGoal(t, a)
	goalReview(t, a, item, 0, "failed")
	if ctx := a.planContextLocked(s, item.ID, 0); !strings.Contains(ctx, "Actual fixture observation") {
		t.Fatal("operator gap missing from next-run context")
	}
	if out := a.taskAcceptance(run, ""); !strings.Contains(out, "Actual fixture observation") || !strings.Contains(out, `"state":"failed"`) {
		t.Fatal(out)
	}
	for i := 0; i < planContextLimit+2; i++ {
		s.Plan = append([]PlanItem{{ID: "other-" + newID(), Title: "Other task", Status: "pending"}}, s.Plan...)
	}
	item = a.findPlanItemLocked(s, run.PlanTaskID)
	item.Status = "completed"
	if ctx := a.planContextLocked(s, item.ID, 0); !strings.Contains(ctx, "Regression passes") || !strings.Contains(ctx, "本次执行") {
		t.Fatal("focused goal was lost behind context cap or completed filter")
	}
	run.WorkspaceRev++
	if out := a.taskAcceptance(run, ""); !strings.Contains(out, "工作区已变化") {
		t.Fatal(out)
	}
}

func TestGoalContractKeepsReviewHistoryAndRejectsStaleRead(t *testing.T) {
	a := testApp(t)
	_, item, run := acceptanceGoal(t, a)
	goalReview(t, a, item, 0, "failed")
	goalReview(t, a, item, 0, "passed")
	if len(item.Contract.Criteria[0].History) != 1 || item.Contract.Criteria[0].History[0].State != "failed" {
		t.Fatal("prior review lost")
	}
	v := goalView(t, a, item)
	run.ToolUses[0].Args = `{"command":"changed"}`
	w := request(a, "POST", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": v["snapshot"], "criterionId": item.Contract.Criteria[1].ID, "state": "passed", "note": "stale evidence"})
	requireStatus(t, w, 409)
	v = goalView(t, a, item)
	w = request(a, "PUT", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": v["snapshot"], "contract": map[string]any{"criteria": []map[string]any{{"text": "Revised requirement"}}}})
	requireStatus(t, w, 200)
	if len(item.ContractHistory) != 1 || item.ContractHistory[0].Contract.Criteria[0].Review == nil {
		t.Fatal("prior contract and review lost")
	}
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", nil), 423)
}

func TestGoalAcceptanceSurvivesRestartAndCannotMoveProjects(t *testing.T) {
	a := testApp(t)
	s, item, run := acceptanceGoal(t, a)
	goalReview(t, a, item, 0, "passed")
	goalReview(t, a, item, 1, "passed")
	a.Close()
	b, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	saved := b.sessions[s.ID]
	item = b.findPlanItemLocked(saved, item.ID)
	run = planRun(saved, item)
	if !goalAccepted(item, run) {
		t.Fatal("accepted goal lost proof after restart")
	}
	item.Status = "closed"
	if !goalDependencyReady(saved, item) {
		t.Fatal("closing an accepted goal broke dependencies")
	}
	item.Status = "completed"
	item.WorkspaceID = "different-project"
	v := goalView(t, b, item)
	w := request(b, "PUT", "/api/sessions/goal-session/plan/"+item.ID+"/acceptance", map[string]any{"snapshot": v["snapshot"], "contract": map[string]any{"criteria": []map[string]any{{"text": "Move target"}}}})
	requireStatus(t, w, 409)
	requireStatus(t, request(b, "POST", "/api/sessions/goal-session/runs", map[string]any{"planTaskId": item.ID, "prompt": "execute goal", "mode": "chat"}), 409)
}

func TestGoalApprovalAppliesFileThenAwaitsAcceptance(t *testing.T) {
	a := testApp(t)
	_, item, run := acceptanceGoal(t, a)
	a.settings.SandboxMode = "workspace-write"
	run.Status = "awaiting_approval"
	run.Files = []Change{{Path: "goal-output.txt", Content: "actual saved output", BaseHash: ""}}
	w := request(a, "POST", "/api/sessions/goal-session/runs/goal-run/apply", nil)
	requireStatus(t, w, 200)
	if run.Status != "completed" || item.Status != "awaiting_acceptance" || !run.Files[0].Applied {
		t.Fatal("application did not transition goal to acceptance")
	}
	b, err := os.ReadFile(filepath.Join(a.workPath, "goal-output.txt"))
	if err != nil || string(b) != "actual saved output" {
		t.Fatal("file was not saved", err)
	}
}

func TestGoalCreateAndImportRejectStaleDestination(t *testing.T) {
	a := testApp(t)
	planSession(a, "goal-session", "Destination")
	w := request(a, "POST", "/api/sessions/goal-session/plan", map[string]any{"title": "other project", "workspaceId": "stale"})
	requireStatus(t, w, 409)
	w = request(a, "POST", "/api/sessions/goal-session/plan/import", map[string]any{"schema": planSchema, "destinationWorkspaceId": "stale", "tasks": []map[string]any{{"title": "imported"}}})
	requireStatus(t, w, 409)
}

func TestGoalAssistantLedgerSeparatesExecutionFromAcceptance(t *testing.T) {
	a := testApp(t)
	s, item, run := acceptanceGoal(t, a)
	event := a.sessionWorkEventLocked(s)
	if event.Status != "awaiting_acceptance" || !strings.Contains(event.Text, "目标仍待验收") || strings.Contains(assistantResultEvidenceText(*event, run), "目标已逐项验收") {
		t.Fatal(event)
	}
	previousID := event.ID
	goalReview(t, a, item, 0, "passed")
	goalReview(t, a, item, 1, "passed")
	event = a.sessionWorkEventLocked(s)
	if event.Status != "completed" || event.ID == previousID || !strings.Contains(event.Text, "目标已逐项验收") {
		t.Fatal("acceptance did not produce a distinct result", event)
	}
	item.Contract.Criteria[0].Review = nil
	item.Status = "closed"
	event = a.sessionWorkEventLocked(s)
	if event.Status != "closed" || !strings.Contains(event.NextAction, "只有用户重新交办") {
		t.Fatal("assistant would resume closed goal", event)
	}
}
