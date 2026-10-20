package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// Only the manual reminder API accepts this authorization. Persist an immutable
// session ID as well as the human-facing number; never resolve the number again.
type reminderExecutionInput struct {
	Target        string `json:"target"` // xiaomi | session
	SessionNumber int    `json:"sessionNumber,omitempty"`
	Prompt        string `json:"prompt"`
}
type ReminderExecution struct {
	Target        string `json:"target"`
	SessionID     string `json:"sessionId"`
	SessionNumber int    `json:"sessionNumber,omitempty"`
	WorkspaceID   string `json:"workspaceId"`
	Prompt        string `json:"prompt"`
	State         string `json:"state"` // pending, dispatching, submitted, succeeded, failed, interrupted, cancelled
	RunID         string `json:"runId,omitempty"`
	RunSessionID  string `json:"runSessionId,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
	FinishedAt    string `json:"finishedAt,omitempty"`
	Error         string `json:"error,omitempty"`
}

func cloneReminder(r Reminder) Reminder {
	if r.Execution != nil {
		e := *r.Execution
		r.Execution = &e
	}
	return r
}

type scheduledReminderKey struct{}
type scheduledWorkspaceKey struct{}

func (a *App) validateScheduledWorkspaceLocked(ctx context.Context) error {
	id, _ := ctx.Value(scheduledWorkspaceKey{}).(string)
	if id == "" {
		return nil
	}
	if id != a.wsID() {
		return errors.New("定时任务工作区已变化")
	}
	return ctx.Err()
}

func (a *App) newReminderExecutionLocked(actor string, in *reminderExecutionInput, due string) (*ReminderExecution, error) {
	if in == nil {
		return nil, nil
	}
	if actor != "manual" {
		return nil, errors.New("定时执行只能由用户在提醒中心创建")
	}
	when, err := time.Parse(time.RFC3339, due)
	if err != nil || !when.After(time.Now()) {
		return nil, errors.New("定时执行需要未来的执行时间")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" || len(prompt) > 20000 {
		return nil, errors.New("执行指令需要 1–20000 字节")
	}
	var target *Session
	switch in.Target {
	case "xiaomi":
		target = a.findAssistantSessionLocked()
	case "session":
		if in.SessionNumber <= 0 {
			return nil, errors.New("请输入有效会话编号")
		}
		for _, s := range a.sessions {
			if s.Number == in.SessionNumber && s.Kind == "" {
				target = s
				break
			}
		}
	default:
		return nil, errors.New("执行目标必须是 xiaomi 或 session")
	}
	if target == nil || target.Deleted || target.Archived {
		return nil, errors.New("目标会话不存在或已归档")
	}
	return &ReminderExecution{Target: in.Target, SessionID: target.ID, SessionNumber: target.Number, WorkspaceID: a.wsID(), Prompt: prompt, State: "pending"}, nil
}
func executionInFlight(e *ReminderExecution) bool {
	return e != nil && (e.State == "dispatching" || e.State == "submitted")
}
func sessionScheduleBusy(s *Session) bool {
	for _, r := range s.Runs {
		switch r.Status {
		case "running", "awaiting_approval", "awaiting_clarification", "queued", "paused":
			return true
		}
	}
	return false
}

// Called inside the normal handler's lock, before it can steer a run or answer
// a clarification. Schedule input must never become a human approval answer.
func (a *App) validateScheduleDispatchLocked(ctx context.Context, s *Session) error {
	if err := a.validateScheduledWorkspaceLocked(ctx); err != nil {
		return err
	}
	id, _ := ctx.Value(scheduledReminderKey{}).(string)
	if id == "" {
		return nil
	}
	for _, r := range a.reminders {
		e := r.Execution
		if r.ID == id && r.Status == "pending" && e != nil && e.State == "dispatching" && e.SessionID == s.ID && e.WorkspaceID == a.wsID() {
			if s.Deleted || s.Archived || a.sessions[s.ID] != s {
				return errors.New("目标会话已删除或归档")
			}
			if sessionScheduleBusy(s) {
				return errors.New("目标会话正忙，请重新安排执行时间")
			}
			return nil
		}
	}
	return errors.New("定时执行授权已失效或工作区已变化")
}

// The persisted claim provides at-most-once dispatch across scans/restarts. A
// crash in the claim/delivery window is reported as interrupted, never replayed.
func (a *App) dispatchScheduledReminders(now time.Time) {
	a.mu.Lock()
	claims := []Reminder{}
	active := 0
	for _, r := range a.reminders {
		if r.Execution != nil && r.Execution.State == "dispatching" {
			active++
		}
	}
	for i := range a.reminders {
		r := &a.reminders[i]
		e := r.Execution
		if e == nil {
			continue
		}
		if e.State == "submitted" {
			runSessionID := e.RunSessionID
			if runSessionID == "" {
				runSessionID = e.SessionID
			}
			s := a.sessions[runSessionID]
			if s == nil || s.Deleted {
				old := *e
				e.State = "interrupted"
				e.Error = "目标会话已删除，执行结果无法核实"
				e.FinishedAt = now.UTC().Format(time.RFC3339Nano)
				if err := a.persistRemindersLocked(); err != nil {
					*e = old
				}
				continue
			}
			for _, run := range s.Runs {
				if run.ID == e.RunID {
					switch run.Status {
					case "completed", "failed", "cancelled", "resumed":
						old := *e
						e.State = "succeeded"
						if run.Status != "completed" {
							e.State = "failed"
						}
						e.Error = run.Error
						e.FinishedAt = now.UTC().Format(time.RFC3339Nano)
						if err := a.persistRemindersLocked(); err != nil {
							*e = old
						}
					}
					break
				}
			}
			continue
		}
		if active+len(a.cancels) >= 4 || r.Status != "pending" || e.State != "pending" || e.WorkspaceID != a.wsID() {
			continue
		}
		due, err := time.Parse(time.RFC3339, r.DueAt)
		if err != nil || due.After(now) {
			continue
		}
		s := a.sessions[e.SessionID]
		if s != nil && !s.Deleted && !s.Archived && sessionScheduleBusy(s) {
			continue
		}
		// Only one scheduled input per session may be in flight.
		busy := false
		for _, other := range a.reminders {
			if other.ID != r.ID && executionInFlight(other.Execution) && other.Execution.SessionID == e.SessionID {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		old := *e
		e.State = "dispatching"
		e.StartedAt = now.UTC().Format(time.RFC3339Nano)
		if err := a.persistRemindersLocked(); err != nil {
			*e = old
			continue
		}
		snapshot := *r
		copyExec := *e
		snapshot.Execution = &copyExec
		claims = append(claims, snapshot)
		active++
	}
	a.mu.Unlock()
	for _, r := range claims {
		claim := r
		a.background(func() { a.executeScheduledReminder(claim) })
	}
}
func (a *App) executeScheduledReminder(r Reminder) {
	e := r.Execution
	body := map[string]any{"prompt": e.Prompt, "mode": "chat"}
	path := "/api/sessions/" + e.SessionID + "/runs"
	if e.Target == "xiaomi" {
		body = map[string]any{"text": e.Prompt}
		path = "/api/sessions/" + e.SessionID + "/assistant-message"
	}
	payload, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(a.bgCtx, 2*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, scheduledReminderKey{}, r.ID)
	ctx = context.WithValue(ctx, scheduledWorkspaceKey{}, e.WorkspaceID)
	// Mark unattended inputs as assistant-originated so they cannot answer a
	// clarification or claim the authority of a fresh human message.
	ctx = context.WithValue(ctx, assistantDispatchKey{}, "定时任务："+e.Prompt)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", e.SessionID)
	out := httptest.NewRecorder()
	if e.Target == "xiaomi" {
		a.assistantMessageHandler(out, req)
	} else {
		a.startTask(out, req)
	}
	var result struct {
		ID         string `json:"id"`
		Error      string `json:"error"`
		Action     string `json:"action"`
		Reason     string `json:"reason"`
		Dispatched *struct {
			Started    bool   `json:"started"`
			SessionID  string `json:"sessionId"`
			RunID      string `json:"runId"`
			StartError string `json:"startError"`
		} `json:"dispatched"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &result)
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.reminders {
		current := a.reminders[i].Execution
		if a.reminders[i].ID != r.ID || current == nil || current.State != "dispatching" {
			continue
		}
		current.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		current.State = "succeeded"
		if out.Code >= 300 || result.Action == "locked" {
			current.State = "failed"
			current.Error = result.Error
			if current.Error == "" {
				current.Error = result.Reason
			}
			if current.Error == "" {
				current.Error = fmt.Sprintf("执行失败（HTTP %d）", out.Code)
			}
		} else if result.ID != "" {
			current.State = "submitted"
			current.RunID = result.ID
			current.RunSessionID = e.SessionID
			current.FinishedAt = ""
		} else if result.Action == "dispatch" && result.Dispatched == nil {
			current.State = "failed"
			current.Error = "未能创建执行会话"
		} else if result.Dispatched != nil {
			if result.Dispatched.Started && result.Dispatched.RunID != "" {
				current.State = "submitted"
				current.RunSessionID = result.Dispatched.SessionID
				current.RunID = result.Dispatched.RunID
				current.FinishedAt = ""
			} else {
				current.State = "failed"
				current.Error = result.Dispatched.StartError
			}
		}
		if err := a.persistRemindersLocked(); err != nil {
			// The durable claim already prevents replay. Keep an actionable in-memory
			// result instead of leaving the UI permanently in flight.
			current.State = "interrupted"
			current.Error = "执行回执保存失败，请核对目标会话；不会自动重复执行"
			current.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			log.Printf("保存定时任务执行回执失败 id=%s: %v", r.ID, err)
		}
		return
	}
}
