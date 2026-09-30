package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReminderAPICRUDSearchAndScopes(t *testing.T) {
	a := testApp(t)
	created := request(a, "POST", "/api/reminders", map[string]any{
		"title": "global urgent", "priority": "urgent", "area": "global", "notify": "popup",
	})
	requireStatus(t, created, 201)
	var global Reminder
	if err := json.Unmarshal(created.Body.Bytes(), &global); err != nil {
		t.Fatal(err)
	}
	if global.Area != "global" || global.Source != "manual" || global.Status != "pending" {
		t.Fatalf("unexpected reminder: %+v", global)
	}

	request(a, "POST", "/api/reminders", map[string]any{"title": "workspace high", "priority": "high", "area": "workspace"})
	list := request(a, "GET", "/api/reminders?q=global&area=global", nil)
	requireStatus(t, list, 200)
	var got struct {
		Items []Reminder `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != global.ID {
		t.Fatalf("filtered reminders: %+v", got.Items)
	}

	complete := request(a, "POST", "/api/reminders/"+global.ID+"/complete", map[string]any{})
	requireStatus(t, complete, 200)
	if err := json.Unmarshal(complete.Body.Bytes(), &global); err != nil {
		t.Fatal(err)
	}
	if global.Status != "completed" || global.CompletedAt == "" {
		t.Fatalf("not completed: %+v", global)
	}
	requireStatus(t, request(a, "DELETE", "/api/reminders/"+global.ID, nil), 200)
	if _, err := os.Stat(RemindersPath(a.dataPath)); err != nil {
		t.Fatalf("reminder store not persisted: %v", err)
	}
	if err := a.loadReminders(); err != nil {
		t.Fatal(err)
	}
	if len(a.reminders) != 1 {
		t.Fatalf("expected remaining workspace reminder, got %d", len(a.reminders))
	}
}

func TestReminderAidePermissionsAndXiaomiTools(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	assistant := a.findAssistantSessionLocked()
	a.mu.Unlock()
	if assistant == nil {
		t.Fatal("missing XiaoMi session")
	}
	toolNames := func(tools []any) map[string]bool {
		names := map[string]bool{}
		for _, item := range tools {
			fn, _ := item.(map[string]any)["function"].(map[string]any)
			if name, _ := fn["name"].(string); name != "" {
				names[name] = true
			}
		}
		return names
	}
	if names := toolNames(a.contextToolsFor(&Session{ID: "plain"})); !names["reminder_create"] || !names["reminder_complete"] || names["reminder_list"] || names["reminder_update"] || names["reminder_delete"] {
		t.Fatalf("ordinary aide reminder tools violate limited access: %v", names)
	}
	if names := toolNames(a.contextToolsFor(assistant)); !names["reminder_create"] || !names["reminder_complete"] || !names["reminder_list"] || !names["reminder_update"] || !names["reminder_delete"] {
		t.Fatalf("XiaoMi reminder tools missing full access: %v", names)
	}
	created, err := a.createReminder("aide", reminderInput{Title: "from aide", Area: "global"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Source != "aide" {
		t.Fatalf("source = %q", created.Source)
	}
	if _, err := a.listReminderItems("aide", "", "", "", ""); err == nil {
		t.Fatal("aide listed reminders")
	}
	if _, err := a.updateReminder("aide", created.ID, reminderPatch{}); err == nil {
		t.Fatal("aide edited reminder")
	}
	if err := a.deleteReminder("aide", created.ID); err == nil {
		t.Fatal("aide deleted reminder")
	}
	if _, err := a.completeReminder("aide", created.ID); err != nil {
		t.Fatalf("aide completion denied: %v", err)
	}
	managed, err := a.createReminder("manual", reminderInput{Title: "xiaomi managed", Area: "global"})
	if err != nil {
		t.Fatal(err)
	}

	tool := func(name, args string) string {
		var call ToolCall
		call.Function.Name, call.Function.Arguments = name, args
		return a.executeReminderTool("xiaomi", call)
	}
	if got := tool("reminder_update", `{"id":"`+managed.ID+`","priority":"low"}`); !strings.Contains(got, `"priority":"low"`) {
		t.Fatalf("Xiaomi update failed: %s", got)
	}
	if got := tool("reminder_list", `{"query":"from aide","status":"all"}`); !strings.Contains(got, created.ID) {
		t.Fatalf("Xiaomi list failed: %s", got)
	}
	if got := tool("reminder_delete", `{"id":"`+managed.ID+`"}`); got != "提醒已删除" {
		t.Fatalf("Xiaomi delete failed: %s", got)
	}
}

func TestDueRemindersPopupAndXiaomiDispatchAreIdempotent(t *testing.T) {
	a := testApp(t)
	created, err := a.createReminder("manual", reminderInput{Title: "due", Area: "global", Notify: "both", DueAt: time.Now().Add(-time.Minute).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.duePopupReminders("", time.Now()); len(got) != 1 || got[0].ID != created.ID {
		t.Fatalf("due popup reminders: %+v", got)
	}
	a.dispatchAssistantReminders(time.Now())
	a.dispatchAssistantReminders(time.Now().Add(time.Second))
	a.mu.Lock()
	assistant := a.findAssistantSessionLocked()
	count := 0
	for _, msg := range assistant.Messages {
		if msg.ReminderID == created.ID {
			count++
		}
	}
	a.mu.Unlock()
	if count != 1 {
		t.Fatalf("Xiaomi reminder messages = %d, want exactly one", count)
	}
	if err := a.markPopupSeen(created.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.duePopupReminders("", time.Now()); len(got) != 0 {
		t.Fatalf("already shown popup repeated: %+v", got)
	}
}
