package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func planSession(a *App, id, title string) *Session {
	s := &Session{ID: id, Title: title, Created: time.Now().UTC().Format(time.RFC3339Nano)}
	a.sessions[id] = s
	return s
}

func TestPlanTaskLifecycleDependenciesAndExport(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	s := planSession(a, "plan-sess", "编排测试")
	a.mu.Unlock()

	w := request(a, "POST", "/api/sessions/plan-sess/plan", map[string]any{"title": "准备数据", "detail": "整理输入"})
	requireStatus(t, w, 201)
	var first PlanItem
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || first.ID == "" || first.Status != "pending" || first.Source != "user" {
		t.Fatalf("add plan task: %v %+v", err, first)
	}

	requireStatus(t, request(a, "POST", "/api/sessions/plan-sess/plan", map[string]any{"title": "训练", "dependsOn": []string{"missing"}}), 400)

	w = request(a, "POST", "/api/sessions/plan-sess/plan", map[string]any{"title": "训练", "dependsOn": []string{first.ID}})
	requireStatus(t, w, 201)
	var second PlanItem
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil || len(second.DependsOn) != 1 || second.DependsOn[0] != first.ID {
		t.Fatalf("dependency not stored: %v %+v", err, second)
	}

	requireStatus(t, request(a, "PATCH", "/api/sessions/plan-sess/plan/"+first.ID, map[string]any{"status": "closed"}), 200)
	a.mu.Lock()
	closed := a.findPlanItemLocked(s, first.ID)
	a.mu.Unlock()
	if closed == nil || closed.Status != "closed" || closed.ClosedAt == "" {
		t.Fatalf("close did not persist: %+v", closed)
	}
	requireStatus(t, request(a, "PATCH", "/api/sessions/plan-sess/plan/"+first.ID, map[string]any{"status": "pending"}), 200)

	w = request(a, "GET", "/api/sessions/plan-sess/plan/export", nil)
	requireStatus(t, w, 200)
	var doc planDocument
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil || doc.Schema != planSchema || len(doc.Tasks) != 2 {
		t.Fatalf("export document: %v %+v", err, doc)
	}
	if disposition := w.Header().Get("Content-Disposition"); !strings.Contains(disposition, "aide-plan-plan-sess.json") {
		t.Fatalf("export filename: %q", disposition)
	}

	importDoc := map[string]any{"schema": planSchema, "tasks": []map[string]any{
		{"id": "a", "title": "导入 A", "detail": "说明 A", "status": "completed"},
		{"id": "b", "title": "导入 B", "dependsOn": []string{"a"}, "agent": "aide"},
	}}
	w = request(a, "POST", "/api/sessions/plan-sess/plan/import", importDoc)
	requireStatus(t, w, 200)
	var imported struct {
		Imported int        `json:"imported"`
		Items    []PlanItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &imported); err != nil || imported.Imported != 2 {
		t.Fatalf("import result: %v %+v", err, imported)
	}
	if imported.Items[0].Status != "pending" || imported.Items[0].RunID != "" || imported.Items[1].DependsOn[0] != imported.Items[0].ID {
		t.Fatalf("imported items must reset state and remap dependencies: %+v", imported.Items)
	}
	if imported.Items[1].Agent != "aide" {
		t.Fatalf("agent hint lost: %+v", imported.Items[1])
	}

	requireStatus(t, request(a, "POST", "/api/sessions/plan-sess/plan/dismiss", map[string]any{"runId": "run-x", "step": 1}), 200)
	a.mu.Lock()
	keys, total := append([]string(nil), s.PlanClosed...), len(s.Plan)
	a.mu.Unlock()
	if len(keys) != 1 || keys[0] != "run-x:1" {
		t.Fatalf("dismiss key missing: %v", keys)
	}
	if total != 4 {
		t.Fatalf("imported items missing: %d", total)
	}

	requireStatus(t, request(a, "POST", "/api/sessions/plan-sess/plan", map[string]any{"title": strings.Repeat("长", planTitleRunes+1)}), 400)
}

