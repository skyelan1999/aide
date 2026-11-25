package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// bindTaskToSessionLineage keeps manually created and automatically spawned
// sub-agents on the same attribution and tool-isolation path.
func bindTaskToSessionLineage(s *Session, task *Task) {
	if s == nil || task == nil || s.ParentID == "" {
		return
	}
	if task.AgentDepth < 1 {
		task.AgentDepth = 1
	}
	task.ParentSessionID = s.ParentID
	task.ChildSessionID = s.ID
	task.ChildNumber = s.Number
	task.ChildTitle = s.Title
}

func derivedSessionTitle(source *Session, kind, requested string) string {
	if title := strings.TrimSpace(requested); title != "" {
		return title
	}
	prefix := "分支: "
	if kind == "subagent" {
		prefix = "子: "
	}
	title := []rune(prefix + strings.TrimSpace(source.Title))
	if len(title) > 48 {
		title = title[:48]
	}
	return string(title)
}

// deriveSession creates an independent branch or a direct child with the same
// conversation context. Only terminal runs are copied, and every copied run
// receives a new ID so retries/resumes in the branch cannot affect the source.
func (a *App) deriveSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if in.Kind != "session" && in.Kind != "subagent" {
		fail(w, http.StatusBadRequest, errors.New("未知会话派生类型"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	source := a.sessionLocked(r.PathValue("id"))
	if source == nil {
		fail(w, http.StatusNotFound, errors.New("会话不存在"))
		return
	}
	if source.Kind == assistantSessionKind {
		fail(w, http.StatusForbidden, errors.New("小秘系统会话不可派生"))
		return
	}
	if in.Kind == "subagent" && source.ParentID != "" {
		fail(w, http.StatusBadRequest, errors.New("子 Agent 不能再次创建子 Agent"))
		return
	}
	for _, run := range source.Runs {
		if run != nil && (run.Status == "running" || run.Status == "awaiting_approval" || run.Status == "awaiting_clarification" || run.Status == "paused") {
			fail(w, http.StatusConflict, errors.New("当前会话仍有任务运行或等待处理，请完成或停止后再派生会话"))
			return
		}
	}
	raw, err := json.Marshal(source)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	var derived Session
	if err := json.Unmarshal(raw, &derived); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	runIDs := map[string]string{}
	for _, run := range derived.Runs {
		if run == nil {
			continue
		}
		oldID := run.ID
		run.ID = newID()
		runIDs[oldID] = run.ID
		run.PlanTaskID = ""
		run.PlanContext = ""
		run.ParentTaskID = ""
		run.ParentSessionID = ""
		run.ChildSessionID = ""
		run.ChildNumber = 0
		run.ChildTitle = ""
		run.AgentDepth = 0
	}
	for i := range derived.Messages {
		if sourceRunID := derived.Messages[i].SourceRunID; sourceRunID != "" {
			derived.Messages[i].SourceRunID = runIDs[sourceRunID]
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	derived.ID = newID()
	derived.Title = derivedSessionTitle(source, in.Kind, in.Title)
	derived.Created = now
	derived.Updated = now
	derived.ParentID = ""
	if in.Kind == "subagent" {
		derived.ParentID = source.ID
	}
	derived.Kind = ""
	derived.Number = 0
	derived.PendingPrompt = ""
	derived.Impacts = nil
	derived.AssistantResultNotices = nil
	derived.Plan = nil
	derived.PlanClosed = nil
	derived.Pinned = false
	derived.Archived = false
	derived.Deleted = false
	derived.Checked = true
	derived.AutoArchived = false
	derived.FollowedByAssistant = false
	derived.FollowNote = ""
	derived.historyDeferred = false
	a.assignSessionNumber(&derived)
	if err := a.save(&derived); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	a.sessions[derived.ID] = &derived
	a.broadcastSessionsChanged(derived.ID)
	jsonOut(w, http.StatusCreated, &derived)
}
