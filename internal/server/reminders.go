package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type Reminder struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Description         string `json:"description,omitempty"`
	DueAt               string `json:"dueAt,omitempty"`
	Priority            string `json:"priority"`
	Status              string `json:"status"`
	Area                string `json:"area"` // global | workspace
	WorkspaceID         string `json:"workspaceId,omitempty"`
	WorkspaceName       string `json:"workspaceName,omitempty"`
	Source              string `json:"source"` // manual | aide | xiaomi
	Notify              string `json:"notify"` // both | popup | xiaomi | none
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	CompletedAt         string `json:"completedAt,omitempty"`
	PopupNotifiedAt     string `json:"popupNotifiedAt,omitempty"`
	AssistantNotifiedAt string `json:"assistantNotifiedAt,omitempty"`
}

type reminderStore struct {
	Version int        `json:"version"`
	Items   []Reminder `json:"items"`
}

type reminderInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	DueAt       string `json:"dueAt"`
	Priority    string `json:"priority"`
	Area        string `json:"area"`
	Notify      string `json:"notify"`
}

type reminderPatch struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	DueAt       *string `json:"dueAt"`
	Priority    *string `json:"priority"`
	Area        *string `json:"area"`
	Notify      *string `json:"notify"`
}

func (a *App) loadReminders() error {
	if err := os.MkdirAll(RemindersDir(a.dataPath), 0700); err != nil {
		return fmt.Errorf("创建提醒存储目录失败: %w", err)
	}
	b, err := os.ReadFile(RemindersPath(a.dataPath))
	if errors.Is(err, os.ErrNotExist) {
		a.reminders = []Reminder{}
		return nil
	}
	if err != nil {
		return err
	}
	var store reminderStore
	if err := json.Unmarshal(b, &store); err != nil {
		return fmt.Errorf("提醒数据损坏: %w", err)
	}
	if store.Version != 1 {
		return fmt.Errorf("不支持的提醒数据版本: %d", store.Version)
	}
	a.reminders = store.Items
	if a.reminders == nil {
		a.reminders = []Reminder{}
	}
	return nil
}

// persistRemindersLocked requires a.mu.
func (a *App) persistRemindersLocked() error {
	if err := os.MkdirAll(RemindersDir(a.dataPath), 0700); err != nil {
		return err
	}
	return atomicJSON(RemindersPath(a.dataPath), reminderStore{Version: 1, Items: a.reminders})
}

