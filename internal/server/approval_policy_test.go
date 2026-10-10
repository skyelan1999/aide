package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestGlobalApprovalModeManualStopsReviewAcrossSessions(t *testing.T) {
	a := testApp(t)
	a.approvalPolicy = ApprovalPolicy{
		Enabled:  true,
		Revision: 12,
		Rules:    []ApprovalRule{{Kind: "shell", Workspace: a.wsID(), Fingerprint: approvalFingerprint("mkdir reports"), At: "2026-10-10T00:00:00Z"}},
	}
	policyEvents := make(chan string, 1)
	a.globalSubs[policyEvents] = struct{}{}

	var tasks []*Task
	var cancelChecks []<-chan struct{}
	for _, sessionID := range []string{"manual-switch-a", "manual-switch-b"} {
		ctx, cancel := context.WithCancel(context.Background())
		task := &Task{
			ID:                 sessionID + "-run",
			Status:             "running",
			AutoReview:         true,
			ApprovalState:      "reviewing",
			WorkspaceID:        a.wsID(),
			approvalGeneration: 4,
			approvalCancel:     cancel,
		}
		a.sessions[sessionID] = &Session{ID: sessionID, Runs: []*Task{task}}
		tasks = append(tasks, task)
		cancelChecks = append(cancelChecks, ctx.Done())
	}

	w := request(a, "PUT", "/api/approval-policy", map[string]bool{"enabled": false})
	requireStatus(t, w, 200)
	for i, task := range tasks {
		if task.AutoReview || task.ApprovalState != "manual" || task.approvalGeneration != 5 {
			t.Fatalf("session %d task did not switch to manual: %+v", i, task)
		}
		select {
		case <-cancelChecks[i]:
		default:
			t.Fatalf("session %d in-flight review was not cancelled", i)
		}
		stored, err := os.ReadFile(SessionPath(a.dataPath, []string{"manual-switch-a", "manual-switch-b"}[i], "active"))
		if err != nil {
			t.Fatal(err)
		}
		var session Session
		if err := json.Unmarshal(stored, &session); err != nil {
			t.Fatal(err)
		}
		if len(session.Runs) != 1 || session.Runs[0].AutoReview || session.Runs[0].ApprovalState != "manual" {
			t.Fatalf("session %d manual mode was not persisted: %+v", i, session.Runs)
		}
	}
	if a.approvalPolicy.Enabled || a.approvalPolicy.Revision != 13 || len(a.approvalPolicy.Rules) != 1 {
		t.Fatalf("global mode change lost policy state: %+v", a.approvalPolicy)
	}
	var response struct {
		Enabled         bool   `json:"enabled"`
		Revision        uint64 `json:"revision"`
		RememberedRules int    `json:"rememberedRules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Enabled || response.Revision != 13 || response.RememberedRules != 1 {
		t.Fatalf("unexpected global mode response: %+v", response)
	}
	select {
	case event := <-policyEvents:
		if event != "approval:" {
			t.Fatalf("unexpected cross-tab policy event %q", event)
		}
	default:
		t.Fatal("global approval change did not notify other tabs")
	}
}

func TestRememberedApprovalDoesNotCrossSSHRoot(t *testing.T) {
	a := testApp(t)
	task := &Task{
		WorkspaceID: a.wsID(),
		AgentRoot: AgentRoot{
			DisplayHost:  "builder-a:/srv/project",
			ContainerAbs: "/workspace",
			ID:           a.wsID(),
		},
	}
	a.mu.Lock()
	err := a.rememberApprovalLocked(task, "shell", "mkdir reports")
	if err == nil {
		task.AgentRoot.DisplayHost = "builder-b:/srv/project"
	}
	matchedOnOtherHost := err == nil && a.rememberedApprovalLocked(task, "shell", "mkdir reports")
	task.AgentRoot.DisplayHost = "builder-a:/srv/project"
	task.WorkspaceID = "another-workspace"
	matchedInOtherWorkspace := err == nil && a.rememberedApprovalLocked(task, "shell", "mkdir reports")
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if matchedOnOtherHost || matchedInOtherWorkspace {
		t.Fatalf("remembered SSH approval crossed identity boundary: host=%v workspace=%v", matchedOnOtherHost, matchedInOtherWorkspace)
	}
}

func TestGlobalApprovalModeAssistedResumesAcrossSessions(t *testing.T) {
	a := testApp(t)
	a.approvalPolicy = ApprovalPolicy{Enabled: false, Revision: 20}
	policyEvents := make(chan string, 1)
	a.globalSubs[policyEvents] = struct{}{}

	for _, sessionID := range []string{"assisted-switch-a", "assisted-switch-b"} {
		task := &Task{
			ID:                 sessionID + "-run",
			Status:             "paused",
			AutoReview:         false,
			ApprovalState:      "manual",
			WorkspaceID:        a.wsID(),
			approvalGeneration: 8,
		}
		a.sessions[sessionID] = &Session{ID: sessionID, Runs: []*Task{task}}
	}

	w := request(a, "PUT", "/api/approval-policy", map[string]bool{"enabled": true})
	requireStatus(t, w, 200)
	for _, sessionID := range []string{"assisted-switch-a", "assisted-switch-b"} {
		task := a.sessions[sessionID].Runs[0]
		if !task.AutoReview || task.ApprovalState != "reviewing" || task.approvalGeneration != 9 {
			t.Fatalf("session %s did not receive the global assisted mode: %+v", sessionID, task)
		}
		stored, err := os.ReadFile(SessionPath(a.dataPath, sessionID, "active"))
		if err != nil {
			t.Fatal(err)
		}
		var session Session
		if err := json.Unmarshal(stored, &session); err != nil {
			t.Fatal(err)
		}
		if len(session.Runs) != 1 || !session.Runs[0].AutoReview || session.Runs[0].ApprovalState != "reviewing" {
			t.Fatalf("session %s assisted mode was not persisted: %+v", sessionID, session.Runs)
		}
	}
	if !a.approvalPolicy.Enabled || a.approvalPolicy.Revision != 21 {
		t.Fatalf("global assisted mode was not persisted: %+v", a.approvalPolicy)
	}
	select {
	case event := <-policyEvents:
		if event != "approval:" {
			t.Fatalf("unexpected cross-tab policy event %q", event)
		}
	default:
		t.Fatal("global assisted mode did not notify other tabs")
	}
}
