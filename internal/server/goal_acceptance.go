package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// A contract extends an existing user plan item. Its execution, delivery and
// operator acceptance remain separate; models cannot manufacture a review.
type GoalContract struct {
	Deliverables []string        `json:"deliverables,omitempty"`
	Criteria     []GoalCriterion `json:"criteria"`
}
type GoalCriterion struct {
	ID      string       `json:"id"`
	Text    string       `json:"text"`
	Review  *GoalReview  `json:"review,omitempty"`
	History []GoalReview `json:"history,omitempty"`
}
type GoalContractArchive struct {
	Contract   *GoalContract `json:"contract"`
	RunID      string        `json:"runId,omitempty"`
	ArchivedAt string        `json:"archivedAt"`
}
type GoalReview struct {
	State          string `json:"state"`
	Note           string `json:"note"`
	Source         string `json:"source"`
	RunID          string `json:"runId"`
	Fingerprint    string `json:"fingerprint"`
	ToolIndex      int    `json:"toolIndex,omitempty"`
	EvidenceDigest string `json:"evidenceDigest,omitempty"`
	RecordedAt     string `json:"recordedAt"`
}

func freshGoalContract(in *GoalContract) (*GoalContract, error) {
	if in == nil {
		return nil, nil
	}
	if len(in.Criteria) < 1 || len(in.Criteria) > 20 || len(in.Deliverables) > 10 {
		return nil, errors.New("目标须包含 1–20 项验收条件，交付物不超过 10 项")
	}
	out := &GoalContract{Criteria: []GoalCriterion{}}
	for _, text := range in.Deliverables {
		text = strings.TrimSpace(text)
		if text == "" || len([]rune(text)) > 200 {
			return nil, errors.New("交付物须为 1–200 字")
		}
		out.Deliverables = append(out.Deliverables, text)
	}
	for _, c := range in.Criteria {
		text := strings.TrimSpace(c.Text)
		if text == "" || len([]rune(text)) > 500 {
			return nil, errors.New("验收条件须为 1–500 字")
		}
		out.Criteria = append(out.Criteria, GoalCriterion{ID: "criterion-" + newID(), Text: text})
	}
	return out, nil
}
func planRun(s *Session, item *PlanItem) *Task {
	if s != nil && item != nil {
		for _, run := range s.Runs {
			if run != nil && run.ID == item.RunID && run.PlanTaskID == item.ID {
				return run
			}
		}
	}
	return nil
}
func goalRunFingerprint(run *Task) string {
	if run == nil {
		return ""
	}
	return recoveryHash([]any{run.ID, run.Status, run.WorkspaceID, run.WorkspaceRev, run.Files, run.Delivery, run.ToolUses, run.ExecutionSequence})
}
func goalExecutionReady(run *Task) bool {
	if run == nil || run.Status != "completed" {
		return false
	}
	if run.Delivery != nil && run.Delivery.State != "delivered" {
		return false
	}
	for _, f := range run.Files {
		if !f.Applied {
			return false
		}
	}
	return true
}
func goalCriterionState(c GoalCriterion, run *Task) string {
	return goalCriterionStateAt(c, run, goalRunFingerprint(run))
}
func goalCriterionStateAt(c GoalCriterion, run *Task, fingerprint string) string {
	r := c.Review
	if r == nil {
		return "pending"
	}
	if run == nil || r.Source != "operator_observation" || r.RunID != run.ID || r.Fingerprint != fingerprint {
		return "needs_review"
	}
	if r.ToolIndex > 0 && (r.ToolIndex > len(run.ToolUses) || r.EvidenceDigest != outcomeDigest(run.ToolUses[r.ToolIndex-1])) {
		return "needs_review"
	}
	if r.State == "passed" && !goalExecutionReady(run) {
		return "needs_review"
	}
	return r.State
}
func goalAccepted(item *PlanItem, run *Task) bool {
	return goalAcceptedAt(item, run, goalRunFingerprint(run))
}
func goalAcceptedAt(item *PlanItem, run *Task, fingerprint string) bool {
	if item == nil || item.Contract == nil || len(item.Contract.Criteria) == 0 || !goalExecutionReady(run) {
		return false
	}
	for _, c := range item.Contract.Criteria {
		if goalCriterionStateAt(c, run, fingerprint) != "passed" {
			return false
		}
	}
	return true
}
func goalPendingStatus(item *PlanItem, run *Task) string {
	fingerprint := goalRunFingerprint(run)
	if !goalExecutionReady(run) {
		return "blocked"
	}
	for _, c := range item.Contract.Criteria {
		state := goalCriterionStateAt(c, run, fingerprint)
		if state == "failed" || state == "blocked" {
			return "blocked"
		}
	}
	return "awaiting_acceptance"
}
func goalDependencyReady(s *Session, item *PlanItem) bool {
	if item == nil {
		return true
	}
	if item.Contract != nil {
		return (item.Status == "completed" || item.Status == "closed") && goalAccepted(item, planRun(s, item))
	}
	return item.Status == "completed" || item.Status == "closed"
}
func goalSnapshot(item *PlanItem, run *Task, workspaceID string) string {
	return recoveryHash([]any{workspaceID, item, goalRunFingerprint(run)})
}
func goalProjection(item *PlanItem, run *Task, workspaceID string) map[string]any {
	fingerprint := goalRunFingerprint(run)
	checks := []map[string]any{}
	if item.Contract != nil {
		for _, c := range item.Contract.Criteria {
			checks = append(checks, map[string]any{"id": c.ID, "text": c.Text, "state": goalCriterionStateAt(c, run, fingerprint), "review": c.Review, "history": c.History})
		}
	}
	receipts := []map[string]any{}
	if run != nil {
		for i, use := range run.ToolUses {
			if len(receipts) >= 100 {
				break
			}
			receipts = append(receipts, map[string]any{"index": i + 1, "tool": use.Tool, "digest": outcomeDigest(use)})
		}
	}
	return map[string]any{"workspaceId": workspaceID, "snapshot": recoveryHash([]any{workspaceID, item, fingerprint}), "contract": item.Contract, "criteria": checks, "accepted": goalAcceptedAt(item, run, fingerprint), "executionReady": goalExecutionReady(run), "runId": item.RunID, "receipts": receipts}
}
func goalContractContext(item PlanItem) string {
	if item.Contract == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n目标：%s\n交付物：%s\n验收条件（仅为目标，不代表已验证；模型不能代替用户签收）：", item.Title, strings.Join(item.Contract.Deliverables, "；"))
	for _, c := range item.Contract.Criteria {
		fmt.Fprintf(&b, "\n- %s", c.Text)
		if c.Review != nil {
			fmt.Fprintf(&b, "（历史用户复核：%s；%s。新执行须重新验收）", c.Review.State, truncateRunes(c.Review.Note, 200))
		}
	}
	return b.String()
}
func (a *App) goalAcceptanceAPI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Snapshot    string        `json:"snapshot"`
		Contract    *GoalContract `json:"contract"`
		CriterionID string        `json:"criterionId"`
		State       string        `json:"state"`
		Note        string        `json:"note"`
		ToolIndex   int           `json:"toolIndex"`
	}
	if r.Method != "GET" {
		if err := decode(w, r, &in); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.workspaceConfigMu.RLock()
	defer a.workspaceConfigMu.RUnlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loadLockStateLocked().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	item := a.findPlanItemLocked(s, r.PathValue("item"))
	if item == nil || item.Source != "user" {
		fail(w, 404, errors.New("用户目标不存在"))
		return
	}
	run := planRun(s, item)
	if r.Method != "GET" && run != nil && (run.WorkspaceID != a.wsID() || run.WorkspaceRev != a.wsRevision) {
		fail(w, 409, errors.New("目标执行所属工作区已变化"))
		return
	}
	if r.Method == "GET" {
		jsonOut(w, 200, goalProjection(item, run, a.wsID()))
		return
	}
	if item.WorkspaceID != "" && item.WorkspaceID != a.wsID() {
		fail(w, 409, errors.New("目标属于其他项目，请返回原项目或导出后重新导入"))
		return
	}
	if item.Status == "closed" {
		fail(w, 409, errors.New("请先重新打开目标"))
		return
	}
	if in.Snapshot == "" || in.Snapshot != goalSnapshot(item, run, a.wsID()) {
		fail(w, 409, errors.New("目标或执行证据已变化，请刷新后重试"))
		return
	}
	oldRaw, _ := json.Marshal(s.Plan)
	oldUpdated := s.Updated
	if r.Method == "PUT" {
		if run != nil && (run.Status == "running" || run.Status == "awaiting_approval" || run.Status == "awaiting_clarification" || run.Status == "paused") {
			fail(w, 409, errors.New("执行中不能替换验收条件，请先停止当前执行"))
			return
		}
		contract, err := freshGoalContract(in.Contract)
		if err != nil || contract == nil {
			if err == nil {
				err = errors.New("验收条件不能为空")
			}
			fail(w, 400, err)
			return
		}
		if item.Contract != nil {
			item.ContractHistory = append(item.ContractHistory, GoalContractArchive{Contract: item.Contract, RunID: item.RunID, ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			if len(item.ContractHistory) > 5 {
				item.ContractHistory = item.ContractHistory[len(item.ContractHistory)-5:]
			}
		}
		item.Contract = contract
		item.WorkspaceID = a.wsID()
	} else {
		if item.Contract == nil {
			fail(w, 400, errors.New("尚未设置验收条件"))
			return
		}
		if in.State != "pending" && in.State != "passed" && in.State != "failed" && in.State != "blocked" {
			fail(w, 400, errors.New("无效验收状态"))
			return
		}
		in.Note = strings.TrimSpace(in.Note)
		if in.State != "pending" && (run == nil || in.Note == "" || len([]rune(in.Note)) > 2000) {
			fail(w, 400, errors.New("验收须关联执行并填写 1–2000 字实际观察"))
			return
		}
		if in.State == "passed" && !goalExecutionReady(run) {
			fail(w, 409, errors.New("执行未完成或文件仍待审批，不能签收"))
			return
		}
		if run != nil && (run.Status == "running" || run.Status == "awaiting_approval" || run.Status == "awaiting_clarification") {
			fail(w, 409, errors.New("请等待执行结束后复核"))
			return
		}
		if in.ToolIndex < 0 || run != nil && in.ToolIndex > len(run.ToolUses) {
			fail(w, 400, errors.New("工具证据不存在"))
			return
		}
		found := false
		for i := range item.Contract.Criteria {
			c := &item.Contract.Criteria[i]
			if c.ID != in.CriterionID {
				continue
			}
			found = true
			if c.Review != nil {
				c.History = append(c.History, *c.Review)
				if len(c.History) > 10 {
					c.History = c.History[len(c.History)-10:]
				}
			}
			c.Review = nil
			if in.State != "pending" {
				c.Review = &GoalReview{State: in.State, Note: in.Note, Source: "operator_observation", RunID: run.ID, Fingerprint: goalRunFingerprint(run), ToolIndex: in.ToolIndex, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
				if in.ToolIndex > 0 {
					c.Review.EvidenceDigest = outcomeDigest(run.ToolUses[in.ToolIndex-1])
				}
			}
			break
		}
		if !found {
			fail(w, 404, errors.New("验收条件不存在"))
			return
		}
	}
	item.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	s.Updated = item.Updated
	if run != nil {
		a.syncPlanOutcomeLocked(s, run)
	}
	if err := a.save(s); err != nil {
		_ = json.Unmarshal(oldRaw, &s.Plan)
		s.Updated = oldUpdated
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, goalProjection(item, run, a.wsID()))
}

// Read-only tool: model planning can address observed gaps but cannot sign off.
func (a *App) taskAcceptance(task *Task, criterionID string) string {
	a.workspaceConfigMu.RLock()
	defer a.workspaceConfigMu.RUnlock()
	if !a.taskWorkspaceIsCurrent(task) {
		return "工作区已变化，拒绝读取其他项目的目标"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	owner := a.approvalSessionLocked(task)
	if owner == nil {
		return "当前任务没有所属会话"
	}
	item := a.findPlanItemLocked(owner, task.PlanTaskID)
	if item == nil || item.Contract == nil {
		return "当前执行未绑定含验收条件的用户目标；现有执行与交付回执仍保留"
	}
	rows := []map[string]any{}
	run := planRun(owner, item)
	fingerprint := goalRunFingerprint(run)
	for _, c := range item.Contract.Criteria {
		if criterionID != "" && c.ID != criterionID {
			continue
		}
		var review *GoalReview
		truncated := false
		if c.Review != nil {
			copy := *c.Review
			if criterionID == "" && len([]rune(copy.Note)) > 200 {
				copy.Note = truncateRunes(copy.Note, 200)
				truncated = true
			}
			review = &copy
		}
		rows = append(rows, map[string]any{"id": c.ID, "text": c.Text, "state": goalCriterionStateAt(c, run, fingerprint), "review": review, "noteTruncated": truncated})
	}
	if criterionID != "" && len(rows) == 0 {
		return "当前目标中没有该验收条件"
	}
	raw, _ := json.Marshal(map[string]any{"goalId": item.ID, "goal": item.Title, "deliverables": item.Contract.Deliverables, "criteria": rows, "accepted": goalAcceptedAt(item, run, fingerprint), "note": "Only operator observations sign off; pass criterionId to read an untruncated observation."})
	return string(raw)
}