func TestPlanMirrorsAgentStepsAndClosesOutcome(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	s := planSession(a, "plan-mirror", "镜像")
	parent := PlanItem{ID: "plan-parent", Title: "父任务", Source: "user", Status: "in_progress", Created: time.Now().UTC().Format(time.RFC3339Nano)}
	s.Plan = append(s.Plan, parent)
	task := &Task{ID: "run-1", PlanTaskID: "plan-parent", Status: "completed", AgentPlan: &AgentPlan{Explanation: "计划", Items: []AgentPlanItem{
		{Step: "读代码", Status: "completed"},
		{Step: "改代码", Status: "in_progress"},
	}}}
	s.Runs = append(s.Runs, task)
	a.mu.Unlock()

	a.mu.Lock()
	a.syncAgentPlanLocked(s, task)
	a.syncPlanOutcomeLocked(s, task)
	mirrored := append([]PlanItem(nil), s.Plan...)
	a.mu.Unlock()

	if len(mirrored) != 3 {
		t.Fatalf("mirror items: %+v", mirrored)
	}
	for _, item := range mirrored {
		if item.Source == "aide" && (item.RunID != "run-1" || item.ParentID != "plan-parent" || item.Status != "completed") {
			t.Fatalf("aide step not closed out: %+v", item)
		}
	}
	if got := a.findPlanItemLocked(s, "plan-parent"); got == nil || got.Status != "completed" || got.RunID != "run-1" {
		t.Fatalf("parent task did not follow run: %+v", got)
	}

	// 关闭的单步在镜像时保持关闭，且已完成的进度不被重复计划回退。
	a.mu.Lock()
	for i := range s.Plan {
		if s.Plan[i].Source == "aide" && s.Plan[i].Step == 1 {
			s.Plan[i].Status, s.Plan[i].ClosedAt = "closed", time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	s.PlanClosed = append(s.PlanClosed, planStepKey("run-1", 1))
	task.Status = "failed"
	a.syncAgentPlanLocked(s, task)
	a.syncPlanOutcomeLocked(s, task)
	snapshot := append([]PlanItem(nil), s.Plan...)
	a.mu.Unlock()
	for _, item := range snapshot {
		if item.Source == "aide" && item.Step == 1 && item.Status != "closed" {
			t.Fatalf("dismissed step reopened: %+v", item)
		}
	}
	if got := a.findPlanItemLocked(s, "plan-parent"); got == nil || got.Status != "failed" {
		t.Fatalf("failed run did not mark parent: %+v", got)
	}
}

func TestPlanContextCarriesOpenTasksAndApprovals(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	s := planSession(a, "plan-ctx", "上下文")
	s.Plan = []PlanItem{
		{ID: "plan-open", Title: "尚未完成", Source: "user", Status: "in_progress"},
		{ID: "plan-fail", Title: "失败的一步", Source: "aide", Status: "failed"},
		{ID: "plan-done", Title: "已完成", Source: "user", Status: "completed"},
	}
	s.Runs = []*Task{{ID: "run-ctx", Status: "awaiting_approval", ApprovalReviews: []ApprovalReview{{Kind: "files", Status: "recommended", Reason: "低风险新文件"}}}}
	ctx := a.planContextLocked(s, "plan-open", 6)
	a.mu.Unlock()
	if ctx == "" || !strings.Contains(ctx, "尚未完成") || !strings.Contains(ctx, "（本次执行）") || !strings.Contains(ctx, "失败的一步") || strings.Contains(ctx, "已完成") {
		t.Fatalf("plan context wrong: %q", ctx)
	}
	if !strings.Contains(ctx, "最近审批记录") || !strings.Contains(ctx, "低风险新文件") {
		t.Fatalf("approval history missing: %q", ctx)
	}
}

func TestRecommendationAutoAppliesAfterWindow(t *testing.T) {
	a := testApp(t)
	a.approvalGrace = 10 * time.Millisecond
	task := &Task{
		ID: "run-auto", Status: "awaiting_approval", ApprovalState: "recommended", WorkspaceID: a.wsID(),
		approvalGeneration: 7,
		ApprovalReviews:    []ApprovalReview{{Kind: "files", Status: "recommended", Reason: "低风险新文件"}},
		Files:              []Change{{Path: "auto-apply.md", Content: "AUTO"}},
	}
	s := &Session{ID: "plan-auto", Runs: []*Task{task}}
	a.sessions[s.ID] = s

	a.applyRecommendedFiles(s.ID, task.ID, 7)
	if _, err := os.Stat(filepath.Join(a.workPath, "auto-apply.md")); err != nil {
		t.Fatalf("recommendation did not apply: %v", err)
	}
	if task.Status != "completed" || task.ApprovalState != "approved" {
		t.Fatalf("unexpected task after auto apply: status=%s state=%s", task.Status, task.ApprovalState)
	}
	if len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Status != "approved" || !strings.Contains(task.ApprovalReviews[0].Reason, "已按推荐方案自动执行") {
		t.Fatalf("auto apply not recorded: %+v", task.ApprovalReviews)
	}
}

func TestStaleRecommendationIsNotApplied(t *testing.T) {
	a := testApp(t)
	task := &Task{
		ID: "run-stale", Status: "awaiting_approval", ApprovalState: "recommended", WorkspaceID: a.wsID(),
		approvalGeneration: 9,
		Files:              []Change{{Path: "stale.md", Content: "STALE"}},
	}
	s := &Session{ID: "plan-stale", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	a.applyRecommendedFiles(s.ID, task.ID, 8)
	if _, err := os.Stat(filepath.Join(a.workPath, "stale.md")); err == nil {
		t.Fatal("stale recommendation applied")
	}
	if task.ApprovalState != "recommended" {
		t.Fatalf("stale timer changed state: %s", task.ApprovalState)
	}
}

func TestDeclineRecommendationReturnsToManual(t *testing.T) {
	a := testApp(t)
	task := &Task{
		ID: "run-decline", Status: "awaiting_approval", ApprovalState: "recommended", WorkspaceID: a.wsID(),
		approvalGeneration: 2, ApprovalDeadline: "2026-11-10T00:00:00Z",
		ApprovalReviews: []ApprovalReview{{Kind: "files", Status: "recommended", Recommendation: "approve"}},
		Files:           []Change{{Path: "decline.md", Content: "DECLINE"}},
	}
	s := &Session{ID: "plan-decline", Runs: []*Task{task}}
	a.sessions[s.ID] = s

	w := request(a, "POST", "/api/sessions/plan-decline/runs/run-decline/decline", map[string]any{})
	requireStatus(t, w, 200)
	if task.ApprovalState != "manual" || task.ApprovalDeadline != "" || task.approvalGeneration != 3 {
		t.Fatalf("decline did not reset recommendation: state=%s gen=%d", task.ApprovalState, task.approvalGeneration)
	}
	if task.ApprovalReviews[0].Status != "declined" {
		t.Fatalf("decline not recorded: %+v", task.ApprovalReviews[0])
	}
	a.applyRecommendedFiles(s.ID, task.ID, 2)
	if _, err := os.Stat(filepath.Join(a.workPath, "decline.md")); err == nil {
		t.Fatal("declined recommendation was applied by the timer")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/plan-decline/runs/run-decline/decline", map[string]any{}), 409)
}
