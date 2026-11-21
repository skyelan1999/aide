package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantComponentHumanApprovals(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	current := a.wsID()
	file := &Task{ID: "file", Status: "awaiting_approval", WorkspaceID: current, ApprovalState: "manual",
		Files: []Change{{Path: "qa.md", Content: "PRIVATE-FILE"}}}
	command := &Task{ID: "command", Status: "awaiting_clarification", WorkspaceID: current, AnswerCh: make(chan string, 1),
		PendingQuestion: json.RawMessage(`{"type":"confirm","questionId":"question","approvalKind":"shell","command":"PRIVATE-COMMAND"}`)}
	ordinary := &Task{ID: "ordinary", Status: "awaiting_clarification", WorkspaceID: current, AnswerCh: make(chan string, 1),
		PendingQuestion: json.RawMessage(`{"type":"question","questionId":"clarify"}`)}
	reviewing := &Task{ID: "reviewing", Status: "awaiting_approval", ApprovalState: "reviewing", WorkspaceID: current, Files: file.Files}
	recommended := &Task{ID: "recommended", Status: "awaiting_approval", ApprovalState: "recommended", WorkspaceID: current, Files: file.Files}
	other := &Task{ID: "foreign", Status: "awaiting_approval", WorkspaceID: "different-workspace", Files: file.Files}
	for i, run := range []*Task{file, command, ordinary, reviewing, recommended, other} {
		a.sessions[run.ID] = &Session{ID: run.ID, Number: 70 + i, Runs: []*Task{run}}
	}
	watch := request(a, "GET", "/api/sessions/"+id+"/assistant-watch", nil)
	requireStatus(t, watch, 200)
	var out struct{ Events []assistantWatchEvent }
	if err := json.Unmarshal(watch.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 2 || strings.Contains(watch.Body.String(), "PRIVATE-") {
		t.Fatalf("unexpected approval reminders %s", watch.Body.String())
	}
	for _, e := range out.Events {
		if e.Kind != "approval" || e.Number != 70 && e.Number != 71 || e.Priority != 4 {
			t.Fatalf("unexpected event %+v", e)
		}
	}
	seen := map[string]bool{}
	if spoken := nativeApprovalAnnouncement(out.Events, seen); !strings.Contains(spoken, "2 项") {
		t.Fatalf("bad spoken reminder: %s", spoken)
	}
	if got := nativeApprovalAnnouncement(out.Events, seen); got != "" {
		t.Fatal("repeated approval reminder")
	}
	command.answerRound++
	if got := nativeApprovalAnnouncement(a.humanApprovalEventsLocked(), seen); !strings.Contains(got, "会话 71") {
		t.Fatalf("approval with changed round was not announced: %s", got)
	}
	a.sessions["file"].Runs = append(a.sessions["file"].Runs, &Task{ID: "second", WorkspaceID: current, Status: "awaiting_approval", Files: file.Files})
	if offer, _ := a.nativeApprovalForSession(current, "#70"); offer != nil {
		t.Fatal("voice approval guessed among multiple proposals")
	}
	command.approvalAnswered = true
	got := a.assistantComponentStatus(context.Background(), "approvals")
	if strings.Contains(got, `"runId":"command"`) || strings.Contains(got, "PRIVATE") {
		t.Fatalf("answered approval or sensitive text was included: %s", got)
	}
	sessions := a.assistantComponentStatus(context.Background(), "sessions")
	if !strings.Contains(sessions, "recentSessions") || strings.Contains(sessions, "foreign") {
		t.Fatalf("incorrect session listing: %s", sessions)
	}
}

func TestAssistantComponentControlsOnlyExplicitCurrentRun(t *testing.T) {
	a := testApp(t)
	run := &Task{ID: "run", Status: "running", WorkspaceID: a.wsID()}
	s := &Session{ID: "control-session", Number: 72, Runs: []*Task{run}}
	a.sessions[s.ID] = s
	for _, heard := range []string{"别停止会话 #72", "停止 #72 还是 #73", "能停止会话 #72 吗？", "停止 #72 的跟进", "停止 #72 并重试 #73"} {
		if result := a.controlAssistantSessionRun(context.Background(), heard); !strings.Contains(result, "请明确") || run.StopRequested {
			t.Fatalf("ambiguous authority %q: %s", heard, result)
		}
	}
	old := assistantRunGuard{workspace: a.wsID(), revision: a.wsRevision, action: "停止"}
	stale := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(context.WithValue(context.Background(), assistantRunGuardKey{}, old))
	stale.SetPathValue("id", s.ID)
	stale.SetPathValue("run", run.ID)
	a.wsRevision++
	w := httptest.NewRecorder()
	a.cancelTask(w, stale)
	if w.Code != 409 || run.StopRequested {
		t.Fatal("stale workspace allowed task control")
	}
	if result := a.controlAssistantSessionRun(context.Background(), "请帮我停止会话 #72。"); !strings.Contains(result, "已提交") || !run.StopRequested {
		t.Fatalf("explicit stop did not use normal task handler: %s", result)
	}
	if result := a.controlAssistantSessionRun(context.Background(), "重试会话 #72"); !strings.Contains(result, "未能") {
		t.Fatalf("retry allowed a running task: %s", result)
	}
}
