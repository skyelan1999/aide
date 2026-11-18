package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunShellRequiresExactConfirmationForWrites(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		withPurpose  bool
		wantFile     bool
	}{
		{name: "confirmed", answer: "确认", withPurpose: true, wantFile: true},
		{name: "declined_with_purpose", answer: "拒绝", withPurpose: true},
		{name: "declined", answer: "拒绝", wantFile: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			task := &Task{ID: "shell-approval", Mode: "chat", Status: "running", WorkspaceMode: "workspace-write"}
			session := &Session{ID: "shell-session", Runs: []*Task{task}}
			a.mu.Lock()
			a.sessions[session.ID] = session
			a.mu.Unlock()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan string, 1)
			const command = "echo approved > shell-approval.txt"
			args := map[string]any{"command": command}
			wantPurpose := &approvalPurpose{Goal: "写一份审批回执", Reason: "验证已确认的操作才会落盘"}
			if tc.withPurpose {
				args[approvalPurposeField] = wantPurpose
			}
			argJSON, _ := json.Marshal(args)
			go func() {
				call := readCall("run_shell", string(argJSON))
				result <- a.executeToolCall(ctx, call, task, nil)
			}()

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				a.mu.Lock()
				status := task.Status
				question := append(json.RawMessage(nil), task.PendingQuestion...)
				a.mu.Unlock()
				if status == "awaiting_clarification" {
					var prompt struct {
						Question string           `json:"question"`
						Type     string           `json:"type"`
						Command  string           `json:"command"`
						Purpose  *approvalPurpose `json:"approvalPurpose"`
					}
					if err := json.Unmarshal(question, &prompt); err != nil {
						t.Fatal(err)
					}
					if prompt.Type != "confirm" || prompt.Command != command || !strings.Contains(prompt.Question, command) {
						t.Fatalf("confirmation prompt must show exact command: %+v", prompt)
					}
					if tc.withPurpose && (prompt.Purpose == nil || *prompt.Purpose != *wantPurpose) || !tc.withPurpose && prompt.Purpose != nil {
						t.Fatalf("unexpected pending purpose: %+v", prompt.Purpose)
					}
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			a.mu.Lock()
			status := task.Status
			a.mu.Unlock()
			if status != "awaiting_clarification" {
				t.Fatal("write command did not pause for user confirmation")
			}
			w := request(a, "POST", "/api/sessions/"+session.ID+"/runs/"+task.ID+"/answer", boundQuestionAnswerForTest(t, a, task, tc.answer))
			requireStatus(t, w, 200)
			select {
			case got := <-result:
				if tc.wantFile && !strings.Contains(got, "exit code: 0") {
					t.Fatalf("confirmed command failed: %s", got)
				}
				if !tc.wantFile && !strings.Contains(got, "没有运行") {
					t.Fatalf("declined command result: %s", got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shell tool did not resume after answer")
			}
			a.mu.Lock()
			receipt := task.ApprovalReviews[len(task.ApprovalReviews)-1]
			a.mu.Unlock()
			if receipt.Command != command || receipt.Fingerprint != approvalFingerprint(command) {
				t.Fatalf("purpose changed the approved operation: %+v", receipt)
			}
			if tc.withPurpose && (receipt.Purpose == nil || *receipt.Purpose != *wantPurpose) || !tc.withPurpose && receipt.Purpose != nil {
				t.Fatalf("unexpected durable purpose: %+v", receipt.Purpose)
			}
			_, err := os.Stat(filepath.Join(a.workPath, "shell-approval.txt"))
			if tc.wantFile && err != nil {
				t.Fatalf("confirmed command did not create file: %v", err)
			}
			if !tc.wantFile && !os.IsNotExist(err) {
				t.Fatalf("declined command changed workspace, stat err=%v", err)
			}
		})
	}
}

func TestReadOnlyAllowedRejectsShellEvaluation(t *testing.T) {
	for _, command := range []string{
		"cat $(touch injected.txt)",
		"ls; touch injected.txt",
		"cat `touch injected.txt`",
		"cat *.txt",
		"cat file.txt\ntouch injected.txt",
	} {
		if readOnlyAllowed(command) {
			t.Errorf("shell-evaluated command was treated as read-only: %q", command)
		}
	}
	for _, command := range []string{"pwd", "ls -la", "cat README.md", "echo hello"} {
		if !readOnlyAllowed(command) {
			t.Errorf("plain read-only command was unexpectedly blocked: %q", command)
		}
	}
}
