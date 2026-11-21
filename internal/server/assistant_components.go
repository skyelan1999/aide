package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
)

func assistantComponentToolSchemas() []any {
	return []any{
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "aide_component_status",
			"description": "查询 Aide 当前工作区、会话运行、已安装插件或待人工审批。直接回报真实状态，不另建任务。插件市场使用 browse_plugin_market。",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"component": map[string]any{"type": "string", "enum": []string{"workspace", "sessions", "plugins", "approvals"}},
			}, "required": []string{"component"}},
		}},
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "control_session_run",
			"description": "用户本轮明确指定会话编号和一个动作时，暂停、继续、停止或重试该会话最近的一次运行。请直接调用，例如「暂停会话 #12」。查询、审批和清理跟进使用其他工具。",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		}},
	}
}

type assistantSessionState struct {
	ID      string `json:"id"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Updated string `json:"updatedAt"`
}

func (a *App) assistantComponentStatus(ctx context.Context, component string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.validateScheduledWorkspaceLocked(ctx); err != nil {
		return err.Error()
	}
	var result any
	switch component {
	case "workspace":
		result = map[string]any{
			"mode": a.workspaceMode(), "workingDirectory": a.statusWorkspaceLabelLocked(),
			"professionalWorkflow": a.pwAvailableLocked(),
		}
	case "sessions":
		workspace := a.wsID()
		rows := []assistantSessionState{}
		counts := map[string]int{}
		for _, s := range a.sessions {
			if s.Deleted || s.Archived || s.Kind == assistantSessionKind {
				continue
			}
			status := "idle"
			if len(s.Runs) > 0 {
				run := s.Runs[len(s.Runs)-1]
				if run.WorkspaceID != "" && run.WorkspaceID != workspace {
					continue
				}
				status = run.Status
			}
			rows = append(rows, assistantSessionState{s.ID, s.Number, s.Title, status, s.Updated})
			counts[status]++
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Updated != rows[j].Updated {
				return rows[i].Updated > rows[j].Updated
			}
			return rows[i].Number > rows[j].Number
		})
		recent := rows
		if len(recent) > 20 {
			recent = recent[:20]
		}
		result = map[string]any{"total": len(rows), "counts": counts, "recentSessions": recent}
	case "plugins":
		type installedPlugin struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version string `json:"version"`
			Enabled bool   `json:"enabled"`
		}
		plugins := []installedPlugin{}
		for _, p := range a.pluginRegistry.Plugins {
			if p.ID != "computer-control" {
				plugins = append(plugins, installedPlugin{p.ID, p.Name, p.Version, p.Enabled})
			}
		}
		sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
		result = plugins
	case "approvals":
		result = a.humanApprovalEventsLocked()
	default:
		return "请选择工作区、会话、插件或人工审批。"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

var assistantRunAction = regexp.MustCompile(`^(?:请[[:space:]]*)?(?:帮我[[:space:]]*)?(暂停|继续|恢复|停止|取消|重试)[[:space:]]*(?:任务[[:space:]]*)?(?:(?:会话[[:space:]]*[#＃]?|[#＃])[[:space:]]*([0-9]+)|([0-9]+)[[:space:]]*号[[:space:]]*会话)(?:[[:space:]]*(?:的)?[[:space:]]*(?:任务|运行))?[[:space:]。！!]*$`)

type assistantRunGuardKey struct{}

type assistantRunGuard struct {
	workspace string
	revision  uint64
	action    string
}

func (a *App) checkAssistantRunLocked(r *http.Request, s *Session, run *Task) error {
	guard, ok := r.Context().Value(assistantRunGuardKey{}).(assistantRunGuard)
	if !ok {
		return nil
	}
	if err := r.Context().Err(); err != nil {
		return errors.New("助理请求已结束")
	}
	if err := a.validateScheduledWorkspaceLocked(r.Context()); err != nil {
		return err
	}
	if s.Deleted || s.Archived || s.Kind == assistantSessionKind ||
		run.WorkspaceID != guard.workspace || a.wsID() != guard.workspace || a.wsRevision != guard.revision ||
		len(s.Runs) == 0 || s.Runs[len(s.Runs)-1] != run || run.StopRequested && (run.Status == "running" || run.Status == "awaiting_clarification") {
		return errors.New("会话或工作区已变化，请核对后重新发出指令")
	}
	ok = false
	switch guard.action {
	case "暂停":
		ok = run.Status == "running"
	case "继续", "恢复":
		ok = run.Status == "paused" || run.Status == "interrupted"
	case "停止", "取消":
		ok = run.Status == "running" || run.Status == "awaiting_clarification"
	case "重试":
		ok = run.Status == "failed" || run.Status == "interrupted" || run.Status == "stopped" || run.Status == "cancelled"
	}
	if !ok {
		return fmt.Errorf("当前任务状态 %s 无法%s", run.Status, guard.action)
	}
	return nil
}

func (a *App) controlAssistantSessionRun(ctx context.Context, heard string) string {
	match := assistantRunAction.FindStringSubmatch(strings.TrimSpace(heard))
	if match == nil {
		return "请明确说一个动作和会话编号，例如「暂停会话 #12」。"
	}
	action := match[1]
	a.mu.Lock()
	if err := a.validateScheduledWorkspaceLocked(ctx); err != nil {
		a.mu.Unlock()
		return err.Error()
	}
	s := a.resolveSessionRef("#" + match[2] + match[3])
	if s == nil || s.Archived || s.Kind == assistantSessionKind || len(s.Runs) == 0 {
		a.mu.Unlock()
		return "该会话没有可操作的任务，请核对会话编号。"
	}
	run := s.Runs[len(s.Runs)-1]
	guard := assistantRunGuard{workspace: a.wsID(), revision: a.wsRevision, action: action}
	sessionID, runID, number := s.ID, run.ID, s.Number
	a.mu.Unlock()

	r := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(context.WithValue(ctx, assistantRunGuardKey{}, guard))
	r.SetPathValue("id", sessionID)
	r.SetPathValue("run", runID)
	w := httptest.NewRecorder()
	switch action {
	case "暂停":
		a.pauseTask(w, r)
	case "继续", "恢复":
		a.resumeTask(w, r)
	case "停止", "取消":
		a.cancelTask(w, r)
	case "重试":
		a.retryTask(w, r)
	}
	if w.Code < 200 || w.Code >= 300 {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return fmt.Sprintf("会话 #%d 未能%s：%s。", number, action, result.Error)
	}
	return fmt.Sprintf("已提交会话 #%d 的%s请求，实际状态以会话为准。", number, action)
}
