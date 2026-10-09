package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRememberedApprovalReceiptAndOutcome(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "receipt", Status: "running", WorkspaceID: a.wsID(), AgentRoot: AgentRoot{ContainerAbs: "/workspace", ID: a.wsID()}}
	session := &Session{ID: "receipt-session", Runs: []*Task{task}}
	a.sessions[session.ID] = session
	a.mu.Lock()
	err := a.rememberApprovalLocked(task, "shell", "mkdir reports")
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	matched, err := a.approveRememberedShell(task, "mkdir reports")
	if err != nil || !matched || len(task.ApprovalReviews) != 1 {
		t.Fatalf("match %v %v", matched, err)
	}
	record := task.ApprovalReviews[0]
	if record.Source != "remembered" || record.RuleID == "" || record.Workspace != task.WorkspaceID || record.Fingerprint != approvalFingerprint("mkdir reports") {
		t.Fatal(record)
	}
	w := request(a, "GET", "/api/sessions/receipt-session/runs/receipt/outcome", nil)
	requireStatus(t, w, 200)
	var outcome struct {
		Approvals []ApprovalReview `json:"approvals"`
		Note      string           `json:"approvalNote"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome.Approvals) != 1 || outcome.Approvals[0].RuleID != record.RuleID || outcome.Note == "" {
		t.Fatal(outcome)
	}
	snapshot, _, _, err := a.outcomeTaskSnapshot(session.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.ApprovalReviews[0].Reason = "changed"
	if snapshot.ApprovalReviews[0].Reason == "changed" {
		t.Fatal("approval snapshot aliases live task")
	}
	matched, err = a.approveRememberedShell(task, "mkdir different")
	if err != nil || matched || len(task.ApprovalReviews) != 1 {
		t.Fatal("nonmatching payload recorded")
	}
	oldData := a.dataPath
	badPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badPath, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a.dataPath = badPath
	matched, err = a.approveRememberedShell(task, "mkdir reports")
	if err == nil || matched || len(task.ApprovalReviews) != 1 {
		t.Fatal("failed persistence released command or changed history")
	}
	a.dataPath = oldData
	// A valid rule without a durable session receipt must not release an operation.
	delete(a.sessions, session.ID)
	matched, err = a.approveRememberedShell(task, "mkdir reports")
	if err == nil || matched {
		t.Fatal("missing session granted approval")
	}
}
