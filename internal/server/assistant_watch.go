package server

import (
	"aide/internal/messaging"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type assistantWatchEvent struct {
	ID         string        `json:"id"`
	Kind       string        `json:"kind"`
	Text       string        `json:"text"`
	SessionID  string        `json:"sessionId,omitempty"`
	Number     int           `json:"sessionNumber,omitempty"`
	Status     string        `json:"status,omitempty"`
	Priority   int           `json:"priority"`
	Detail     string        `json:"detail,omitempty"`
	RunID      string        `json:"runId,omitempty"`
	Blocker    string        `json:"blocker,omitempty"`
	NextAction string        `json:"nextAction,omitempty"`
	UpdatedAt  string        `json:"updatedAt,omitempty"`
	Delivery   *TaskDelivery `json:"delivery,omitempty"`
}

func watchEvent(kind, key, text, session string) assistantWatchEvent {
	return assistantWatchEvent{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(kind+":"+key))), Kind: kind, Text: text, SessionID: session}
}

// Human approvals are visible even for sessions the assistant is not following.
// Only expose the session and approval type, never the underlying action.
func (a *App) humanApprovalEventsLocked() []assistantWatchEvent {
	events := []assistantWatchEvent{}
	workspace := a.wsID()
	for _, s := range a.pendingSessionBodiesLocked() {
		if s.Deleted || s.Archived || s.Kind == assistantSessionKind {
			continue
		}
		for _, run := range s.Runs {
			if (run.WorkspaceID != "" && run.WorkspaceID != workspace) || run.StopRequested || run.approvalAnswered {
				continue
			}
			switch run.ApprovalState {
			case "reviewing", "recommended", "approved", "applying":
				continue
			}
			var key, label string
			switch run.Status {
			case "awaiting_approval":
				if len(run.Files) > 0 {
					key, label = "files", "文件提案"
				}
			case "awaiting_clarification":
				var question struct {
					ID   string `json:"questionId"`
					Kind string `json:"approvalKind"`
				}
				if run.AnswerCh != nil && json.Unmarshal(run.PendingQuestion, &question) == nil &&
					(question.Kind == "shell" || surfaceApprovalKind(question.Kind)) {
					key, label = fmt.Sprintf("%s:%d", question.ID, run.answerRound), "操作"
				}
			}
			if key == "" {
				continue
			}
			event := watchEvent("approval", s.ID+":"+run.ID+":"+key,
				fmt.Sprintf("会话 #%d 有%s需要你审批。", s.Number, label), s.ID)
			event.Number, event.RunID, event.Status = s.Number, run.ID, run.Status
			event.Priority, event.UpdatedAt = 4, s.Updated
			event.NextAction = "打开对应会话查看审批"
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].UpdatedAt != events[j].UpdatedAt {
			return events[i].UpdatedAt > events[j].UpdatedAt
		}
		return events[i].ID < events[j].ID
	})
	return events
}

func nativeApprovalAnnouncement(events []assistantWatchEvent, seen map[string]bool) string {
	count, number := 0, 0
	for _, event := range events {
		if seen[event.ID] || event.Number <= 0 {
			continue
		}
		seen[event.ID] = true
		count++
		if number == 0 {
			number = event.Number
		}
	}
	if count == 0 {
		return ""
	}
	prefix := "只用一句话简短提醒用户，不作自我介绍，不决定审批，不调用工具。"
	if count == 1 {
		return prefix + fmt.Sprintf("会话 %d 有一项需要你审批，可以打开对应会话查看。", number)
	}
	return prefix + fmt.Sprintf("有 %d 项需要你审批，其中一项在会话 %d，其他可以打开工作统筹查看。", count, number)
}

// Passive page polling: no model calls, task starts or consent decisions.
func (a *App) assistantWatch(w http.ResponseWriter, r *http.Request) {
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	a.mu.Lock()
	assistant := a.sessionLocked(r.PathValue("id"))
	if assistant == nil || assistant.Kind != assistantSessionKind {
		a.mu.Unlock()
		fail(w, 404, errors.New("不是有效的小秘系统会话"))
		return
	}
	if !a.isAssistantUnlocked(assistant.ID) {
		a.mu.Unlock()
		fail(w, 423, errors.New("请先解锁小秘会话"))
		return
	}
	events := append(a.humanApprovalEventsLocked(), a.assistantWorkLedgerLocked()...)
	now := time.Now()
	for _, reminder := range a.reminders {
		if reminder.Status != "pending" || reminder.Notify == "none" || reminder.DueAt == "" || reminder.Area == "workspace" && reminder.WorkspaceID != a.wsID() {
			continue
		}
		due, err := time.Parse(time.RFC3339, reminder.DueAt)
		if err != nil || due.After(now) {
			continue
		}
		event := watchEvent("reminder", reminder.ID+":"+reminder.DueAt, "提醒到期："+reminder.Title, "")
		event.Priority = 3
		events = append(events, event)
	}
	a.mu.Unlock()
	a.messageMu.Lock()
	config, err := a.messageConfig()
	m := a.messages
	if err == nil && config.AssistantAccess && config.Enabled && config.Name != "" && m != nil {
		m.lease.RLock()
		a.messageMu.Unlock()
		data := m.service.Snapshot()
		m.lease.RUnlock()
		messages, _ := data["messages"].([]messaging.Message)
		read, _ := data["read"].(map[string]int64)
		for _, message := range messages {
			key := message.Peer
			if message.Group != "" {
				key = "group:" + message.Group
			}
			if message.Incoming && message.At.UnixNano() > read[key] {
				event := watchEvent("message", message.ID, "消息模块收到一条新的未读消息。", "")
				event.Priority = 2
				events = append(events, event)
			}
		}
	} else {
		a.messageMu.Unlock()
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Priority != events[j].Priority {
			return events[i].Priority > events[j].Priority
		}
		if events[i].Kind == "approval" && events[j].Kind == "approval" && events[i].UpdatedAt != events[j].UpdatedAt {
			return events[i].UpdatedAt > events[j].UpdatedAt
		}
		return events[i].ID < events[j].ID
	})
	if len(events) > 100 {
		events = events[:100]
	}
	quiet := false
	if b, e := a.selectedMixSoul(); b != nil || e != nil {
		quiet = true
		if e == nil {
			if state, err := a.mixSoulCall(r.Context(), *b, map[string]any{"phase": "initiative.status", "now": time.Now().UTC().Format(time.RFC3339Nano)}); err == nil {
				var parsed struct {
					Quiet bool `json:"quiet"`
				}
				if json.Unmarshal(state.State, &parsed) == nil {
					quiet = parsed.Quiet
				}
			}
		}
	}
	jsonOut(w, 200, map[string]any{"events": events, "proactiveQuiet": quiet})
}