func normalizeReminderInput(in reminderInput, workspaceID, workspaceName, actor string) (Reminder, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if in.Title == "" || len([]rune(in.Title)) > 200 {
		return Reminder{}, errors.New("提醒标题必填且不能超过 200 个字符")
	}
	if len([]rune(in.Description)) > 4000 {
		return Reminder{}, errors.New("提醒说明不能超过 4000 个字符")
	}
	dueAt := strings.TrimSpace(in.DueAt)
	if dueAt != "" {
		due, err := time.Parse(time.RFC3339, dueAt)
		if err != nil {
			return Reminder{}, errors.New("提醒时间格式无效，请使用 RFC3339 日期时间")
		}
		dueAt = due.UTC().Format(time.RFC3339)
	}
	priority := strings.ToLower(strings.TrimSpace(in.Priority))
	if priority == "" {
		priority = "normal"
	}
	if priority != "urgent" && priority != "high" && priority != "normal" && priority != "low" {
		return Reminder{}, errors.New("优先级必须是 urgent、high、normal 或 low")
	}
	area := strings.ToLower(strings.TrimSpace(in.Area))
	if area == "" {
		area = "workspace"
	}
	if area != "global" && area != "workspace" {
		return Reminder{}, errors.New("存储区域必须是 global 或 workspace")
	}
	notify := strings.ToLower(strings.TrimSpace(in.Notify))
	if notify == "" {
		notify = "both"
	}
	if notify != "both" && notify != "popup" && notify != "xiaomi" && notify != "none" {
		return Reminder{}, errors.New("提醒方式必须是 both、popup、xiaomi 或 none")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r := Reminder{ID: newID(), Title: in.Title, Description: in.Description, DueAt: dueAt, Priority: priority,
		Status: "pending", Area: area, Source: actor, Notify: notify, CreatedAt: now, UpdatedAt: now}
	if area == "workspace" {
		if workspaceID == "" {
			return Reminder{}, errors.New("当前没有可绑定的工作区")
		}
		r.WorkspaceID = workspaceID
		r.WorkspaceName = workspaceName
	}
	return r, nil
}

func (a *App) createReminder(actor string, in reminderInput) (Reminder, error) {
	if actor != "manual" && actor != "aide" && actor != "xiaomi" {
		return Reminder{}, errors.New("提醒创建来源无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	workspaceID, workspaceName := a.wsID(), strings.TrimSpace(a.workspaceDisplay)
	r, err := normalizeReminderInput(in, workspaceID, workspaceName, actor)
	if err != nil {
		return Reminder{}, err
	}
	a.reminders = append(a.reminders, r)
	if err := a.persistRemindersLocked(); err != nil {
		a.reminders = a.reminders[:len(a.reminders)-1]
		return Reminder{}, err
	}
	return r, nil
}

func reminderPriorityValue(p string) int {
	switch p {
	case "urgent":
		return 4
	case "high":
		return 3
	case "normal":
		return 2
	default:
		return 1
	}
}

func (a *App) listReminderItems(actor, query, area, status, workspaceID string) ([]Reminder, error) {
	if actor != "manual" && actor != "xiaomi" {
		return nil, errors.New("当前来源无权读取提醒列表")
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Reminder, 0, len(a.reminders))
	for _, r := range a.reminders {
		if area == "global" && r.Area != "global" {
			continue
		}
		if area == "workspace" && (r.Area != "workspace" || (workspaceID != "" && r.WorkspaceID != workspaceID)) {
			continue
		}
		if workspaceID != "" && area != "global" && area != "workspace" && r.Area == "workspace" && r.WorkspaceID != workspaceID {
			continue
		}
		if status != "" && r.Status != status {
			continue
		}
		if lower != "" && !strings.Contains(strings.ToLower(r.Title+"\n"+r.Description+"\n"+r.WorkspaceName), lower) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if reminderPriorityValue(out[i].Priority) != reminderPriorityValue(out[j].Priority) {
			return reminderPriorityValue(out[i].Priority) > reminderPriorityValue(out[j].Priority)
		}
		if out[i].DueAt == "" {
			return false
		}
		if out[j].DueAt == "" {
			return true
		}
		return out[i].DueAt < out[j].DueAt
	})
	return out, nil
}

func (a *App) updateReminder(actor, id string, patch reminderPatch) (Reminder, error) {
	if actor != "manual" && actor != "xiaomi" {
		return Reminder{}, errors.New("当前来源无权修改提醒")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.reminders {
		r := &a.reminders[i]
		if r.ID != id {
			continue
		}
		if r.Status == "completed" {
			return Reminder{}, errors.New("已完成提醒不能编辑")
		}
		workspaceID, workspaceName := a.wsID(), strings.TrimSpace(a.workspaceDisplay)
		in := reminderInput{Title: r.Title, Description: r.Description, DueAt: r.DueAt, Priority: r.Priority, Area: r.Area, Notify: r.Notify}
		if patch.Title != nil {
			in.Title = *patch.Title
		}
		if patch.Description != nil {
			in.Description = *patch.Description
		}
		if patch.DueAt != nil {
			in.DueAt = *patch.DueAt
		}
		if patch.Priority != nil {
			in.Priority = *patch.Priority
		}
		if patch.Area != nil {
			in.Area = *patch.Area
		}
		if patch.Notify != nil {
			in.Notify = *patch.Notify
		}
		updated, err := normalizeReminderInput(in, workspaceID, workspaceName, r.Source)
		if err != nil {
			return Reminder{}, err
		}
		updated.ID, updated.CreatedAt, updated.Status = r.ID, r.CreatedAt, r.Status
		updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if updated.Area == r.Area && updated.WorkspaceID == r.WorkspaceID {
			updated.WorkspaceName = r.WorkspaceName
		}
		updated.PopupNotifiedAt = r.PopupNotifiedAt
		updated.AssistantNotifiedAt = r.AssistantNotifiedAt
		old := *r
		*r = updated
		if err := a.persistRemindersLocked(); err != nil {
			*r = old
			return Reminder{}, err
		}
		return *r, nil
	}
	return Reminder{}, errors.New("提醒不存在")
}

func (a *App) completeReminder(actor, id string) (Reminder, error) {
	if actor != "manual" && actor != "aide" && actor != "xiaomi" {
		return Reminder{}, errors.New("提醒完成来源无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.reminders {
		if a.reminders[i].ID != id {
			continue
		}
		if a.reminders[i].Status != "completed" {
			r := &a.reminders[i]
			old := *r
			r.Status, r.CompletedAt, r.UpdatedAt = "completed", time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)
			if err := a.persistRemindersLocked(); err != nil {
				*r = old
				return Reminder{}, err
			}
		}
		return a.reminders[i], nil
	}
	return Reminder{}, errors.New("提醒不存在")
}

func (a *App) deleteReminder(actor, id string) error {
	if actor == "aide" {
		return errors.New("aide 无权删除提醒")
	}
	if actor != "manual" && actor != "xiaomi" {
		return errors.New("提醒删除来源无效")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.reminders {
		if a.reminders[i].ID != id {
			continue
		}
		oldItems := append([]Reminder(nil), a.reminders...)
		a.reminders = append(a.reminders[:i], a.reminders[i+1:]...)
		if err := a.persistRemindersLocked(); err != nil {
			a.reminders = oldItems
			return err
		}
		return nil
	}
	return errors.New("提醒不存在")
}

func (a *App) markPopupSeen(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.reminders {
		if a.reminders[i].ID == id {
			if a.reminders[i].PopupNotifiedAt != "" {
				return nil
			}
			a.reminders[i].PopupNotifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if err := a.persistRemindersLocked(); err != nil {
				a.reminders[i].PopupNotifiedAt = ""
				return err
			}
			return nil
		}
	}
	return errors.New("提醒不存在")
}

func (a *App) duePopupReminders(workspaceID string, now time.Time) []Reminder {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Reminder{}
	for _, r := range a.reminders {
		if r.Status != "pending" || r.PopupNotifiedAt != "" || (r.Notify != "both" && r.Notify != "popup") || r.DueAt == "" {
			continue
		}
		due, err := time.Parse(time.RFC3339, r.DueAt)
		if err != nil || due.After(now) {
			continue
		}
		if r.Area == "workspace" && r.WorkspaceID != workspaceID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if reminderPriorityValue(out[i].Priority) != reminderPriorityValue(out[j].Priority) {
			return reminderPriorityValue(out[i].Priority) > reminderPriorityValue(out[j].Priority)
		}
		return out[i].DueAt < out[j].DueAt
	})
	return out
}

func (a *App) dispatchAssistantReminders(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	assistant := a.findAssistantSessionLocked()
	if assistant == nil {
		return
	}
	changed := false
	for i := range a.reminders {
		r := &a.reminders[i]
		if r.Status != "pending" || r.AssistantNotifiedAt != "" || (r.Notify != "both" && r.Notify != "xiaomi") || r.DueAt == "" {
			continue
		}
		due, err := time.Parse(time.RFC3339, r.DueAt)
		if err != nil || due.After(now) {
			continue
		}
		found := false
		for _, msg := range assistant.Messages {
			if msg.ReminderID == r.ID {
				found = true
				break
			}
		}
		if !found {
			text := "⏰ 提醒到期：" + r.Title
			if r.Description != "" {
				text += "\n" + r.Description
			}
			assistant.Messages = append(assistant.Messages, Message{Role: "assistant", Type: "reminder", ReminderID: r.ID, Content: text})
			assistant.Updated = now.UTC().Format(time.RFC3339Nano)
			assistant.Checked = false
			if err := a.save(assistant); err != nil {
				assistant.Messages = assistant.Messages[:len(assistant.Messages)-1]
				log.Printf("保存小秘提醒消息失败: %v", err)
				continue
			}
		}
		r.AssistantNotifiedAt = now.UTC().Format(time.RFC3339Nano)
		changed = true
	}
	if changed {
		if err := a.persistRemindersLocked(); err != nil {
			log.Printf("保存小秘提醒状态失败: %v", err)
		}
		a.broadcastSessionsChanged(assistant.ID)
	}
}

func (a *App) reminderLoop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	// Defer the first sweep until the ticker fires. New() is also used by tests
	// that populate session state immediately after construction; an eager sweep
	// races those callers and production startup does not need a sub-second scan.
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.dispatchAssistantReminders(now)
		}
	}
}

