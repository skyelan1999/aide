package server

import (
	"aide/internal/messaging"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type assistantWatchEvent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	SessionID string `json:"sessionId,omitempty"`
	Status    string `json:"status,omitempty"`
	Priority  int    `json:"priority"`
	Detail    string `json:"detail,omitempty"`
}

func watchEvent(kind, key, text, session string) assistantWatchEvent {
	return assistantWatchEvent{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(kind+":"+key))), Kind: kind, Text: text, SessionID: session}
}

// Passive page polling: no model calls, task starts or consent decisions.
func (a *App) assistantWatch(w http.ResponseWriter, r *http.Request) {
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	a.mu.Lock()
	assistant := a.sessions[r.PathValue("id")]
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
	events := []assistantWatchEvent{}
	statusName := map[string]string{"running": "正在处理", "completed": "已完成", "failed": "执行失败", "stopped": "已停止", "cancelled": "已取消", "interrupted": "已中断", "paused": "已暂停", "awaiting_approval": "等待你的审批", "awaiting_clarification": "需要你的回答", "queued": "正在排队"}
	for _, s := range a.sessions {
		if s.Deleted || !s.FollowedByAssistant || len(s.Runs) == 0 {
			continue
		}
		run := s.Runs[len(s.Runs)-1]
		status := statusName[run.Status]
		if status == "" {
			status = run.Status
		}
		event := watchEvent("task", run.ID+":"+run.Status, fmt.Sprintf("任务 #%d「%s」%s。", s.Number, s.Title, status), s.ID)
		event.Status = run.Status
		switch run.Status {
		case "awaiting_approval", "awaiting_clarification", "failed":
			event.Priority = 3
		case "completed":
			event.Priority = 2
		default:
			event.Priority = 1
		}
		if run.Status == "completed" {
			// Only final user-facing content, never private reasoning/tool arguments.
			for i := len(run.Steps) - 1; i >= 0; i-- {
				if run.Steps[i].Name != "chat" {
					continue
				}
				content := []rune(strings.TrimSpace(run.Steps[i].Content))
				if len(content) > 240 {
					content = append(content[:240], '…')
				}
				if len(content) > 0 {
					event.Detail = string(content)
					break
				}
			}
		}
		events = append(events, event)
	}
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
		return events[i].ID < events[j].ID
	})
	if len(events) > 100 {
		events = events[:100]
	}
	jsonOut(w, 200, map[string]any{"events": events})
}
