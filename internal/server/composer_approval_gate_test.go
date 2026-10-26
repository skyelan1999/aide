package server

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func issue108Pending(t *testing.T, kind, questionType string) (*App, *Session, *Task) {
	t.Helper()
	a := testApp(t)
	q, _ := json.Marshal(map[string]any{"question": "Owned confirmation", "type": questionType, "approvalKind": kind, "command": "mkdir owned-fixture-output", "questionId": "owned-current-question", "approvalRound": "7"})
	task := &Task{ID: "owned-run", Mode: "chat", Status: "awaiting_clarification", WorkspaceID: a.wsID(), AnswerCh: make(chan string, 1), answerRound: 7, PendingQuestion: q}
	s := &Session{ID: "owned-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	return a, s, task
}

func TestIssue108ComposerCannotResolveApprovalQuestions(t *testing.T) {
	for _, tc := range []struct{ kind, typ string }{{"shell", "confirm"}, {"shell", ""}, {"", "confirm"}} {
		t.Run(tc.kind+"-"+tc.typ, func(t *testing.T) {
			a, s, task := issue108Pending(t, tc.kind, tc.typ)
			file := SessionPath(a.dataPath, s.ID, "active")
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "确认"})
			if w.Code != 409 {
				t.Errorf("composer approved pending question: status%d", w.Code)
			}
			if len(task.AnswerCh) != 0 || task.approvalAnswered || len(s.Messages) != 0 || len(task.ApprovalReviews) != 0 {
				t.Error("composer confirmation altered approval or message state")
			}
			after, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("rejected composer persisted session changes")
			}
		})
	}
}

func TestIssue108ApprovalSaveFailureNeverDeliversAnswer(t *testing.T) {
	a, s, task := issue108Pending(t, "shell", "confirm")
	file := SessionPath(a.dataPath, s.ID, "active")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", boundQuestionAnswerForTest(t, a, task, "确认"))
	requireStatus(t, w, 500)
	if len(task.AnswerCh) != 0 || task.approvalAnswered || len(task.ApprovalReviews) != 0 || len(s.Messages) != 0 {
		t.Fatal("failed receipt persistence released approval or kept partial state")
	}
}

func TestIssue108MalformedQuestionCannotBeAnswered(t *testing.T) {
	for _, endpoint := range []string{"composer", "answer"} {
		t.Run(endpoint, func(t *testing.T) {
			a, s, task := issue108Pending(t, "shell", "confirm")
			task.PendingQuestion = json.RawMessage(`"invalid-question-object"`)
			var status int
			if endpoint == "composer" {
				status = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "确认"}).Code
			} else {
				status = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "确认"}).Code
			}
			if status != 409 || len(task.AnswerCh) != 0 || len(s.Messages) != 0 {
				t.Fatal("malformed question admitted an answer")
			}
		})
	}
}

func TestIssue108ApprovalAnswerRequiresCurrentQuestionAndRound(t *testing.T) {
	for _, tc := range []struct{ name, id, round string }{{"missing-both", "", ""}, {"missing-id", "", "7"}, {"missing-round", "owned-current-question", ""}, {"stale-id", "old-question", "7"}, {"stale-round", "owned-current-question", "6"}} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, task := issue108Pending(t, "shell", "confirm")
			w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "确认", "questionId": tc.id, "round": tc.round})
			if w.Code != 409 {
				t.Errorf("unbound/stale approval returned%d", w.Code)
			}
			if len(task.AnswerCh) != 0 || task.approvalAnswered || len(s.Messages) != 0 || len(task.ApprovalReviews) != 0 {
				t.Error("unbound/stale approval was delivered or persisted")
			}
		})
	}
}

func TestIssue108BoundApprovalPersistsHumanReceiptBeforeDelivery(t *testing.T) {
	a, s, task := issue108Pending(t, "shell", "confirm")
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/answer", map[string]any{"answer": "确认", "questionId": "owned-current-question", "round": "7"})
	requireStatus(t, w, 200)
	if len(task.AnswerCh) != 1 || <-task.AnswerCh != "确认" {
		t.Fatal("current bound answer not delivered")
	}
	if len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Source != "human" || task.ApprovalReviews[0].Command != "mkdir owned-fixture-output" {
		t.Fatal("human command receipt missing")
	}
	b, err := os.ReadFile(SessionPath(a.dataPath, s.ID, "active"))
	if err != nil {
		t.Fatal(err)
	}
	var stored Session
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Runs) != 1 || len(stored.Runs[0].ApprovalReviews) != 1 {
		t.Fatal("answer was delivered without durable receipt")
	}
}

func TestIssue108ComposerStillAnswersOrdinaryClarification(t *testing.T) {
	a, s, task := issue108Pending(t, "", "input")
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "owned-blue"})
	requireStatus(t, w, 202)
	if len(task.AnswerCh) != 1 || <-task.AnswerCh != "owned-blue" {
		t.Fatal("ordinary clarification no longer receives composer input")
	}
	if len(task.ApprovalReviews) != 0 || len(s.Messages) != 1 {
		t.Fatalf("ordinary clarification side effects: reviews%d messages%d", len(task.ApprovalReviews), len(s.Messages))
	}
}

func boundQuestionAnswerForTest(t *testing.T, a *App, task *Task, answer string) map[string]string {
	t.Helper()
	a.mu.Lock()
	var current struct {
		ID    string `json:"questionId"`
		Round string `json:"approvalRound"`
	}
	err := json.Unmarshal(task.PendingQuestion, &current)
	a.mu.Unlock()
	if err != nil || current.ID == "" || current.Round == "" {
		t.Fatal("fixture has no current bound question", err)
	}
	return map[string]string{"answer": answer, "questionId": current.ID, "round": current.Round}
}
