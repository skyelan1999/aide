package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestApprovalRememberExpiryFromAnswer(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "expiry", WorkspaceID: a.wsID(), AgentRoot: AgentRoot{ContainerAbs: "/workspace", ID: a.wsID()}, Status: "awaiting_clarification", AnswerCh: make(chan string, 1), answerRound: 1, PendingQuestion: json.RawMessage(`{"approvalKind":"shell","command":"mkdir reports","questionId":"expiry-question"}`)}
	a.sessions["expiry-session"] = &Session{ID: "expiry-session", Runs: []*Task{task}}
	endpoint := "/api/sessions/expiry-session/runs/expiry/answer"
	body := map[string]any{"answer": "确认", "remember": true, "round": "1", "questionId": "expiry-question", "expiresAt": "invalid"}
	w := request(a, "POST", endpoint, body)
	requireStatus(t, w, 400)
	if len(task.AnswerCh) != 0 || len(a.approvalPolicy.Rules) != 0 {
		t.Fatal("invalid deadline consumed answer or grant")
	}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	body["expiresAt"] = deadline
	w = request(a, "POST", endpoint, body)
	requireStatus(t, w, 200)
	if len(a.approvalPolicy.Rules) != 1 || a.approvalPolicy.Rules[0].ExpiresAt != deadline || len(task.AnswerCh) != 1 {
		t.Fatal("expiry not saved with explicit answer")
	}
	if len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Source != "human" || task.ApprovalReviews[0].ExpiresAt != deadline {
		t.Fatal("human receipt missing")
	}
	w = request(a, "POST", endpoint, body)
	requireStatus(t, w, 409)
}

func TestApprovalRuleManagement(t *testing.T) {
	a := testApp(t)
	task := &Task{WorkspaceID: a.wsID(), AgentRoot: AgentRoot{ContainerAbs: "/workspace", ID: a.wsID()}}
	a.mu.Lock()
	if err := a.rememberApprovalLocked(task, "shell", "mkdir reports"); err != nil {
		t.Fatal(err)
	}
	rule := a.approvalPolicy.Rules[0]
	revision := a.approvalPolicy.Revision
	a.mu.Unlock()
	id := approvalRuleID(rule)
	w := request(a, "GET", "/api/approval-policy/rules", nil)
	requireStatus(t, w, 200)
	var data struct {
		Rules []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || len(data.Rules) != 1 || data.Rules[0].ID != id || !data.Rules[0].Active {
		t.Fatalf("list: %s", w.Body.String())
	}
	expiry := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	w = request(a, "PUT", "/api/approval-policy/rules/"+id, map[string]any{"revision": revision, "expiresAt": expiry})
	requireStatus(t, w, 200)
	w = request(a, "DELETE", "/api/approval-policy/rules/"+id, map[string]any{"revision": revision})
	requireStatus(t, w, 409)
	a.mu.Lock()
	current := a.approvalPolicy.Rules[0]
	if current.ExpiresAt != expiry || approvalRuleID(current) != id || !a.rememberedApprovalLocked(task, "shell", "mkdir reports") {
		t.Fatal("expiry changed scope or matching")
	}
	if approvalRuleActive(current, time.Now().Add(2*time.Hour)) {
		t.Fatal("expired still active")
	}
	current.ExpiresAt = "invalid"
	if approvalRuleActive(current, time.Now()) {
		t.Fatal("invalid expiry active")
	}
	a.approvalPolicy.Rules[0].ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	if a.rememberedApprovalLocked(task, "shell", "mkdir reports") {
		t.Fatal("expired remembered match")
	}
	if err := a.rememberApprovalLocked(task, "shell", "mkdir reports"); err != nil {
		t.Fatal(err)
	}
	if len(a.approvalPolicy.Rules) != 1 || !a.rememberedApprovalLocked(task, "shell", "mkdir reports") {
		t.Fatal("explicit reapproval failed")
	}
	current = a.approvalPolicy.Rules[0]
	revision = a.approvalPolicy.Revision
	a.mu.Unlock()
	w = request(a, "PUT", "/api/approval-policy/rules/"+approvalRuleID(current), map[string]any{"revision": revision, "expiresAt": "yesterday"})
	requireStatus(t, w, 400)
	w = request(a, "DELETE", "/api/approval-policy/rules/"+approvalRuleID(current), map[string]any{"revision": revision})
	requireStatus(t, w, 200)
	if a.rememberedShellApproval(task, "mkdir reports") {
		t.Fatal("revocation ineffective")
	}
	if err := a.loadApprovalPolicy(); err != nil {
		t.Fatal(err)
	}
	if len(a.approvalPolicy.Rules) != 0 {
		t.Fatal("revocation not persisted")
	}
}

func TestConcurrentApprovalExpiryAndRevocationUseRevisionGuard(t *testing.T) {
	a := testApp(t)
	task := &Task{WorkspaceID: a.wsID(), AgentRoot: AgentRoot{ContainerAbs: "/workspace", ID: a.wsID()}}
	a.mu.Lock()
	if err := a.rememberApprovalLocked(task, "shell", "mkdir reports"); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	rule := a.approvalPolicy.Rules[0]
	revision := a.approvalPolicy.Revision
	a.mu.Unlock()
	id := approvalRuleID(rule)
	expiry := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	start := make(chan struct{})
	type result struct {
		method string
		status int
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, method := range []string{"PUT", "DELETE"} {
		method := method
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := map[string]any{"revision": revision}
			if method == "PUT" {
				body["expiresAt"] = expiry
			}
			w := request(a, method, "/api/approval-policy/rules/"+id, body)
			results <- result{method: method, status: w.Code}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for got := range results {
		if got.status == 200 {
			successes++
		} else if got.status == 409 {
			conflicts++
		} else {
			t.Fatalf("concurrent %s returned %d", got.method, got.status)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one winning mutation and one stale-revision conflict, got success=%d conflict=%d", successes, conflicts)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch len(a.approvalPolicy.Rules) {
	case 0: // Revocation won; expiry edit must not re-create the permission.
	case 1: // Expiry edit won; stale revocation cannot remove a newer revision.
		if a.approvalPolicy.Rules[0].ExpiresAt != expiry {
			t.Fatalf("expiry update lost after concurrent mutation: %+v", a.approvalPolicy.Rules[0])
		}
	default:
		t.Fatalf("unexpected concurrent rule state: %+v", a.approvalPolicy.Rules)
	}
}
