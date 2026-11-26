package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionApprovalModeRequiresBoundSingleUseGrant(t *testing.T) {
	a := testApp(t)
	a.sessions["one"] = &Session{ID: "one"}
	a.sessions["two"] = &Session{ID: "two"}
	a.approvalPolicy.Enabled = false
	a.settings.UserPasswordHash = mustHashPassword("approval-test-password")

	w := request(a, "PUT", "/api/sessions/one/approval-mode", map[string]any{"mode": "full_auto", "riskConfirmation": sessionApprovalRiskPhrase})
	requireStatus(t, w, 403)
	if a.sessions["one"].ApprovalMode != "" {
		t.Fatal("elevated mode changed without a grant")
	}

	wrong := request(a, "POST", "/api/auth/verify", map[string]any{"password": "wrong-password", "approvalSessionId": "one", "approvalMode": "full_auto"})
	requireStatus(t, wrong, 401)
	auth := request(a, "POST", "/api/auth/verify", map[string]any{"password": "approval-test-password", "approvalSessionId": "one", "approvalMode": "full_auto"})
	requireStatus(t, auth, 200)
	var verified struct {
		Grant string `json:"approvalGrant"`
	}
	if err := json.Unmarshal(auth.Body.Bytes(), &verified); err != nil || verified.Grant == "" {
		t.Fatalf("missing approval grant: %s (%v)", auth.Body.String(), err)
	}
	cross := request(a, "PUT", "/api/sessions/two/approval-mode", map[string]any{"mode": "full_auto", "grant": verified.Grant, "riskConfirmation": sessionApprovalRiskPhrase})
	requireStatus(t, cross, 403)

	grant, err := a.issueSessionApprovalGrant("one", "full_auto", "password")
	if err != nil {
		t.Fatal(err)
	}
	w = request(a, "PUT", "/api/sessions/one/approval-mode", map[string]any{"mode": "full_auto", "grant": grant, "riskConfirmation": sessionApprovalRiskPhrase})
	requireStatus(t, w, 200)
	if a.sessions["one"].ApprovalMode != sessionApprovalFullAuto || a.sessions["two"].ApprovalMode != "" || a.approvalPolicy.Enabled {
		t.Fatalf("session isolation or global policy changed: one=%q two=%q global=%v", a.sessions["one"].ApprovalMode, a.sessions["two"].ApprovalMode, a.approvalPolicy.Enabled)
	}
	replay := request(a, "PUT", "/api/sessions/one/approval-mode", map[string]any{"mode": "full_auto", "grant": grant, "riskConfirmation": sessionApprovalRiskPhrase})
	requireStatus(t, replay, 403)

	w = request(a, "GET", "/api/approval-audit/verify", nil)
	requireStatus(t, w, 200)
	logBytes, err := os.ReadFile(filepath.Join(a.dataPath, "approval-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), "approval-test-password") || strings.Contains(string(logBytes), "wrong-password") {
		t.Fatal("password material leaked into approval audit")
	}
}

func TestSessionApprovalRejectsPasswordlessMasterAuth(t *testing.T) {
	a := testApp(t)
	a.sessions["one"] = &Session{ID: "one"}
	a.settings.UserPasswordHash = ""
	w := request(a, "POST", "/api/auth/verify", map[string]any{"password": "anything", "approvalSessionId": "one", "approvalMode": "full_auto"})
	requireStatus(t, w, 403)
	if strings.Contains(w.Body.String(), "approvalGrant") || a.sessions["one"].ApprovalMode != "" {
		t.Fatalf("passwordless installation issued a high-risk approval grant: %s", w.Body.String())
	}
	if _, err := a.issueSessionApprovalGrant("one", "full_auto", "passwordless"); err == nil {
		t.Fatal("direct grant issuance accepted passwordless authentication")
	}
	// The existing passwordless behavior remains available outside elevated approvals.
	ordinary := request(a, "POST", "/api/auth/verify", map[string]any{"password": "anything"})
	requireStatus(t, ordinary, 200)
}

func TestSessionFullAutoPersistsReceiptBeforeConfirmation(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "run-full", Status: "awaiting_clarification", PendingQuestion: json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"printf safe"}`), AnswerCh: make(chan string, 1), WorkspaceID: a.wsID()}
	session := &Session{ID: "full", ApprovalMode: sessionApprovalFullAuto, Runs: []*Task{task}}
	a.sessions[session.ID] = session
	a.autoAnswerFullApproval(task)
	select {
	case answer := <-task.AnswerCh:
		if answer != "确认" {
			t.Fatalf("unexpected automatic answer %q", answer)
		}
	default:
		t.Fatal("full-auto approval was not answered")
	}
	if !task.approvalAnswered || len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Source != "session_full_auto" {
		t.Fatalf("missing full-auto receipt: %+v", task.ApprovalReviews)
	}
	stored, err := os.ReadFile(SessionPath(a.dataPath, session.ID, "active"))
	if err != nil || !strings.Contains(string(stored), "session_full_auto") {
		t.Fatalf("receipt was not durable before release: %v %s", err, stored)
	}
	if _, _, err := a.approvalAudit.verify(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionFullAutoDoesNotAffectOtherSession(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "run-manual", Status: "awaiting_clarification", PendingQuestion: json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"printf manual"}`), AnswerCh: make(chan string, 1), WorkspaceID: a.wsID()}
	a.sessions["manual"] = &Session{ID: "manual", Runs: []*Task{task}}
	a.sessions["full"] = &Session{ID: "full", ApprovalMode: sessionApprovalFullAuto}
	a.autoAnswerFullApproval(task)
	if len(task.AnswerCh) != 0 || task.approvalAnswered || len(task.ApprovalReviews) != 0 {
		t.Fatal("full-auto mode leaked into another session")
	}
}