func (a *App) listRemindersHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	workspaceID := a.wsID()
	a.mu.Unlock()
	items, err := a.listReminderItems("manual", r.URL.Query().Get("q"), r.URL.Query().Get("area"), r.URL.Query().Get("status"), workspaceID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"items": items, "workspaceId": workspaceID})
}

func (a *App) dueRemindersHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	workspaceID := a.wsID()
	a.mu.Unlock()
	jsonOut(w, 200, a.duePopupReminders(workspaceID, time.Now()))
}

func (a *App) createReminderHandler(w http.ResponseWriter, r *http.Request) {
	var in reminderInput
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	reminder, err := a.createReminder("manual", in)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 201, reminder)
}

func (a *App) updateReminderHandler(w http.ResponseWriter, r *http.Request) {
	var patch reminderPatch
	if err := decode(w, r, &patch); err != nil {
		fail(w, 400, err)
		return
	}
	updated, err := a.updateReminder("manual", r.PathValue("id"), patch)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, updated)
}

func (a *App) completeReminderHandler(w http.ResponseWriter, r *http.Request) {
	completed, err := a.completeReminder("manual", r.PathValue("id"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, completed)
}

func (a *App) popupSeenReminderHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.markPopupSeen(r.PathValue("id")); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) deleteReminderHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.deleteReminder("manual", r.PathValue("id")); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func reminderToolSchema(name, description string, properties map[string]any, required []string) any {
	return map[string]any{"type": "function", "function": map[string]any{
		"name": name, "description": description,
		"parameters": map[string]any{"type": "object", "properties": properties, "required": required},
	}}
}

