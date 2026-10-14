package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxCommandPanelEnforcement(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			a.settings.SandboxMode = mode
			w := request(a, "POST", "/api/command", map[string]any{"command": "echo marker > panel.txt"})
			if mode == "read-only" {
				requireStatus(t, w, 403)
			} else {
				requireStatus(t, w, 200)
				if !strings.Contains(w.Body.String(), `"code":0`) {
					t.Fatal(w.Body.String())
				}
			}
			_, err := os.Stat(filepath.Join(a.workPath, "panel.txt"))
			if (err == nil) != (mode != "read-only") {
				t.Fatal("command-panel write did not respect mode")
			}
		})
	}
}
func TestSandboxReadonlyPrecedesApprovalAndProposalApply(t *testing.T) {
	a := testApp(t)
	a.settings.SandboxMode = "read-only"
	task := &Task{ID: "readonly-task", Status: "running", AutoReview: true}
	out, ok := a.approvedShell(context.Background(), task, "mkdir forbidden", 0)
	if ok || !strings.Contains(out, "read-only") || task.PendingQuestion != nil {
		t.Fatal("read-only write entered approval or ran")
	}
	task.Status = "awaiting_approval"
	task.Files = []Change{{Path: "forbidden.txt", Content: "x"}}
	s := &Session{ID: "readonly-owner", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), 403)
	if _, err := os.Stat(filepath.Join(a.workPath, "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("readonly proposal wrote data")
	}
}
func TestSandboxModesAndRemoteBoundary(t *testing.T) {
	if shellSandboxPolicy("read-only", "echo x > file", false, false) == nil {
		t.Fatal("readonly write allowed")
	}
	if shellSandboxPolicy("workspace-write", "sudo true", false, false) == nil {
		t.Fatal("workspace dangerous command allowed")
	}
	if shellSandboxPolicy("danger-full-access", "sudo true", false, false) != nil {
		t.Fatal("full access still used workspace blacklist")
	}
	if shellSandboxPolicy("workspace-write", "pwd", true, false) == nil {
		t.Fatal("SSH claimed local kernel enforcement")
	}
	if shellSandboxPolicy("workspace-write", "pwd", false, true) == nil {
		t.Fatal("interactive shell bypassed restricted policy")
	}
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"sandboxMode": "invalid-mode"}), 400)
	w := request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"approvalIndependent":true`) {
		t.Fatal("missing enforcement scope")
	}
}

func TestSandboxReadonlyWorkspaceTools(t *testing.T) {
	a := testApp(t)
	a.settings.SandboxMode = "read-only"
	task := &Task{ID: "read-tools", Status: "running"}
	for _, name := range []string{"create_diagram", "create_requirement", "docx_add_comment", "office_create", "archive_extract"} {
		out := a.executeToolCall(context.Background(), readCall(name, `{}`), task, nil)
		if !strings.Contains(out, "read-only") {
			t.Fatalf("workspace-writing tool escaped read-only: %s %s", name, out)
		}
	}
}

func TestSandboxConfigurationPersistsAndAffectsNextCommand(t *testing.T) {
	a := testApp(t)
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"sandboxMode": mode, "activeModel": ""}), 200)
		data, err := os.ReadFile(SettingsPath(a.dataPath))
		if err != nil {
			t.Fatal(err)
		}
		var saved Settings
		if json.Unmarshal(data, &saved) != nil || saved.SandboxMode != mode {
			t.Fatal("sandbox preference did not persist")
		}
		w := request(a, "POST", "/api/command", map[string]any{"command": "echo marker > changed.txt"})
		expected := 200
		if mode == "read-only" {
			expected = 403
		}
		requireStatus(t, w, expected)
	}
}