func TestSessionAssistedAutoDeclinesWithoutHuman(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "run-assisted", Status: "awaiting_clarification", AutoReview: true, ApprovalState: "manual", PendingQuestion: json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"unknown-operation"}`), AnswerCh: make(chan string, 1), WorkspaceID: a.wsID(), ApprovalReviews: []ApprovalReview{{Status: "manual", Reason: "审核证据不足"}}}
	task.answerRound, task.approvalGeneration = 2, 3
	session := &Session{ID: "assisted", ApprovalMode: sessionApprovalAssistedAuto, Runs: []*Task{task}}
	a.sessions[session.ID] = session
	a.rejectAssistedAutoApproval(task, 2, 3)
	select {
	case answer := <-task.AnswerCh:
		if answer != "拒绝" {
			t.Fatalf("unexpected assisted-auto answer %q", answer)
		}
	default:
		t.Fatal("unapproved assisted-auto operation fell back to human review")
	}
	if task.ApprovalState != "declined" || task.ApprovalReviews[0].Status != "declined" {
		t.Fatalf("missing automatic rejection receipt: %+v", task)
	}
}

func TestSessionFullAutoFilesUseNormalApplyPath(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "run-files", Status: "awaiting_approval", WorkspaceID: a.wsID(), Files: []Change{{Path: "auto-approval.md", Content: "verified"}}}
	session := &Session{ID: "files", ApprovalMode: sessionApprovalFullAuto, Runs: []*Task{task}}
	a.sessions[session.ID] = session
	a.autoApplySessionFiles(task)
	if !task.Applied || task.Status != "completed" {
		t.Fatalf("full-auto file proposal was not applied: status=%q error=%q", task.Status, task.Error)
	}
	content, err := os.ReadFile(filepath.Join(a.workPath, "auto-approval.md"))
	if err != nil || string(content) != "verified" {
		t.Fatalf("unexpected applied content %q: %v", content, err)
	}
	if len(task.ApprovalReviews) == 0 || task.ApprovalReviews[len(task.ApprovalReviews)-1].Source != "session_full_auto" {
		t.Fatalf("missing file approval receipt: %+v", task.ApprovalReviews)
	}
}

func TestApprovalAuditRestartAndTamperDetection(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		root := t.TempDir()
		log, err := openApprovalAudit(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := log.append(approvalAuditEntry{Action: "restart-check"}); err != nil {
			t.Fatal(err)
		}
		reopened, err := openApprovalAudit(root)
		if err != nil {
			t.Fatal(err)
		}
		if entries, _, err := reopened.verify(); err != nil || entries != 1 {
			t.Fatalf("restart verification failed: entries=%d err=%v", entries, err)
		}
	})
	for _, mutation := range []string{"edit", "truncate", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			log, err := openApprovalAudit(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := log.append(approvalAuditEntry{Action: "original", Detail: "untampered"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "approval-audit.jsonl")
			switch mutation {
			case "edit":
				raw, _ := os.ReadFile(path)
				raw = []byte(strings.Replace(string(raw), "original", "modified", 1))
				_ = os.WriteFile(path, raw, 0600)
			case "truncate":
				_ = os.Truncate(path, 0)
			case "delete":
				_ = os.Remove(path)
			}
			if _, err := openApprovalAudit(root); err == nil {
				t.Fatalf("%s was not detected", mutation)
			}
		})
	}
}
