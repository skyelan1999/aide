package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfessionalWorkflowBranchContracts(t *testing.T) {
	validator, _, d := pwFixture(t)
	d.Nodes = []pwNode{{ID: "start", Type: "start"}, {ID: "choice", Type: "switch", Selector: "{{input}}", Cases: []pwCase{{ID: "a", Value: "A"}, {ID: "b", Value: "B"}}}, {ID: "a", Type: "end", Text: "A branch"}, {ID: "b", Type: "end", Text: "B branch"}, {ID: "fallback", Type: "end", Text: "fallback"}}
	d.Edges = []pwEdge{{From: "start", To: "choice"}, {From: "choice", To: "a", Port: "a"}, {From: "choice", To: "b", Port: "b"}, {From: "choice", To: "fallback", Port: "default"}}
	if err := validator.validatePWRuntimeLocked(d); err != nil {
		t.Fatal(err)
	}
	bad := d
	bad.Edges = append([]pwEdge(nil), d.Edges[:3]...)
	if validator.validatePWRuntimeLocked(bad) == nil {
		t.Fatal("missing default accepted")
	}
	for _, input := range []string{"A", "B", "other"} {
		a, s, _ := pwFixture(t)
		task := &Task{ID: "branch-run", Mode: "pw", Status: "running", Prompt: input, WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Nodes: map[string]*pwNodeResult{}}}
		for _, n := range d.Nodes {
			task.PW.Nodes[n.ID] = &pwNodeResult{Status: "pending"}
		}
		s.Runs = append(s.Runs, task)
		a.executePW(context.Background(), s, task, a.settings)
		want := "fallback"
		if input != "other" {
			want = input + " branch"
		}
		if task.PW.Result != want || task.Status != "completed" {
			t.Fatalf("wrong branch %s: %+v", input, task)
		}
	}
	d.Nodes[1].Type = "condition"
	d.Nodes[1].Cases = nil
	d.Nodes[1].Text = "A"
	d.Nodes = d.Nodes[:4]
	d.Edges = d.Edges[:3]
	d.Edges[1].Port = "true"
	d.Edges[2].Port = "false"
	if err := validator.validatePWRuntimeLocked(d); err != nil {
		t.Fatal(err)
	}
	d.Edges[2].Port = "true"
	if validator.validatePWRuntimeLocked(d) == nil {
		t.Fatal("two true exits accepted")
	}
}
func TestProfessionalWorkflowFileBindingAndAutoInput(t *testing.T) {
	a, s, d := pwFixture(t)
	os.Mkdir(filepath.Join(a.workspace.Name(), "inputs"), 0700)
	os.WriteFile(filepath.Join(a.workspace.Name(), "inputs", "proof.txt"), []byte("bound input evidence"), 0600)
	os.WriteFile(filepath.Join(a.workspace.Name(), "outside.txt"), []byte("secret"), 0600)
	os.Symlink("../outside.txt", filepath.Join(a.workspace.Name(), "inputs", "link.txt"))
	d.Nodes[1] = pwNode{ID: "text", Type: "llm", Text: "Summarize", Bindings: []pwBinding{{Path: "inputs/proof.txt", Kind: "file", Access: "read", AutoRead: true}, {Path: "outputs", Kind: "directory", Access: "write"}}}
	task := &Task{ID: "bound-llm", Mode: "pw", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "running"}}}}
	for _, tc := range []struct {
		name, path string
		allowed    bool
	}{{"read_file", "inputs/proof.txt", true}, {"write_file", "outputs/result.txt", true}, {"read_file", "outside.txt", false}, {"write_file", "inputs/proof.txt", false}, {"read_file", "outputs/result.txt", false}, {"write_file", "outputs2/result.txt", false}, {"run_shell", "inputs/proof.txt", false}, {"mcp_call", "inputs/proof.txt", false}, {"read_file", "../outside.txt", false}} {
		if err := a.pwBindingToolError(task, tc.name, map[string]any{"path": tc.path}); (err == nil) != tc.allowed {
			t.Fatalf("%s %s: %v", tc.name, tc.path, err)
		}
	}
	task.PW.Definition.Nodes[1].Bindings[0] = pwBinding{Path: "inputs", Kind: "directory", Access: "read"}
	if a.pwBindingToolError(task, "read_file", map[string]any{"path": "inputs/link.txt"}) == nil {
		t.Fatal("symlink escaped binding")
	}
	task.PW.Definition = d
	task.PW.Definition.Nodes[1].Bindings[0] = pwBinding{Path: "inputs/proof.txt", Kind: "file", Access: "read", AutoRead: true}
	seen := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		last := b.Messages[len(b.Messages)-1].Content
		seen = strings.Contains(last, "bound input evidence") && strings.Contains(last, "untrusted data")
		parallelReply(w, "bound done")
	}))
	defer provider.Close()
	cfg := routerSettings(t, a, provider.URL, provider.URL)
	task.Model = cfg.Model
	task.ModelSelection = &SessionModelSelection{Model: cfg.Model, Strategy: "auto", Profile: "default", ReasoningEffort: "off"}
	s.Runs = append(s.Runs, task)
	out, err := a.executePWInference(context.Background(), s, task, cfg, d.Nodes[1], "Summarize", map[string]Change{})
	if err != nil || !seen || out != "bound done" {
		t.Fatalf("auto input failed: %s %v seen=%v", out, err, seen)
	}
}

func TestProfessionalWorkflowAutoReadRespectsDisabledTool(t *testing.T) {
	a, _, d := pwFixture(t)
	d.Nodes[1] = pwNode{ID: "text", Type: "llm", Bindings: []pwBinding{{Path: "proof.txt", Kind: "file", Access: "read", AutoRead: true}}}
	os.WriteFile(filepath.Join(a.workspace.Name(), "proof.txt"), []byte("not-to-be-read"), 0600)
	task := &Task{ID: "auto-read-disabled", Mode: "pw", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "running"}}}}
	a.settings.DisabledTools = append(a.settings.DisabledTools, "read_file")
	out, err := a.pwAutoInputs(context.Background(), task, d.Nodes[1], map[string]Change{})
	if err == nil || !strings.Contains(err.Error(), "禁用") || strings.Contains(out, "not-to-be-read") {
		t.Fatal("disabled automatic input not rejected", out, err)
	}
}
