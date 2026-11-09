package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PlanItem 是会话级「任务与规划」的一项：要么是用户用自然语言添加的任务
// （source=user），要么是 aide 在执行中用 update_plan 拆解出来的步骤
// （source=aide，runId/step 指回所属 run）。它是编排视图的唯一数据源，
// 也是 aide 后续轮次理解背景与历史的上下文来源。
type PlanItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail,omitempty"`
	Source    string   `json:"source"` // user | aide
	Status    string   `json:"status"` // pending | in_progress | completed | failed | blocked | closed
	ParentID  string   `json:"parentId,omitempty"`
	RunID     string   `json:"runId,omitempty"`
	Step      int      `json:"step,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	Created   string   `json:"created"`
	Updated   string   `json:"updated"`
	ClosedAt  string   `json:"closedAt,omitempty"`
}

const (
	planSchema       = "aide.plan/v1"
	planItemLimit    = 200
	planTitleRunes   = 200
	planDetailRunes  = 2000
	planContextLimit = 40
)

func validPlanStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed", "failed", "blocked", "closed":
		return true
	}
	return false
}

func (a *App) findPlanItemLocked(s *Session, id string) *PlanItem {
	if s == nil {
		return nil
	}
	for i := range s.Plan {
		if s.Plan[i].ID == id {
			return &s.Plan[i]
		}
	}
	return nil
}

func planStepKey(runID string, step int) string { return runID + ":" + strconv.Itoa(step) }

func (a *App) planStepClosedLocked(s *Session, runID string, step int) bool {
	key := planStepKey(runID, step)
	for _, k := range s.PlanClosed {
		if k == key {
			return true
		}
	}
	return false
}

// addPlanTask 添加一条自然语言任务。dependsOn 指向同一会话内已有计划项，
// 用于导入编排好的工作流后按顺序执行。
func (a *App) addPlanTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title     string   `json:"title"`
		Detail    string   `json:"detail"`
		DependsOn []string `json:"dependsOn"`
		Agent     string   `json:"agent"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len([]rune(in.Title)) > planTitleRunes || len([]rune(in.Detail)) > planDetailRunes {
		fail(w, 400, fmt.Errorf("任务标题需为 1–%d 字，说明不超过 %d 字", planTitleRunes, planDetailRunes))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if len(s.Plan) >= planItemLimit {
		fail(w, 409, fmt.Errorf("单个会话最多 %d 项规划，请先关闭或归档旧任务", planItemLimit))
		return
	}
	for _, id := range in.DependsOn {
		if a.findPlanItemLocked(s, id) == nil {
			fail(w, 400, errors.New("依赖的任务不存在："+id))
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	item := PlanItem{ID: "plan-" + newID(), Title: in.Title, Detail: in.Detail, Source: "user", Status: "pending",
		DependsOn: append([]string(nil), in.DependsOn...), Agent: strings.TrimSpace(in.Agent), Created: now, Updated: now}
	s.Plan = append(s.Plan, item)
	s.Updated = now
	if err := a.save(s); err != nil {
		s.Plan = s.Plan[:len(s.Plan)-1]
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 201, item)
}

// updatePlanTask 只允许关闭/重新打开；真实执行状态由 run 与 update_plan 驱动。
func (a *App) updatePlanTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Status != "closed" && in.Status != "pending" {
		fail(w, 400, errors.New("只能关闭任务或重新打开"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	item := a.findPlanItemLocked(s, r.PathValue("item"))
	if item == nil {
		fail(w, 404, errors.New("任务不存在"))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	old, oldClosed := item.Status, item.ClosedAt
	if in.Status == "closed" {
		item.Status, item.ClosedAt = "closed", now
	} else {
		item.Status, item.ClosedAt = "pending", ""
	}
	item.Updated = now
	if err := a.save(s); err != nil {
		item.Status, item.ClosedAt = old, oldClosed
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, item)
}

// dismissPlanStep 关闭 aide 拆解出来的单步任务：不改写 run 本身的执行，
// 只把该步骤从编排视图收起，避免已放弃的步骤反复干扰后续判断。
func (a *App) dismissPlanStep(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunID string `json:"runId"`
		Step  int    `json:"step"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.TrimSpace(in.RunID) == "" || in.Step < 0 {
		fail(w, 400, errors.New("缺少 runId 或 step"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if !a.planStepClosedLocked(s, in.RunID, in.Step) {
		s.PlanClosed = append(s.PlanClosed, planStepKey(in.RunID, in.Step))
	}
	if err := a.save(s); err != nil {
		s.PlanClosed = s.PlanClosed[:len(s.PlanClosed)-1]
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, map[string]any{"closed": planStepKey(in.RunID, in.Step)})
}

type planDocument struct {
	Schema       string     `json:"schema"`
	SessionTitle string     `json:"sessionTitle,omitempty"`
	ExportedAt   string     `json:"exportedAt"`
	Tasks        []PlanItem `json:"tasks"`
}

// exportPlan 输出可编排的 JSON：保留依赖关系、执行归属与状态，便于在
// 另一个会话/仓库里复用成专业工作流的任务清单。
func (a *App) exportPlan(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		a.mu.Unlock()
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	doc := planDocument{Schema: planSchema, SessionTitle: s.Title, ExportedAt: time.Now().UTC().Format(time.RFC3339Nano), Tasks: append([]PlanItem(nil), s.Plan...)}
	a.mu.Unlock()
	if doc.Tasks == nil {
		doc.Tasks = []PlanItem{}
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "aide-plan-"+s.ID+".json"))
	jsonOut(w, 200, doc)
}

// importPlan 接收 aide.plan/v1 文档：条目一律以 pending 的用户任务导入，
// 旧的 id 映射为新 id，依赖关系同步重写；导入后由用户逐项交给 aide 执行。
func (a *App) importPlan(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := decode(w, r, &raw); err != nil {
		fail(w, 400, err)
		return
	}
	var doc planDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		// 兼容直接传 tasks 数组
		var tasks []PlanItem
		if err := json.Unmarshal(raw, &tasks); err != nil {
			fail(w, 400, errors.New("无法解析规划 JSON：需要 aide.plan/v1 文档或任务数组"))
			return
		}
		doc = planDocument{Tasks: tasks}
	}
	if doc.Schema != "" && doc.Schema != planSchema {
		fail(w, 400, errors.New("不支持的规划版本："+doc.Schema))
		return
	}
	if len(doc.Tasks) == 0 || len(doc.Tasks) > planItemLimit {
		fail(w, 400, fmt.Errorf("导入任务需为 1–%d 项", planItemLimit))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if len(s.Plan)+len(doc.Tasks) > planItemLimit {
		fail(w, 409, fmt.Errorf("导入后超过 %d 项上限，请先关闭或清理旧任务", planItemLimit))
		return
	}
	idMap := map[string]string{}
	for _, t := range doc.Tasks {
		if strings.TrimSpace(t.ID) != "" {
			idMap[t.ID] = "plan-" + newID()
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	added := make([]PlanItem, 0, len(doc.Tasks))
	for _, t := range doc.Tasks {
		title := strings.TrimSpace(t.Title)
		if title == "" || len([]rune(title)) > planTitleRunes || len([]rune(t.Detail)) > planDetailRunes {
			fail(w, 400, errors.New("导入任务标题为空或超长"))
			return
		}
		id := idMap[t.ID]
		if id == "" {
			id = "plan-" + newID()
		}
		deps := make([]string, 0, len(t.DependsOn))
		for _, dep := range t.DependsOn {
			if mapped := idMap[dep]; mapped != "" {
				deps = append(deps, mapped)
			}
		}
		added = append(added, PlanItem{ID: id, Title: title, Detail: t.Detail, Source: "user", Status: "pending",
			DependsOn: deps, Agent: strings.TrimSpace(t.Agent), Created: now, Updated: now})
	}
	s.Plan = append(s.Plan, added...)
	s.Updated = now
	if err := a.save(s); err != nil {
		s.Plan = s.Plan[:len(s.Plan)-len(added)]
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, map[string]any{"imported": len(added), "items": added})
}

// syncAgentPlanLocked 把 update_plan 的计划镜像进会话规划。按标题保留
// 已完成/已关闭状态，避免模型重复下发同一计划时把进度清零。
func (a *App) syncAgentPlanLocked(s *Session, task *Task) {
	if s == nil || task == nil || task.AgentPlan == nil {
		return
	}
	existing := map[string]PlanItem{}
	for _, item := range s.Plan {
		if item.Source == "aide" && item.RunID == task.ID {
			existing[item.Title] = item
		}
	}
	kept := s.Plan[:0]
	for _, item := range s.Plan {
		if !(item.Source == "aide" && item.RunID == task.ID) {
			kept = append(kept, item)
		}
	}
	s.Plan = kept
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, step := range task.AgentPlan.Items {
		if len(s.Plan) >= planItemLimit {
			break
		}
		item := PlanItem{ID: "plan-" + newID(), Title: step.Step, Source: "aide", Status: step.Status,
			RunID: task.ID, Step: i, ParentID: task.PlanTaskID, Created: now, Updated: now}
		if old, ok := existing[step.Step]; ok {
			item.ID, item.Created = old.ID, old.Created
			item.DependsOn, item.Agent = old.DependsOn, old.Agent
			if old.Status == "completed" && step.Status != "completed" {
				item.Status = "completed"
			}
			if old.Status == "closed" {
				item.Status, item.ClosedAt = "closed", old.ClosedAt
			}
		}
		if a.planStepClosedLocked(s, task.ID, i) && item.Status != "closed" {
			item.Status, item.ClosedAt = "closed", now
		}
		s.Plan = append(s.Plan, item)
	}
}

// syncPlanOutcomeLocked 在 run 收尾时给规划收口：成功则未完成的步骤记为完成，
// 失败/中止则记为失败；用户任务的状态跟随它启动的 run。
func (a *App) syncPlanOutcomeLocked(s *Session, task *Task) {
	if s == nil || task == nil {
		return
	}
	ok := task.Status == "completed" || task.Status == "awaiting_approval"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range s.Plan {
		item := &s.Plan[i]
		if item.Source != "aide" || item.RunID != task.ID || item.Status == "closed" {
			continue
		}
		if item.Status == "pending" || item.Status == "in_progress" {
			if ok {
				item.Status = "completed"
			} else {
				item.Status = "failed"
			}
			item.Updated = now
		}
	}
	if task.PlanTaskID == "" {
		return
	}
	item := a.findPlanItemLocked(s, task.PlanTaskID)
	if item == nil || item.Status == "closed" {
		return
	}
	item.RunID, item.Updated = task.ID, now
	switch {
	case ok:
		item.Status = "completed"
	case task.Status == "running":
		item.Status = "in_progress"
	default:
		item.Status = "failed"
	}
}

// planContextLocked 生成给模型的「任务与规划」摘要：未完成项 + 最近审批结论。
// 这是 aide 跨轮次记住背景与历史的来源，避免重复询问或遗漏。
func (a *App) planContextLocked(s *Session, focusID string, approvalLimit int) string {
	if s == nil {
		return ""
	}
	var lines []string
	open := 0
	for _, item := range s.Plan {
		if item.Status == "closed" || item.Status == "completed" {
			continue
		}
		open++
		if open > planContextLimit {
			break
		}
		mark := map[string]string{"pending": "○", "in_progress": "●", "failed": "✗", "blocked": "!"}[item.Status]
		if mark == "" {
			mark = "○"
		}
		focus := ""
		if item.ID == focusID {
			focus = "（本次执行）"
		}
		deps := ""
		if len(item.DependsOn) > 0 {
			deps = " 依赖:" + strings.Join(item.DependsOn, ",")
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s%s%s", mark, item.Source, item.Title, focus, deps))
	}
	if len(lines) == 0 {
		return ""
	}
	var approvals []string
	for i := len(s.Runs) - 1; i >= 0 && len(approvals) < approvalLimit; i-- {
		for j := len(s.Runs[i].ApprovalReviews) - 1; j >= 0 && len(approvals) < approvalLimit; j-- {
			rec := s.Runs[i].ApprovalReviews[j]
			approvals = append(approvals, fmt.Sprintf("- %s：%s（%s）", rec.Kind, rec.Status, truncateRunes(rec.Reason, 80)))
		}
	}
	out := "会话级任务与规划（未完成项）：\n" + strings.Join(lines, "\n")
	if len(approvals) > 0 {
		out += "\n最近审批记录：\n" + strings.Join(approvals, "\n")
	}
	out += "\n以上为上下文参考；执行进度以实际工具结果为准，关闭的任务不要自行恢复。"
	return out
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
