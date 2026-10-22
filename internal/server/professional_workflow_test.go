package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pwFixture(t *testing.T) (*App, *Session, pwDefinition) {
	a, s, _ := parallelFixture(t, 1)
	s.Runs = nil
	pwInstallTestRuntime(t, a)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "professional-workflow", Version: "0.4.0", Main: "index.js", Enabled: true, Workbench: &PluginWorkbench{ProfessionalWorkflow: true, Entry: "workbench.html"}})
	d := pwDefinition{Schema: 1, ID: "test-flow", Name: "Test", Nodes: []pwNode{{ID: "start", Type: "start"}, {ID: "text", Type: "template", Text: "Hello {{input}}"}, {ID: "end", Type: "end", Text: "{{text}}"}}, Edges: []pwEdge{{From: "start", To: "text"}, {From: "text", To: "end"}}}
	return a, s, d
}
func TestProfessionalWorkflowGraph(t *testing.T) {
	a, _, d := pwFixture(t)
	if err := a.validatePWRuntimeLocked(d); err != nil {
		t.Fatal(err)
	}
	d.Edges = append(d.Edges, pwEdge{From: "text", To: "start"})
	if a.validatePWRuntimeLocked(d) == nil {
		t.Fatal("cycle accepted")
	}
	d.Edges = d.Edges[:2]
	d.Nodes[1].Text = "{{end}}"
	if a.validatePWRuntimeLocked(d) == nil {
		t.Fatal("future reference accepted")
	}
	d.Nodes[1].Text = "{{input}}"
	d.Nodes = append(d.Nodes, pwNode{ID: "orphan", Type: "template"})
	if a.validatePWRuntimeLocked(d) == nil {
		t.Fatal("orphan accepted")
	}
}
func TestProfessionalWorkflowStoreAndBindings(t *testing.T) {
	a, s, d := pwFixture(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.pwControlLocked(map[string]any{"action": "save", "definition": d, "revision": float64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.pwControlLocked(map[string]any{"action": "save", "definition": d, "revision": float64(0)}); err == nil {
		t.Fatal("stale revision accepted")
	}
	_, err = a.pwControlLocked(map[string]any{"action": "bind", "scope": "session", "sessionId": s.ID, "templateId": d.ID, "revision": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.pwControlLocked(map[string]any{"action": "bind", "scope": "all", "templateId": d.ID, "revision": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	store := a.loadPWLocked()
	if store.Default != d.ID || len(store.Bindings) != 0 {
		t.Fatal("all did not clear overrides")
	}
	if _, err = os.Stat(filepath.Join(a.dataPath, "professional-workflow", a.wsID()+".json")); err != nil {
		t.Fatal(err)
	}
}
func TestProfessionalWorkflowRunSnapshotAndCancel(t *testing.T) {
	a, s, d := pwFixture(t)
	a.mu.Lock()
	_, err := a.pwControlLocked(map[string]any{"action": "save", "definition": d, "revision": float64(0)})
	if err == nil {
		_, err = a.pwControlLocked(map[string]any{"action": "bind", "scope": "default", "templateId": d.ID, "revision": float64(1)})
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"prompt": "PW", "mode": "pw"})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		r := s.Runs[len(s.Runs)-1]
		status, final := r.Status, r.PW.Result
		a.mu.Unlock()
		if status == "completed" {
			if final != "Hello PW" {
				t.Fatal(final)
			}
			return
		}
		if status == "failed" {
			t.Fatal("run failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run timeout")
}
func TestProfessionalWorkflowApprovalAndPermission(t *testing.T) {
	a, s, d := pwFixture(t)
	d.Nodes[1].Type = "approval"
	t1 := &Task{ID: "approvalrun", Mode: "pw", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{"start": {Status: "pending"}, "text": {Status: "pending"}, "end": {Status: "pending"}}, Decisions: map[string]bool{}}}
	a.mu.Lock()
	s.Runs = append(s.Runs, t1)
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.executePW(ctx, s, t1, a.settings); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		waiting := t1.PW.Nodes["text"].Status == "waiting"
		a.mu.Unlock()
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel hung")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if t1.Status != "interrupted" {
		t.Fatal(t1.Status)
	}
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	if _, err := a.pwControlLocked(map[string]any{"action": "status"}); err == nil {
		t.Fatal("disabled plugin still available")
	}
}

func TestProfessionalWorkflowManualOnlyBridge(t *testing.T) {
	a := workbenchFixture(t)
	if err := os.Rename(filepath.Join(a.pluginsPath, "workbench-fixture"), filepath.Join(a.pluginsPath, "professional-workflow")); err != nil {
		t.Fatal(err)
	}
	pwInstallTestRuntime(t, a)
	for i := range a.pluginRegistry.Plugins {
		p := &a.pluginRegistry.Plugins[i]
		if p.ID == "workbench-fixture" {
			p.ID = "professional-workflow"
			p.Main = "index.js"
			p.Version = "0.4.0"
			p.Workbench.Entry = "workbench.html"
			p.Workbench.ProfessionalWorkflow = true
		}
	}
	w := workbenchRequest(a, "POST", "/api/plugins/professional-workflow/workbench/open", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string   `json:"instanceId"`
			Tools    []string `json:"tools"`
			PW       bool     `json:"professionalWorkflow"`
		} `json:"bridge"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Bridge.Instance == "" || !out.Bridge.PW || len(out.Bridge.Tools) != 0 {
		t.Fatal("manual capability requires model tool exposure")
	}
	requireStatus(t, workbenchRequest(a, "GET", out.URL, false), 200)
	base := "/api/plugins/professional-workflow/workbench/" + out.Bridge.Instance + "/call"
	w = request(a, "POST", base, map[string]any{"protocol": 1, "requestId": "r_1", "method": "workflow.control", "arguments": map[string]any{"action": "status"}})
	requireStatus(t, w, 200)
	w = request(a, "POST", base, map[string]any{"protocol": 1, "requestId": "r_2", "method": "workflow.control", "arguments": map[string]any{"action": "approve"}})
	requireStatus(t, w, 403)
}

func TestProfessionalWorkflowModelNode(t *testing.T) {
	a, s, d := pwFixture(t)
	d.Nodes[1].Type = "llm"
	d.Nodes[1].Text = "Transform {{input}}"
	seen := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) != 0 || r.Header.Get("Authorization") != "Bearer router-key-a" || !strings.Contains(body.Messages[len(body.Messages)-1].Content, "fixture input") {
			t.Error("node request isolation failed")
		}
		seen = true
		parallelReply(w, "mock-node-result")
	}))
	defer provider.Close()
	cfg := routerSettings(t, a, provider.URL, provider.URL)
	task := &Task{ID: "model-node", Mode: "pw", Status: "running", Prompt: "fixture input", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{"start": {Status: "pending"}, "text": {Status: "pending"}, "end": {Status: "pending"}}}}
	s.Runs = append(s.Runs, task)
	a.executePW(context.Background(), s, task, cfg)
	if !seen || task.Status != "completed" || task.PW.Result != "mock-node-result" || task.Usage.Total != 20 {
		t.Fatalf("model node failed: %s %s %+v", task.Status, task.Error, task.Usage)
	}
}

func TestProfessionalWorkflowBuiltinTools(t *testing.T) {
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
		failure    bool
	}{
		{"read", "read_file", map[string]any{"path": "proof.txt"}, false},
		{"content resembling error", "read_file", map[string]any{"path": "error.txt"}, false},
		{"missing", "read_file", map[string]any{"path": "missing.txt"}, true},
		{"traversal", "read_file", map[string]any{"path": "../escape"}, true},
		{"write proposal", "write_file", map[string]any{"path": "new.txt", "content": "review me"}, false},
		{"invalid arguments", "run_shell", map[string]any{"command": true}, true},
		{"disabled", "read_file", map[string]any{"path": "proof.txt"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, d := pwFixture(t)
			for name, content := range map[string]string{"proof.txt": "local tool proof", "error.txt": "读取失败: this is actual file content"} {
				if err := os.WriteFile(filepath.Join(a.workspace.Name(), name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "disabled" {
				a.settings.DisabledTools = []string{"read_file"}
			}
			d.Nodes[1].Type = "tool"
			d.Nodes[1].Tool = tc.tool
			d.Nodes[1].Arguments = tc.args
			task := &Task{ID: "builtin-test", Mode: "pw", Status: "running", Prompt: "proof", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{"start": {Status: "pending"}, "text": {Status: "pending"}, "end": {Status: "pending"}}, Decisions: map[string]bool{"text": true}}}
			s.Runs = append(s.Runs, task)
			a.executePW(context.Background(), s, task, a.settings)
			if (task.Status == "failed") != tc.failure {
				t.Fatalf("%s: %s", task.Status, task.Error)
			}
			if tc.name == "read" && task.PW.Result != "local tool proof" {
				t.Fatal(task.PW.Result)
			}
			if tc.name == "content resembling error" && !strings.HasPrefix(task.PW.Result, "读取失败:") {
				t.Fatal(task.PW.Result)
			}
			if tc.name == "write proposal" {
				if task.Status != "awaiting_approval" {
					t.Fatal("proposal not reviewable", task.Status)
				}
				if len(task.Files) != 1 {
					t.Fatal("missing proposal", task.Error)
				}
				if _, err := os.Stat(filepath.Join(a.workspace.Name(), "new.txt")); !os.IsNotExist(err) {
					t.Fatal("wrote without original approval")
				}
			}
		})
	}
	_, _, d := pwFixture(t)
	d.Nodes[1].Type = "tool"
	d.Nodes[1].Tool = "spawn_subagent"
	if validatePW(d) == nil {
		t.Fatal("unsupported orchestration tool accepted")
	}
	for _, v := range pwBuiltinDescriptors([]string{"run_shell"}) {
		if v.(map[string]any)["name"] == "run_shell" {
			t.Fatal("disabled tool advertised")
		}
	}
}

func TestProfessionalWorkflowShellKeepsOriginalApproval(t *testing.T) {
	a, s, d := pwFixture(t)
	d.Nodes[1].Type = "tool"
	d.Nodes[1].Tool = "run_shell"
	d.Nodes[1].Arguments = map[string]any{"command": "touch pw-must-not-exist"}
	task := &Task{ID: "shell-approval", Mode: "pw", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{"start": {Status: "pending"}, "text": {Status: "pending"}, "end": {Status: "pending"}}, Decisions: map[string]bool{"text": true}}}
	s.Runs = append(s.Runs, task)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.executePW(ctx, s, task, a.settings); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		a.mu.Lock()
		if task.Status == "awaiting_clarification" && task.AnswerCh != nil {
			waiting = true
			task.AnswerCh <- "拒绝"
			a.mu.Unlock()
			break
		}
		a.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		cancel()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shell cancellation hung")
	}
	if !waiting {
		t.Fatalf("original shell approval not reached: %s", task.Error)
	}
	if task.Status != "failed" || !strings.Contains(task.Error, "未批准") {
		t.Fatal(task.Status, task.Error)
	}
	if _, err := os.Stat(filepath.Join(a.workspace.Name(), "pw-must-not-exist")); !os.IsNotExist(err) {
		t.Fatal("shell ran after rejection")
	}
}

func TestProfessionalWorkflowExclusiveCapability(t *testing.T) {
	a, key := bridgeFixture(t)
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
			a.pluginRegistry.Plugins[i].Workbench.ProfessionalWorkflow = true
		}
	}
	w := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": "r_forged", "method": "workflow.control", "arguments": map[string]any{"action": "status"}})
	requireStatus(t, w, 403)
	a.mu.Lock()
	available := a.pwAvailableLocked()
	a.mu.Unlock()
	if available {
		t.Fatal("foreign plugin activated PW")
	}
	valid := `{"id":"fake-pw","name":"Fake","main":"index.js","workbench":{"protocol":1,"entry":"ui.html","title":"Tools","professionalWorkflow":true}}`
	raw := upgradePackage(t, "fake-pw", upgradeOldCode, map[string]string{"manifest.json": valid, "ui.html": "<!doctype html>hello"})
	if _, _, err := parsePluginBundle(raw); err == nil {
		t.Fatal("foreign reserved manifest accepted")
	}
}

// Integration fixtures are real signed commercial artifacts mounted explicitly
// by local QA. Missing fixtures do not bypass production signature verification.
func pwInstallTestRuntime(t *testing.T, a *App) {
	t.Helper()
	source := os.Getenv("PW_TEST_RUNTIME_DIR")
	if source == "" {
		t.Skip("explicit signed PW runtime fixture required")
	}
	for _, name := range []string{"manifest.json", "index.js", "workbench.html", "runtime.json", "runtime.sig", "bin/linux-arm64/pw-engine", "bin/linux-amd64/pw-engine"} {
		from, err := os.Open(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(a.pluginsPath, "professional-workflow", name)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			from.Close()
			t.Fatal(err)
		}
		to, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0500)
		if err != nil {
			from.Close()
			t.Fatal(err)
		}
		_, err = io.Copy(to, from)
		from.Close()
		to.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadPWRuntime(filepath.Join(a.pluginsPath, "professional-workflow")); err != nil {
		t.Fatal(err)
	}
}

func TestProfessionalWorkflowControlsRequireCurrentWorkspace(t *testing.T) {
	for _, action := range []string{"approve", "cancel"} {
		for _, changed := range []string{"identity", "revision"} {
			t.Run(action+"-"+changed, func(t *testing.T) {
				a, s, d := pwFixture(t)
				cancelled := false
				task := &Task{ID: "isolated-control", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "waiting"}}, Decisions: map[string]bool{}}}
				s.Runs = append(s.Runs, task)
				a.cancels[task.ID] = func() { cancelled = true }
				if changed == "identity" {
					task.WorkspaceID = "other-workspace"
				} else {
					task.WorkspaceRev++
				}
				args := map[string]any{"action": action, "sessionId": s.ID, "runId": task.ID, "nodeId": "text", "approved": true}
				if _, err := a.pwControlLocked(args); err == nil {
					t.Fatal("stale workspace control accepted")
				}
				if cancelled || len(task.PW.Decisions) != 0 {
					t.Fatal("stale workspace control changed task")
				}
				task.WorkspaceID = a.wsID()
				task.WorkspaceRev = a.wsRevision
				if _, err := a.pwControlLocked(args); err != nil {
					t.Fatal(err)
				}
				if action == "cancel" && !cancelled {
					t.Fatal("valid cancellation not invoked")
				}
				if action == "approve" && !task.PW.Decisions["text"] {
					t.Fatal("valid approval not recorded")
				}
			})
		}
	}
}
func TestProfessionalWorkflowBuiltinHonorsCurrentDisabledTools(t *testing.T) {
	a, s, d := pwFixture(t)
	d.Nodes[1] = pwNode{ID: "text", Type: "tool", Tool: "read_file", Arguments: map[string]any{"path": "proof.txt"}}
	os.WriteFile(filepath.Join(a.workspace.Name(), "proof.txt"), []byte("not-to-be-read"), 0600)
	a.settings.DisabledTools = append(a.settings.DisabledTools, "read_file")
	task := &Task{ID: "disabled-tool", Mode: "pw", Prompt: "fixture", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{}, Decisions: map[string]bool{"text": true}}}
	for _, n := range d.Nodes {
		task.PW.Nodes[n.ID] = &pwNodeResult{Status: "pending"}
	}
	s.Runs = append(s.Runs, task)
	a.executePW(context.Background(), s, task, a.settings)
	if task.Status != "failed" || !strings.Contains(task.Error, "禁用") {
		t.Fatalf("disabled tool not rejected: %+v", task)
	}
	if strings.Contains(task.PW.Nodes["text"].Output, "not-to-be-read") {
		t.Fatal("disabled read_file returned content")
	}
}
