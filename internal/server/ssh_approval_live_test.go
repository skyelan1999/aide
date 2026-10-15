package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"testing"
	"time"
)

// Called only inside the opt-in, disposable dual-OpenSSH integration test.
// Answers use the authenticated product Handler; remote() is setup/readback,
// never the execution route under test. No model or user SSH host is involved.
func assertSSHApprovalLifecycle(t *testing.T, a *App, hostA, hostB, workspace string, remote func(*testing.T, string) string) {
	t.Helper()
	a.mu.Lock()
	original, sandbox := a.wsConfig, a.settings.SandboxMode
	a.settings.SandboxMode = "danger-full-access"
	a.mu.Unlock()
	configure := func(host, root string) {
		t.Helper()
		requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
			"workspace": map[string]any{"mode": "ssh", "host": host, "port": 22, "username": "fixture", "auth": "password", "path": root},
			"docs":      original.Docs, "cache": original.Cache, "password": "fixture-pass",
		}), http.StatusOK)
	}
	defer func() {
		requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
			"workspace": original.Workspace, "docs": original.Docs, "cache": original.Cache, "password": "fixture-pass",
		}), http.StatusOK)
		a.mu.Lock()
		a.settings.SandboxMode = sandbox
		a.mu.Unlock()
	}()
	configure(hostA, workspace)
	type execution struct {
		session string
		task    *Task
		result  <-chan bool
	}
	command := "mkdir approval-marker"
	start := func() execution {
		t.Helper()
		a.mu.Lock()
		task := &Task{ID: newID(), Status: "running", WorkspaceID: a.wsID(), WorkspaceMode: "ssh", WorkspaceRemotePath: a.wsConfig.Workspace.Path, AgentRoot: a.snapshotAgentRootLocked()}
		s := &Session{ID: newID(), Title: "owned SSH authorization fixture", Runs: []*Task{task}}
		a.sessions[s.ID] = s
		err := a.save(s)
		a.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, done := make(chan bool, 1), make(chan struct{})
		go func() {
			defer close(done)
			_, ok := a.approvedShell(ctx, task, command, 5*time.Second)
			result <- ok
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("SSH approval operation did not stop")
			}
		})
		return execution{s.ID, task, result}
	}
	question := func(run execution) map[string]any {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			w := request(a, "GET", "/api/sessions/"+run.session, nil)
			requireStatus(t, w, http.StatusOK)
			var view Session
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			if len(view.Runs) == 1 && view.Runs[0].Status == "awaiting_clarification" {
				var q map[string]any
				if err := json.Unmarshal(view.Runs[0].PendingQuestion, &q); err != nil {
					t.Fatal(err)
				}
				if q["command"] != command || q["questionId"] == "" {
					t.Fatalf("incorrect SSH approval question: %v", q)
				}
				return q
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("expected a fresh SSH command approval, not an inherited authorization")
		return nil
	}
	answer := func(run execution, q map[string]any, approve, remember bool, expiry string) {
		t.Helper()
		text := "拒绝"
		if approve {
			text = "确认"
		}
		requireStatus(t, request(a, "POST", "/api/sessions/"+run.session+"/runs/"+run.task.ID+"/answer", map[string]any{
			"answer": text, "remember": remember, "round": q["approvalRound"], "questionId": q["questionId"], "expiresAt": expiry,
		}), http.StatusOK)
	}
	result := func(run execution, want bool) {
		t.Helper()
		select {
		case ok := <-run.result:
			if ok != want {
				t.Fatalf("SSH execution success=%v, want %v", ok, want)
			}
		case <-time.After(7 * time.Second):
			t.Fatal("SSH command result timeout")
		}
	}
	marker := path.Join(workspace, "approval-marker")
	absent := func(p string) { t.Helper(); remote(t, "test ! -e "+shellQuote(p)) }
	remove := func() { t.Helper(); remote(t, "rmdir "+shellQuote(marker)) }
	type rulesView struct {
		Revision uint64 `json:"revision"`
		Rules    []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
		} `json:"rules"`
	}
	rules := func() rulesView {
		t.Helper()
		w := request(a, "GET", "/api/approval-policy/rules", nil)
		requireStatus(t, w, http.StatusOK)
		var v rulesView
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	absent(marker)
	first := start()
	q := question(first)
	absent(marker)
	answer(first, q, true, true, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	result(first, true)
	remote(t, "test -d "+shellQuote(marker))
	remove()
	second := start()
	result(second, true)
	remote(t, "test -d "+shellQuote(marker))
	w := request(a, "GET", "/api/sessions/"+second.session+"/runs/"+second.task.ID+"/outcome", nil)
	requireStatus(t, w, http.StatusOK)
	var outcome struct {
		Approvals []ApprovalReview `json:"approvals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil || len(outcome.Approvals) != 1 || outcome.Approvals[0].Source != "remembered" || outcome.Approvals[0].RuleID == "" {
		t.Fatalf("missing durable SSH remembered receipt: %s", w.Body.String())
	}
	remove()
	for _, mode := range []string{"read-only", "workspace-write"} {
		a.mu.Lock()
		a.settings.SandboxMode = mode
		a.mu.Unlock()
		result(start(), false)
		absent(marker)
	}
	a.mu.Lock()
	a.settings.SandboxMode = "danger-full-access"
	a.mu.Unlock()
	// The identical payload must prompt again on another real server and root.
	configure(hostB, "")
	other := workspace // Same absolute path on B isolates the host boundary.
	remote(t, "mkdir -p "+shellQuote(other))
	configure(hostB, other)
	foreign := start()
	answer(foreign, question(foreign), false, false, "")
	result(foreign, false)
	absent(path.Join(other, "approval-marker"))
	configure(hostA, workspace)
	child := path.Join(workspace, "another-root")
	remote(t, "mkdir -p "+shellQuote(child))
	configure(hostA, child)
	differentRoot := start()
	answer(differentRoot, question(differentRoot), false, false, "")
	result(differentRoot, false)
	absent(path.Join(child, "approval-marker"))
	configure(hostA, workspace)
	v := rules()
	if len(v.Rules) != 1 {
		t.Fatalf("unexpected remembered rules: %+v", v)
	}
	expires := time.Now().Add(2 * time.Second)
	requireStatus(t, request(a, "PUT", "/api/approval-policy/rules/"+v.Rules[0].ID, map[string]any{"revision": v.Revision, "expiresAt": expires.UTC().Format(time.RFC3339Nano)}), http.StatusOK)
	requireStatus(t, request(a, "DELETE", "/api/approval-policy/rules/"+v.Rules[0].ID, map[string]any{"revision": v.Revision}), http.StatusConflict)
	time.Sleep(time.Until(expires.Add(20 * time.Millisecond)))
	if rules().Rules[0].Active {
		t.Fatal("real-clock deadline did not expire the rule")
	}
	expired := start()
	answer(expired, question(expired), false, false, "")
	result(expired, false)
	absent(marker)
	renewed := start()
	answer(renewed, question(renewed), true, true, "")
	result(renewed, true)
	remove()
	v = rules()
	requireStatus(t, request(a, "DELETE", "/api/approval-policy/rules/"+v.Rules[0].ID, map[string]any{"revision": v.Revision}), http.StatusOK)
	a.mu.Lock()
	err := a.loadApprovalPolicy()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules().Rules) != 0 {
		t.Fatal("revocation did not survive policy reload")
	}
	revoked := start()
	answer(revoked, question(revoked), false, false, "")
	result(revoked, false)
	absent(marker)
}
