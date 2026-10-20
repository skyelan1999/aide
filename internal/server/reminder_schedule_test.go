package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func scheduleFixture(t *testing.T, a *App, target string) Reminder {
	t.Helper()
	if target == "session" {
		a.mu.Lock()
		a.sessions["scheduled-test"] = &Session{ID: "scheduled-test", Number: 987, Title: "fixture"}
		a.mu.Unlock()
	}
	r, err := a.createReminder("manual", reminderInput{Title: "scheduled fixture", DueAt: time.Now().Add(time.Hour).Format(time.RFC3339), Notify: "none", Execution: &reminderExecutionInput{Target: target, SessionNumber: 987, Prompt: "fixture task"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func waitSchedule(t *testing.T, a *App, id string) Reminder {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		a.mu.Lock()
		var r Reminder
		for _, v := range a.reminders {
			if v.ID == id {
				r = cloneReminder(v)
			}
		}
		a.mu.Unlock()
		if r.Execution != nil && r.Execution.State != "dispatching" {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("schedule did not settle")
	return Reminder{}
}
func TestReminderScheduleValidationAndPermissions(t *testing.T) {
	a := testApp(t)
	r := scheduleFixture(t, a, "session")
	if r.Execution.SessionID != "scheduled-test" || r.Execution.WorkspaceID != a.wsID() {
		t.Fatal(r)
	}
	due := time.Now().Add(time.Hour).Format(time.RFC3339)
	for _, actor := range []string{"aide", "xiaomi"} {
		if _, err := a.createReminder(actor, reminderInput{Title: "bad", DueAt: due, Execution: &reminderExecutionInput{Target: "xiaomi", Prompt: "bad"}}); err == nil {
			t.Fatal("model actor scheduled a task", actor)
		}
	}
	for _, body := range []map[string]any{
		{"title": "bad", "execution": map[string]any{"target": "xiaomi", "prompt": "x"}},
		{"title": "bad", "dueAt": due, "execution": map[string]any{"target": "session", "sessionNumber": 9999, "prompt": "x"}},
		{"title": "bad", "dueAt": due, "execution": map[string]any{"target": "xiaomi", "prompt": ""}},
		{"title": "bad", "dueAt": time.Now().Add(-time.Hour).Format(time.RFC3339), "execution": map[string]any{"target": "xiaomi", "prompt": "x"}},
	} {
		requireStatus(t, request(a, "POST", "/api/reminders", body), 400)
	}
	if _, err := a.updateReminder("xiaomi", r.ID, reminderPatch{Execution: json.RawMessage(`null`)}); err == nil {
		t.Fatal("tool edited authorization")
	}
	requireStatus(t, request(a, "GET", "/api/reminders", nil), 200)
	requireStatus(t, request(a, "PUT", "/api/reminders/"+r.ID, map[string]any{"execution": nil}), 200)
	a.mu.Lock()
	if a.reminders[0].Execution != nil {
		t.Error("not converted to notification")
	}
	a.mu.Unlock()
}
func TestReminderScheduleDueBusyAndAtMostOnce(t *testing.T) {
	a := testApp(t)
	r := scheduleFixture(t, a, "session")
	now := time.Now()
	a.dispatchScheduledReminders(now)
	a.mu.Lock()
	if a.reminders[0].Execution.State != "pending" {
		t.Fatal("early dispatch")
	}
	a.sessions["scheduled-test"].Runs = []*Task{{ID: "waiting", Status: "awaiting_clarification", AnswerCh: make(chan string, 1)}}
	a.mu.Unlock()
	a.dispatchScheduledReminders(now.Add(2 * time.Hour))
	a.mu.Lock()
	if a.reminders[0].Execution.State != "pending" {
		t.Fatal("busy dispatch")
	}
	a.sessions["scheduled-test"].Runs = nil
	a.mu.Unlock()
	// No model is configured: exercise the real startTask validation, never call a provider.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.dispatchScheduledReminders(now.Add(2 * time.Hour)) }()
	}
	wg.Wait()
	got := waitSchedule(t, a, r.ID)
	if got.Execution.State != "failed" || got.Execution.Error == "" {
		t.Fatalf("%+v", got.Execution)
	}
	a.dispatchScheduledReminders(now.Add(3 * time.Hour))
	again := waitSchedule(t, a, r.ID)
	if again.Execution.StartedAt != got.Execution.StartedAt || again.Execution.State != "failed" {
		t.Fatal("replayed")
	}
	if len(a.sessions["scheduled-test"].Messages) != 0 {
		t.Fatal("unexpected message/provider task")
	}
}
func TestReminderScheduleWorkspaceCancelAndApproval(t *testing.T) {
	a := testApp(t)
	r := scheduleFixture(t, a, "session")
	a.mu.Lock()
	e := a.reminders[0].Execution
	e.WorkspaceID = "other"
	a.mu.Unlock()
	a.dispatchScheduledReminders(time.Now().Add(2 * time.Hour))
	if waitSchedule(t, a, r.ID).Execution.State != "pending" {
		t.Fatal("cross workspace dispatch")
	}
	a.mu.Lock()
	e.WorkspaceID = a.wsID()
	e.State = "dispatching"
	s := a.sessions[e.SessionID]
	s.Runs = []*Task{{Status: "awaiting_clarification", AnswerCh: make(chan string, 1)}}
	ctx := context.WithValue(context.Background(), scheduledReminderKey{}, r.ID)
	if err := a.validateScheduleDispatchLocked(ctx, s); err == nil {
		t.Error("schedule may answer approval")
	}
	a.mu.Unlock()
	if _, err := a.completeReminder("manual", r.ID); err == nil {
		t.Fatal("inflight completion")
	}
	if err := a.deleteReminder("manual", r.ID); err == nil {
		t.Fatal("inflight delete")
	}
	a.mu.Lock()
	e.State = "pending"
	a.mu.Unlock()
	if _, err := a.completeReminder("manual", r.ID); err != nil {
		t.Fatal(err)
	}
	a.dispatchScheduledReminders(time.Now().Add(2 * time.Hour))
	if waitSchedule(t, a, r.ID).Execution.StartedAt != "" {
		t.Fatal("cancelled task started")
	}
}
func TestReminderScheduleRecoveryAndPersistenceFailure(t *testing.T) {
	a := testApp(t)
	r := scheduleFixture(t, a, "xiaomi")
	a.mu.Lock()
	a.reminders[0].Execution.State = "dispatching"
	if err := a.persistRemindersLocked(); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	if err := a.loadReminders(); err != nil {
		t.Fatal(err)
	}
	if got := waitSchedule(t, a, r.ID); got.Execution.State != "interrupted" {
		t.Fatal(got)
	}
	a.mu.Lock()
	a.reminders[0].Execution.State = "pending"
	a.mu.Unlock()
	// A directory where the atomic target should be causes rename failure.
	if err := os.Remove(RemindersPath(a.dataPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(RemindersPath(a.dataPath), 0700); err != nil {
		t.Fatal(err)
	}
	a.dispatchScheduledReminders(time.Now().Add(2 * time.Hour))
	got := waitSchedule(t, a, r.ID)
	if got.Execution.State != "pending" || got.Execution.StartedAt != "" {
		t.Fatal("dispatched without durable claim")
	}
}
func TestReminderScheduleLockedXiaomiAndResult(t *testing.T) {
	a := testApp(t)
	r := scheduleFixture(t, a, "xiaomi")
	a.mu.Lock()
	a.settings.UserPasswordHash = "fixture locked"
	a.mu.Unlock()
	a.dispatchScheduledReminders(time.Now().Add(2 * time.Hour))
	got := waitSchedule(t, a, r.ID)
	if got.Execution.State != "failed" {
		t.Fatalf("locked assistant: %+v", got.Execution)
	}
	a.mu.Lock()
	a.reminders[0].Execution.State = "submitted"
	a.reminders[0].Execution.SessionID = "result-session"
	a.reminders[0].Execution.RunID = "run-fixture"
	a.sessions["result-session"] = &Session{ID: "result-session", Runs: []*Task{{ID: "run-fixture", Status: "completed"}}}
	a.mu.Unlock()
	a.dispatchScheduledReminders(time.Now())
	got = waitSchedule(t, a, r.ID)
	if got.Execution.State != "succeeded" || got.Execution.FinishedAt == "" {
		t.Fatalf("result: %+v", got.Execution)
	}
}

func TestReminderScheduleRealRunAndXiaomi(t *testing.T) {
	for _, target := range []string{"session", "xiaomi"} {
		t.Run(target, func(t *testing.T) {
			a := testApp(t)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { parallelReply(w, "Fixture task complete.") }))
			defer provider.Close()
			a.mu.Lock()
			a.settings.BaseURL = provider.URL
			a.settings.Model = "test"
			a.mu.Unlock()
			if target == "xiaomi" {
				unlockAssistantSessionForTest(t, a)
			}
			r := scheduleFixture(t, a, target)
			a.dispatchScheduledReminders(time.Now().Add(2 * time.Hour))
			got := waitSchedule(t, a, r.ID)
			if target == "xiaomi" {
				if got.Execution.State != "succeeded" {
					t.Fatalf("%+v", got.Execution)
				}
				return
			}
			if got.Execution.State != "submitted" || got.Execution.RunID == "" {
				t.Fatalf("%+v", got.Execution)
			}
			waitSelectionTask(t, a, "scheduled-test")
			a.dispatchScheduledReminders(time.Now().Add(3 * time.Hour))
			got = waitSchedule(t, a, r.ID)
			if got.Execution.State != "succeeded" {
				t.Fatalf("%+v", got.Execution)
			}
			a.mu.Lock()
			task := a.sessions["scheduled-test"].Runs[0]
			if !task.AssistantManaged || task.AssistantAuthorization == "" {
				t.Error("unattended run lost origin")
			}
			a.mu.Unlock()
		})
	}
}

func TestReminderScheduleDelegationKeepsWorkspace(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.Model = "test"
	a.sessions["delegate"] = &Session{ID: "delegate", Title: "fixture"}
	a.mu.Unlock()
	ctx := context.WithValue(context.Background(), scheduledWorkspaceKey{}, "not-current")
	dispatch := map[string]any{"sessionId": "delegate"}
	a.startDispatchedAssistantTaskWithContext(ctx, dispatch, "fixture", false, "scheduled authorization")
	if dispatch["started"] != false {
		t.Fatal("delegation crossed workspace", dispatch)
	}
	dec := assistantDecision{}
	reply := a.execAssistantToolWithContext(ctx, asstToolCall("push_to_session", `{"ref":"new","message":"fixture"}`), &dec, "scheduled")
	if !strings.Contains(reply, "工作区") {
		t.Fatal(reply)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions["delegate"].Runs) != 0 {
		t.Fatal("cross-workspace execution")
	}
}