func aideReminderToolSchemas() []any {
	return []any{
		reminderToolSchema("reminder_create", "Create a reminder. You may choose global or current-workspace storage, priority, due date/time and notification channel. You cannot edit or delete reminders.", map[string]any{
			"title": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
			"dueAt":    map[string]any{"type": "string", "description": "RFC3339 date/time, optional"},
			"priority": map[string]any{"type": "string", "enum": []string{"urgent", "high", "normal", "low"}},
			"area":     map[string]any{"type": "string", "enum": []string{"global", "workspace"}},
			"notify":   map[string]any{"type": "string", "enum": []string{"both", "popup", "xiaomi", "none"}},
		}, []string{"title"}),
		reminderToolSchema("reminder_complete", "Mark an existing reminder completed. This is the only change allowed on an existing reminder.", map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"}),
	}
}

func xiaomiReminderToolSchemas() []any {
	props := map[string]any{
		"title": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
		"dueAt":    map[string]any{"type": "string", "description": "RFC3339 date/time; empty clears it"},
		"priority": map[string]any{"type": "string", "enum": []string{"urgent", "high", "normal", "low"}},
		"area":     map[string]any{"type": "string", "enum": []string{"global", "workspace"}},
		"notify":   map[string]any{"type": "string", "enum": []string{"both", "popup", "xiaomi", "none"}},
	}
	return []any{
		reminderToolSchema("reminder_list", "Search and list reminders from all storage areas, sorted by priority and due date.", map[string]any{"query": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "completed", "all"}}}, nil),
		reminderToolSchema("reminder_create", "Create a reminder in global or current-workspace storage.", props, []string{"title"}),
		reminderToolSchema("reminder_update", "Edit any field of an existing reminder, including moving it between global and current-workspace storage.", map[string]any{"id": map[string]any{"type": "string"}, "title": props["title"], "description": props["description"], "dueAt": props["dueAt"], "priority": props["priority"], "area": props["area"], "notify": props["notify"]}, []string{"id"}),
		reminderToolSchema("reminder_complete", "Mark an existing reminder completed.", map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"}),
		reminderToolSchema("reminder_delete", "Delete an existing reminder.", map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"}),
	}
}

func reminderArgs(call ToolCall) map[string]any {
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	if args == nil {
		args = map[string]any{}
	}
	return args
}

func (a *App) executeReminderTool(actor string, call ToolCall) string {
	args := reminderArgs(call)
	str := func(key string) string { value, _ := args[key].(string); return strings.TrimSpace(value) }
	switch call.Function.Name {
	case "reminder_create":
		r, err := a.createReminder(actor, reminderInput{Title: str("title"), Description: str("description"), DueAt: str("dueAt"), Priority: str("priority"), Area: str("area"), Notify: str("notify")})
		if err != nil {
			return "创建提醒失败: " + err.Error()
		}
		b, _ := json.Marshal(r)
		return string(b)
	case "reminder_complete":
		r, err := a.completeReminder(actor, str("id"))
		if err != nil {
			return "标记提醒完成失败: " + err.Error()
		}
		b, _ := json.Marshal(r)
		return string(b)
	case "reminder_update":
		if actor != "xiaomi" {
			return "权限拒绝：aide 不能修改已有提醒"
		}
		var patch reminderPatch
		if v, ok := args["title"].(string); ok {
			patch.Title = &v
		}
		if v, ok := args["description"].(string); ok {
			patch.Description = &v
		}
		if v, ok := args["dueAt"].(string); ok {
			patch.DueAt = &v
		}
		if v, ok := args["priority"].(string); ok {
			patch.Priority = &v
		}
		if v, ok := args["area"].(string); ok {
			patch.Area = &v
		}
		if v, ok := args["notify"].(string); ok {
			patch.Notify = &v
		}
		r, err := a.updateReminder(actor, str("id"), patch)
		if err != nil {
			return "修改提醒失败: " + err.Error()
		}
		b, _ := json.Marshal(r)
		return string(b)
	case "reminder_delete":
		if actor != "xiaomi" {
			return "权限拒绝：aide 不能删除提醒"
		}
		if err := a.deleteReminder(actor, str("id")); err != nil {
			return "删除提醒失败: " + err.Error()
		}
		return "提醒已删除"
	case "reminder_list":
		if actor != "xiaomi" {
			return "权限拒绝：aide 不能读取提醒列表"
		}
		status := str("status")
		if status == "all" {
			status = ""
		}
		items, err := a.listReminderItems(actor, str("query"), "", status, "")
		if err != nil {
			return err.Error()
		}
		b, _ := json.Marshal(items)
		return string(b)
	default:
		return "未知提醒操作"
	}
}

func (a *App) dispatchAssistantReminderTool(call ToolCall) string {
	return a.executeReminderTool("xiaomi", call)
}

func (a *App) reminderActorForTask(taskID string) string {
	sessionID := a.liveSessionID(taskID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[sessionID]; s != nil && s.Kind == assistantSessionKind {
		return "xiaomi"
	}
	return "aide"
}

func dueReminderText(r Reminder) string {
	return fmt.Sprintf("%s（%s）", r.Title, r.Priority)
}
