package server

import (
	"fmt"
	"sort"
	"strings"
)

// Read-only projections reuse persisted task/reminder state. Polling never
// claims a job, retries a generation, grants approval or mutates a reminder.
func (a *App) sessionWorkEventLocked(s *Session) *assistantWatchEvent {
	if s == nil || s.Deleted || len(s.Runs) == 0 {
		return nil
	}
	run := s.Runs[len(s.Runs)-1]
	names := map[string]string{"running": "正在处理", "completed": "已完成", "failed": "执行失败", "stopped": "已停止", "cancelled": "已取消", "interrupted": "已中断", "paused": "已暂停", "awaiting_approval": "等待你的审批", "awaiting_clarification": "需要你的回答", "queued": "正在排队"}
	status := names[run.Status]
	if status == "" {
		status = run.Status
	}
	delivery := ""
	if run.Delivery != nil {
		delivery = run.Delivery.State
	}
	event := watchEvent("task", run.ID+":"+run.Status+":"+delivery, fmt.Sprintf("任务 #%d「%s」%s。", s.Number, s.Title, status), s.ID)
	event.Status = run.Status
	event.RunID = run.ID
	event.Delivery = run.Delivery
	event.UpdatedAt = s.Updated
	event.Priority = 1
	event.NextAction = "等待当前执行结果"
	switch run.Status {
	case "awaiting_approval":
		event.Priority = 3
		event.NextAction = "打开任务审阅具体操作；由已有授权范围内的审批流程处理"
	case "awaiting_clarification":
		event.Priority = 3
		event.NextAction = "打开任务回答必要问题；助理不能代答人工审批"
	case "failed":
		event.Priority = 3
		event.NextAction = "核对失败原因后决定恢复或重试；未自动重新提交收费请求"
	case "paused", "interrupted":
		event.Priority = 3
		event.NextAction = "打开任务从已有检查点继续；先核对已经执行的操作"
	case "completed":
		event.Priority = 2
		event.NextAction = "查看交付结果"
	case "stopped", "cancelled":
		event.NextAction = "已停止；只有用户重新交办才继续"
	}
	if run.Error != "" {
		event.Blocker = boundedEvidenceText(run.Error, 300)
	}
	if run.Delivery != nil && delivery != "delivered" {
		event.Blocker = run.Delivery.Reason
		if event.Priority < 3 {
			event.Priority = 3
		}
		event.NextAction = "核对交付缺口或待审批文件；不按成功交付汇报"
	}
	if run.Status == "completed" {
		for i := len(run.Steps) - 1; i >= 0; i-- {
			if run.Steps[i].Name == "chat" || run.Steps[i].Name == "agent" {
				r := []rune(run.Steps[i].Content)
				if len(r) > 240 {
					r = append(r[:240], '…')
				}
				event.Detail = string(r)
				break
			}
		}
	}
	// Goal acceptance is a business outcome, separate from executor completion.
	if run.Status == "completed" {
		item := a.findPlanItemLocked(s, run.PlanTaskID)
		if item != nil && item.Contract != nil {
			goalStatus, label, next := "completed", "目标已逐项验收", "查看已验收的目标与交付结果"
			switch {
			case item.RunID != run.ID:
				goalStatus, label, next = "historical", "历史执行结束，当前目标以最新执行和验收为准", "查看当前目标；历史执行结果不代表当前签收"
			case item.Status == "closed" && !goalAccepted(item, run):
				goalStatus, label, next = "closed", "目标已关闭，未逐项验收", "已关闭；只有用户重新交办才继续"
			case !goalAccepted(item, run):
				goalStatus, label, next = goalPendingStatus(item, run), "执行结束，目标仍待验收", "打开任务与规划逐项复核；先处理未通过条件，不能代替用户签收"
			}
			criterionIDs := []string{}
			for _, c := range item.Contract.Criteria {
				criterionIDs = append(criterionIDs, c.ID)
			}
			event.ID = watchEvent("task", run.ID+":"+run.Status+":"+delivery+":goal:"+goalStatus+":"+recoveryHash(criterionIDs), "", s.ID).ID
			event.Status = goalStatus
			event.Text = fmt.Sprintf("任务 #%d「%s」%s。", s.Number, s.Title, label)
			event.NextAction = next
			if goalStatus != "completed" {
				event.Blocker = strings.TrimSpace(event.Blocker + " 目标尚未完成逐项签收。")
				event.Detail = ""
			}
			if goalStatus == "blocked" {
				event.Priority = 3
			}
			if goalStatus == "closed" || goalStatus == "historical" {
				event.Priority = 1
			}
		}
	}
	return &event
}
func (a *App) assistantWorkLedgerLocked() []assistantWatchEvent {
	events := []assistantWatchEvent{}
	for _, s := range a.sessions {
		if s.FollowedByAssistant {
			if event := a.sessionWorkEventLocked(s); event != nil {
				events = append(events, *event)
			}
		}
	}
	for _, r := range a.reminders {
		e := r.Execution
		if e == nil || e.WorkspaceID != a.wsID() {
			continue
		}
		names := map[string]string{"pending": "等待执行", "dispatching": "正在交办", "submitted": "已提交执行", "succeeded": "执行成功", "failed": "执行失败", "interrupted": "执行中断", "cancelled": "已取消"}
		status := names[e.State]
		if status == "" {
			status = e.State
		}
		event := watchEvent("schedule", r.ID+":"+e.State, "定时执行「"+r.Title+"」："+status, e.SessionID)
		event.Status = e.State
		event.RunID = e.RunID
		event.Priority = 1
		event.UpdatedAt = e.FinishedAt
		event.NextAction = "按用户已保存的执行安排处理；无需保持页面打开，服务必须运行"
		if e.State == "failed" || e.State == "interrupted" {
			event.Priority = 3
			event.Blocker = boundedEvidenceText(e.Error, 300)
			event.NextAction = "在提醒中心核对并重新安排；未自动重放"
		}
		if e.State == "succeeded" {
			event.Priority = 2
			event.NextAction = "查看执行结果"
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Priority != events[j].Priority {
			return events[i].Priority > events[j].Priority
		}
		if events[i].UpdatedAt != events[j].UpdatedAt {
			return events[i].UpdatedAt > events[j].UpdatedAt
		}
		return events[i].ID < events[j].ID
	})
	if len(events) > 100 {
		events = events[:100]
	}
	return events
}
