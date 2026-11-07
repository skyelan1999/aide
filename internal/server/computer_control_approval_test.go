package server

import (
	"encoding/json"
	"os"
	"testing"
)

func TestComputerAppApprovalChoices(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		remember     bool
	}{
		{"once", "确认", false},
		{"remember", "记住并允许", true},
		{"deny", "拒绝", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			a.settings.ComputerControlEnabled = true
			q, _ := json.Marshal(map[string]any{"question": "Allow TextEdit?", "type": "confirm", "approvalKind": "computer-app", "app": "TextEdit", "appRule": "bundle:com.apple.TextEdit", "questionId": "app-question", "approvalRound": "1"})
			task := &Task{ID: "app-run", Mode: "chat", Status: "awaiting_clarification", WorkspaceID: a.wsID(), AnswerCh: make(chan string, 1), answerRound: 1, PendingQuestion: q}
			s := &Session{ID: "app-session", Runs: []*Task{task}}
			a.sessions[s.ID] = s
			if err := a.save(s); err != nil {
				t.Fatal(err)
			}
			w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": tc.answer, "questionId": "app-question", "round": "1"})
			requireStatus(t, w, 200)
			if got := <-task.AnswerCh; got != tc.answer {
				t.Fatalf("answer=%q, want %q", got, tc.answer)
			}
			if got := computerAppAllowed(a.settings.ComputerControlAllowedApps, computerControlTarget{Name: "TextEdit", Rule: "bundle:com.apple.TextEdit"}); got != tc.remember {
				t.Fatalf("remembered=%v, want %v", got, tc.remember)
			}
			if tc.remember {
				data, err := os.ReadFile(SettingsPath(a.dataPath))
				if err != nil {
					t.Fatal(err)
				}
				if !json.Valid(data) {
					t.Fatal("remembered settings are invalid JSON")
				}
			}
		})
	}
}

func TestComputerAppApprovalRejectsStaleQuestion(t *testing.T) {
	a := testApp(t)
	a.settings.ComputerControlEnabled = true
	q := json.RawMessage(`{"question":"Allow app?","type":"confirm","approvalKind":"computer-app","app":"TextEdit","appRule":"bundle:com.apple.TextEdit","questionId":"fresh","approvalRound":"1"}`)
	task := &Task{ID: "app-run", Mode: "chat", Status: "awaiting_clarification", WorkspaceID: a.wsID(), AnswerCh: make(chan string, 1), answerRound: 1, PendingQuestion: q}
	s := &Session{ID: "app-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "记住并允许", "questionId": "old", "round": "1"})
	requireStatus(t, w, 409)
	if len(task.AnswerCh) != 0 || len(a.settings.ComputerControlAllowedApps) != 0 {
		t.Fatal("stale approval changed state")
	}
}

func TestComputerAppApprovalSaveFailureDoesNotRemember(t *testing.T) {
	a := testApp(t)
	a.settings.ComputerControlEnabled = true
	q := json.RawMessage(`{"question":"Allow app?","type":"confirm","approvalKind":"computer-app","app":"TextEdit","appRule":"bundle:com.apple.TextEdit","questionId":"fresh","approvalRound":"1"}`)
	task := &Task{ID: "app-run", Mode: "chat", Status: "awaiting_clarification", WorkspaceID: a.wsID(), AnswerCh: make(chan string, 1), answerRound: 1, PendingQuestion: q}
	s := &Session{ID: "app-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	path := SessionPath(a.dataPath, s.ID, "active")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "记住并允许", "questionId": "fresh", "round": "1"})
	requireStatus(t, w, 500)
	if len(task.AnswerCh) != 0 || len(a.settings.ComputerControlAllowedApps) != 0 {
		t.Fatal("failed approval changed state")
	}
}
