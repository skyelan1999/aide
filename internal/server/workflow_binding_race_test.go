package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ownedBoundTask(t *testing.T) (*App, *Session, *Task) {
	t.Helper()
	// Host filesystem enforcement does not execute the commercial engine. Keep
	// that signature gate intact and test a synthetic, enabled host task only.
	a, s, _ := parallelFixture(t, 1)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "professional-workflow", Version: "0.4.0", Main: "index.js", Enabled: true, Workbench: &PluginWorkbench{ProfessionalWorkflow: true, Entry: "workbench.html"}})
	if !a.pwEnabledLocked() || a.pwAvailableLocked() {
		t.Fatal("expected host-enabled fixture with no signed commercial runtime")
	}
	d := pwDefinition{Schema: 1, ID: "owned-host-flow", Name: "Owned host fixture", Nodes: []pwNode{{ID: "start", Type: "start"}, {ID: "text", Type: "agent"}, {ID: "end", Type: "end"}}}
	d.Nodes[1] = pwNode{ID: "text", Type: "agent", Bindings: []pwBinding{{Path: "allowed", Kind: "directory", Access: "readwrite"}}}
	task := &Task{ID: "owned-bound-task", Mode: "pw", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "running"}}}}
	s.Runs = append(s.Runs, task)
	for _, dir := range []string{"allowed", "other"} {
		if err := os.Mkdir(filepath.Join(a.workPath, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return a, s, task
}

func boundCall(a *App, task *Task, name string, args map[string]any) string {
	raw, _ := json.Marshal(args)
	call := ToolCall{ID: "owned-bound-call"}
	call.Function.Name, call.Function.Arguments = name, string(raw)
	return a.executeToolCall(context.Background(), call, task, map[string]Change{})
}

func TestIssue157ConcurrentParentReplacementCannotExposeUnboundFiles(t *testing.T) {
	a, _, task := ownedBoundTask(t)
	for p, text := range map[string]string{"allowed/proof.txt": "owned-bound-content\nsecond line", "other/proof.txt": "owned-unbound-content", "other/owned-unbound-entry": "fixture"} {
		if err := os.WriteFile(filepath.Join(a.workPath, p), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	allowed, parked := filepath.Join(a.workPath, "allowed"), filepath.Join(a.workPath, "owned-parked")
	probe := filepath.Join(a.workPath, "owned-symlink-probe")
	if err := os.Symlink("other", probe); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	stop, done := make(chan struct{}), make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := os.Rename(allowed, parked); err != nil {
				done <- err
				return
			}
			if err := os.Symlink("other", allowed); err != nil {
				os.Rename(parked, allowed)
				done <- err
				return
			}
			runtime.Gosched()
			if err := os.Remove(allowed); err != nil {
				done <- err
				return
			}
			if err := os.Rename(parked, allowed); err != nil {
				done <- err
				return
			}
			runtime.Gosched()
		}
	}()
	stopped := false
	finish := func() {
		if !stopped {
			stopped = true
			close(stop)
			if err := <-done; err != nil {
				t.Error("owned race actor:", err)
			}
		}
	}
	defer finish()
	for i := 0; i < 5000; i++ {
		name, args := "read_file", map[string]any{"path": "allowed/proof.txt"}
		if i%3 == 1 {
			args["limit"] = 1
		} else if i%3 == 2 {
			name, args = "list_files", map[string]any{"path": "allowed"}
		}
		out := boundCall(a, task, name, args)
		if strings.Contains(out, "owned-unbound-content") || strings.Contains(out, "owned-unbound-entry") {
			t.Fatalf("%s exposed an unbound sibling during directory replacement (iteration %d)", name, i)
		}
	}
	finish()
	if out := boundCall(a, task, "read_file", map[string]any{"path": "allowed/proof.txt"}); !strings.Contains(out, "owned-bound-content") {
		t.Fatal("legitimate bound input could not be read after race", out)
	}
}

func TestIssue157ApprovedBoundProposalRejectsRedirectedParent(t *testing.T) {
	for _, mode := range []string{"captured", "legacy", "mixed"} {
		t.Run(mode, func(t *testing.T) { issue157ApprovalRedirect(t, mode) })
	}
}

func issue157ApprovalRedirect(t *testing.T, mode string) {
	a, s, task := ownedBoundTask(t)
	var docs string
	if mode == "mixed" {
		docs = bindProposalDocs(t, a, "owned-docs")
		task.WorkspaceID, task.WorkspaceRev = a.wsID(), a.wsRevision
	}
	out := boundCall(a, task, "write_file", map[string]any{"path": "allowed/new.txt", "content": "owned proposed content"})
	if len(task.Files) != 1 {
		t.Fatal("bound write did not create a proposal", out)
	}
	if !task.Files[0].NoSymlinks {
		t.Fatal("file binding was not captured on proposal")
	}
	if mode == "mixed" {
		if _, err := proposeDocument(a, task, nil, "owned-report.md", "owned document content"); err != nil {
			t.Fatal(err)
		}
	}
	// Proposal constraints must survive save/reload and a completed node.
	b, err := json.Marshal(task.Files)
	if err != nil {
		t.Fatal(err)
	}
	var restored []Change
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored[0].NoSymlinks {
		t.Fatal("file binding was lost on reload")
	}
	if mode == "legacy" {
		restored[0].NoSymlinks = false // older serialized changes have no marker
	}
	task.Files, task.Status, task.PW.Current = restored, "awaiting_approval", "end"
	if mode == "captured" {
		task.PW = nil // the captured constraint is independent of the live node
	}
	allowed, parked := filepath.Join(a.workPath, "allowed"), filepath.Join(a.workPath, "owned-parked")
	if err := os.Rename(allowed, parked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", allowed); err != nil {
		os.Rename(parked, allowed)
		t.Skip("symlinks unavailable:", err)
	}
	restoredParent := false
	restore := func() {
		if !restoredParent {
			restoredParent = true
			if err := os.Remove(allowed); err != nil {
				t.Error(err)
			}
			if err := os.Rename(parked, allowed); err != nil {
				t.Error(err)
			}
		}
	}
	defer restore()
	url := "/api/sessions/" + s.ID + "/runs/" + task.ID + "/apply"
	w := request(a, "POST", url, map[string]any{})
	t.Logf("redirected approval response: %d %s", w.Code, w.Body.String())
	if w.Code != 409 || task.Files[0].Applied {
		t.Fatalf("redirected approval status=%d applied=%v", w.Code, task.Files[0].Applied)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "other/new.txt")); !os.IsNotExist(err) {
		t.Fatal("bound proposal wrote into unbound sibling", err)
	}
	restore()
	requireStatus(t, request(a, "POST", url, map[string]any{}), 200)
	if got, err := os.ReadFile(filepath.Join(allowed, "new.txt")); err != nil || string(got) != "owned proposed content" {
		t.Fatal("valid retry did not write the bound target", err)
	}
	if mode == "mixed" {
		if got, err := os.ReadFile(filepath.Join(docs, "owned-report.md")); err != nil || string(got) != "owned document content" {
			t.Fatal("mixed document retry did not preserve the document target", err)
		}
	}
}

func TestIssue157PinnedDirectoryAndAllReadModes(t *testing.T) {
	a, _, task := ownedBoundTask(t)
	var encoded []byte
	encoded = append(encoded, 0xff, 0xfe)
	for _, r := range "{\n\"owned\": true\n}" {
		encoded = append(encoded, byte(r), 0)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "allowed/windows.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if out := boundCall(a, task, "read_file", map[string]any{"path": "allowed/windows.json", "offset": 1, "limit": 1}); !strings.Contains(out, `"owned": true`) {
		t.Fatal("bound line reader lost UTF-16 JSON normalization", out)
	}
	for _, dir := range []string{"allowed", "other"} {
		if err := os.WriteFile(filepath.Join(a.workPath, dir, "raw.bin"), []byte(dir+"\x00fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pinned, closeRoot, err := boundDirectoryRoot(a.workspace, "allowed", false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot()
	if err := os.Rename(filepath.Join(a.workPath, "allowed"), filepath.Join(a.workPath, "owned-parked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", filepath.Join(a.workPath, "allowed")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	got, err := readBoundRawBytes(pinned, "raw.bin")
	if err != nil || string(got) != "allowed\x00fixture" {
		t.Fatal("pinned directory followed its replacement", err)
	}
	if _, err := readBoundRawBytes(a.workspace, "allowed/raw.bin"); err == nil {
		t.Fatal("bound raw read followed a directory symlink")
	}
	// Existing unrestricted workspace reads retain their in-root link behavior.
	if got, err := readRawBytes(a.workspace, "allowed/raw.bin"); err != nil || string(got) != "other\x00fixture" {
		t.Fatal("ordinary workspace raw read compatibility changed", err)
	}
}

func TestIssue157NewParentsLeafLinksAndApprovalFingerprint(t *testing.T) {
	a, _, task := ownedBoundTask(t)
	if err := putBoundText(a.workspace, "allowed/new/deep/proof.txt", []byte("owned new file")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "allowed/sibling.txt"), []byte("owned sibling"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sibling.txt", filepath.Join(a.workPath, "allowed/link.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := readBoundText(a.workspace, "allowed/link.txt"); err == nil {
		t.Fatal("file binding followed a same-parent leaf alias")
	}
	if err := putBoundText(a.workspace, "allowed/link.txt", []byte("must not replace link")); err == nil {
		t.Fatal("bound write accepted an existing leaf link")
	}
	if got, err := os.ReadFile(filepath.Join(a.workPath, "allowed/sibling.txt")); err != nil || string(got) != "owned sibling" {
		t.Fatal("bound write modified sibling through a leaf link", err)
	}
	file := Change{Path: "allowed/proof.txt", Content: "owned proposal"}
	unbound := approvalFilesFingerprint([]Change{file})
	file.NoSymlinks = true
	if approvalFilesFingerprint([]Change{file}) == unbound || !pwChangeNoSymlinks(task, file) {
		t.Fatal("file constraint missing from approval identity")
	}
}

func TestIssue157HostBindingGuardKeepsPathAndToolRestrictions(t *testing.T) {
	a, _, task := ownedBoundTask(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "allowed/proof.txt"), []byte("owned bound input"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"read_file", map[string]any{"path": "other/proof.txt"}},
		{"write_file", map[string]any{"path": "allowed2/proof.txt"}},
		{"read_file", map[string]any{"path": "../other/proof.txt"}},
		{"read_file", map[string]any{"path": "allowed/proof.txt", "source": "unbound-reference"}},
		{"run_shell", map[string]any{"command": "owned inert text"}},
		{"mcp_call", map[string]any{"source": "owned-reference", "tool": "read"}},
	} {
		if err := a.pwBindingToolError(task, tc.tool, tc.args); err == nil {
			t.Fatalf("binding guard accepted %s outside its declared contract", tc.tool)
		}
	}
	task.PW.Definition.Nodes[1].Bindings[0].Access = "read"
	if err := a.pwBindingToolError(task, "write_file", map[string]any{"path": "allowed/new.txt"}); err == nil {
		t.Fatal("read-only binding permitted a write proposal")
	}
	if out := boundCall(a, task, "read_file", map[string]any{"path": "allowed/proof.txt"}); out != "owned bound input" {
		t.Fatal("valid read-only binding no longer reads", out)
	}
}

func TestIssue157AppliedReadbackKeepsLegacyBinding(t *testing.T) {
	a, _, task := ownedBoundTask(t)
	for _, dir := range []string{"allowed", "other"} {
		if err := os.WriteFile(filepath.Join(a.workPath, dir, "proof.txt"), []byte(dir+" owned content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(filepath.Join(a.workPath, "allowed"), filepath.Join(a.workPath, "owned-parked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", filepath.Join(a.workPath, "allowed")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	task.PW.Current = "end"
	file := Change{Path: "allowed/proof.txt", Applied: true}
	a.mu.Lock()
	_, err := a.readAppliedProposalLocked(task, file)
	a.mu.Unlock()
	if err == nil {
		t.Fatal("legacy applied readback lost the originating binding")
	}
}
