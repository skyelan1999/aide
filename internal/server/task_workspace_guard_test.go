package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue82StaleRemoteTaskCannotDispatchOrApprove(t *testing.T) {
	for _, kind := range []string{"skill", "shell", "answer"} {
		t.Run(kind, func(t *testing.T) {
			a, _, remote := issue79SFTPFixture(t, false)
			a.settings.SandboxMode = "danger-full-access"
			task := &Task{ID: "old-remote-task", WorkspaceID: "ssh|old.invalid|/old", WorkspaceRev: a.wsRevision, WorkspaceMode: "ssh", WorkspaceRemotePath: remote, HarnessConfig: &HarnessConfig{Skills: []HarnessSkill{{Name: "device", Path: "SKILL.md"}}}}
			if err := os.WriteFile(filepath.Join(remote, "SKILL.md"), []byte("replacement-host-private-skill"), 0600); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "dispatch")
			// New remote channel requests are observable, including SFTP and commands.
			oldBin := a.sshBin
			a.sshBin = filepath.Join(t.TempDir(), "observed-ssh")
			if err := os.WriteFile(a.sshBin, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\nexec "+shellQuote(oldBin)+" \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "skill":
				got := a.readHarnessSkill(task, "device")
				if !strings.Contains(got, "工作区已变化") {
					t.Errorf("stale task skill not rejected: %q", got)
				}
			case "shell":
				if _, _, err := a.execShellCommand(context.Background(), task, "echo must-not-run"); err == nil || !strings.Contains(err.Error(), "工作区已变化") {
					t.Errorf("stale task shell not rejected: %v", err)
				}
			case "answer":
				task.Status = "awaiting_clarification"
				task.AnswerCh = make(chan string, 1)
				task.PendingQuestion = json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"echo old"}`)
				a.sessions["old-session"] = &Session{ID: "old-session", Runs: []*Task{task}}
				out := request(a, "POST", "/api/sessions/old-session/runs/"+task.ID+"/answer", map[string]any{"answer": "确认"})
				if out.Code != 409 {
					t.Errorf("stale approval accepted: %d", out.Code)
				}
				if len(task.AnswerCh) != 0 || len(a.sessions["old-session"].Messages) != 0 {
					t.Error("stale approval changed run state")
				}
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("stale task touched replacement SSH endpoint", err)
			}
		})
	}
}

func TestIssue82MatchingRemoteTaskSkillAndApproval(t *testing.T) {
	a, _, remote := issue79SFTPFixture(t, false)
	if err := os.WriteFile(filepath.Join(remote, "SKILL.md"), []byte("owned original skill"), 0600); err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: "current-task", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: "ssh", WorkspaceRemotePath: remote, HarnessConfig: &HarnessConfig{Skills: []HarnessSkill{{Name: "device", Path: "SKILL.md"}}}}
	if got := a.readHarnessSkill(task, "device"); got != "owned original skill" {
		t.Fatal("matching skill binding rejected", got)
	}
	task.Status = "awaiting_clarification"
	task.AnswerCh = make(chan string, 1)
	task.answerRound = 1
	task.PendingQuestion = json.RawMessage(`{"type":"confirm","approvalKind":"shell","command":"echo owned","questionId":"owned-current-question","approvalRound":"1"}`)
	a.sessions["current-session"] = &Session{ID: "current-session", Runs: []*Task{task}}
	requireStatus(t, request(a, "POST", "/api/sessions/current-session/runs/"+task.ID+"/answer", boundQuestionAnswerForTest(t, a, task, "确认")), 200)
	if len(task.AnswerCh) != 1 {
		t.Fatal("matching task approval not delivered")
	}
}
