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

func bindProposalDocs(t *testing.T, a *App, name string) string {
	t.Helper()
	dir := filepath.Join(a.workPath, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"docs": map[string]any{"location": "local", "path": "/workspace/" + name}}), 200)
	return dir
}
func documentProposalTask(a *App) (*Session, *Task) {
	task := &Task{ID: "doc-run", Status: "awaiting_approval", WorkspaceID: a.wsID(), AutoReview: true, Prompt: "Create report.md in the configured automatic documents directory"}
	s := &Session{ID: "doc-owner", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[s.ID] = s
	return s, task
}
func proposeDocument(a *App, task *Task, versions map[string]Change, p, content string) (string, error) {
	return a.recordToolProposal(task, versions, map[string]any{"type": "file", "source": systemDocsSource, "path": p, "content": content})
}
func TestDocumentProposalBindingAndMixedPaths(t *testing.T) {
	a := testApp(t)
	docs := bindProposalDocs(t, a, "docs-A")
	s, task := documentProposalTask(a)
	if _, err := proposeDocument(a, task, nil, "report.md", "document"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.recordToolProposal(task, nil, map[string]any{"type": "file", "path": "report.md", "content": "workspace"}); err != nil {
		t.Fatal(err)
	}
	if len(task.Files) != 2 || task.Files[0].Source != systemDocsSource || task.Files[1].Source != "" {
		t.Fatalf("source collision: %+v", task.Files)
	}
	if _, err := os.Stat(filepath.Join(docs, "report.md")); !os.IsNotExist(err) {
		t.Fatal("proposal wrote before approval")
	}
	if a.pendingNewFileRead(task, "report.md", systemDocsSource) == "" {
		t.Fatal("missing pending source receipt")
	}
	before := approvalFilesFingerprint(task.Files)
	changed := append([]Change(nil), task.Files...)
	changed[0].TargetPath += "-other"
	if before == approvalFilesFingerprint(changed) {
		t.Fatal("approval ignores destination")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	for p, want := range map[string]string{filepath.Join(docs, "report.md"): "document", filepath.Join(a.workPath, "report.md"): "workspace"} {
		b, err := os.ReadFile(p)
		if err != nil || string(b) != want {
			t.Fatalf("%s: %s %v", p, b, err)
		}
	}
	a.recordReviewedFileResult(task)
	if !strings.Contains(task.Steps[len(task.Steps)-1].Content, "自动系统文档 / report.md") || !strings.Contains(task.Steps[len(task.Steps)-1].Content, "读回一致") {
		t.Fatal("receipt did not use document binding")
	}
	// Persisted origin and destination survive normal session JSON round trips.
	raw, _ := json.Marshal(s)
	var restored Session
	if err := json.Unmarshal(raw, &restored); err != nil || restored.Runs[0].Files[0] != task.Files[0] {
		t.Fatal("destination not persisted")
	}
}
func TestDocumentProposalBindingChangeRejectsOldApproval(t *testing.T) {
	a := testApp(t)
	old := bindProposalDocs(t, a, "docs-A")
	s, task := documentProposalTask(a)
	if _, err := proposeDocument(a, task, nil, "report.md", "A"); err != nil {
		t.Fatal(err)
	}
	next := bindProposalDocs(t, a, "docs-B")
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 409)
	for _, dir := range []string{old, next} {
		if _, err := os.Stat(filepath.Join(dir, "report.md")); !os.IsNotExist(err) {
			t.Fatal("stale proposal wrote a file")
		}
	}
	if _, err := proposeDocument(a, task, nil, "report.md", "B"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	b, _ := os.ReadFile(filepath.Join(next, "report.md"))
	if string(b) != "B" {
		t.Fatal("new binding not followed")
	}
}
func TestDocumentProposalExistingAttachmentAndVersion(t *testing.T) {
	a := testApp(t)
	dir := bindProposalDocs(t, a, "docs-A")
	s, task := documentProposalTask(a)
	p := filepath.Join(dir, "report.md")
	os.WriteFile(p, []byte("before"), 0644)
	if _, err := proposeDocument(a, task, nil, "report.md", "after"); err == nil {
		t.Fatal("unattached original accepted")
	}
	_, _, versions, err := a.attachmentContext([]Attachment{{Root: "source", Source: systemDocsSource, Path: "report.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proposeDocument(a, task, versions, "report.md", "after"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	eligible := a.newProposalTargetEligibleLocked(task.Files[0])
	a.mu.Unlock()
	if eligible {
		t.Fatal("existing file auto-eligible")
	}
	os.WriteFile(p, []byte("outside edit"), 0644)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 409)
	b, _ := os.ReadFile(p)
	if string(b) != "outside edit" {
		t.Fatal("version conflict overwritten")
	}
}
func TestDocumentProposalRejectsUnavailableAndEscapedBinding(t *testing.T) {
	a := testApp(t)
	_, task := documentProposalTask(a)
	if _, err := proposeDocument(a, task, nil, "report.md", "x"); err == nil {
		t.Fatal("empty binding fell back")
	}
	dir := bindProposalDocs(t, a, "docs-A")
	task.WorkspaceID = a.wsID()
	for _, p := range []string{"../outside.md", "/absolute.md", "."} {
		if _, err := proposeDocument(a, task, nil, p, "x"); err == nil {
			t.Fatalf("unsafe %s", p)
		}
	}
	os.Symlink(t.TempDir(), filepath.Join(dir, "escape"))
	if _, err := proposeDocument(a, task, nil, "escape/report.md", "x"); err == nil {
		t.Fatal("escaping symlink accepted")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == systemDocsSource {
			a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
		}
	}
	a.mu.Unlock()
	if _, err := proposeDocument(a, task, nil, "report.md", "x"); err == nil {
		t.Fatal("hidden documents accepted")
	}
}
func TestDocumentProposalIndependentReviewUsesBoundRoot(t *testing.T) {
	a := testApp(t)
	dir := bindProposalDocs(t, a, "docs-A")
	_, task := documentProposalTask(a)
	if _, err := proposeDocument(a, task, nil, "report.md", "reviewed"); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&payload)
		if !strings.Contains(payload.Messages[1].Content, "/workspace/docs-A/report.md") {
			t.Error("review omitted configured destination")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"authorized document target"}`}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	a.reviewPendingFiles(context.Background(), task)
	b, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil || string(b) != "reviewed" || !task.Applied {
		t.Fatalf("not applied to binding: %s %v %+v", b, err, task.ApprovalReviews)
	}
}

func TestSourceProposalVisibilityAndWriteLocks(t *testing.T) {
	for _, mode := range []string{"allowed", "readonly", "hidden", "disabled", "moved", "removed", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			dir := filepath.Join(a.workPath, "reference-a")
			os.MkdirAll(dir, 0755)
			source := sourceBody("chosen-ref", "Chosen reference", "local", map[string]any{"path": "/workspace/reference-a"}, true)
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{source}}), 200)
			s, task := documentProposalTask(a)
			args, _ := json.Marshal(map[string]any{"source": "chosen-ref", "path": "report.md", "content": "reference deliverable"})
			call := ToolCall{}
			call.Function.Name = "write_file"
			call.Function.Arguments = string(args)
			out := a.executeToolCall(context.Background(), call, task, nil)
			if !strings.Contains(out, "已生成文件修改提案") || len(task.Files) != 1 || task.Files[0].Source != "chosen-ref" {
				t.Fatalf("tool not routed to reference: %s", out)
			}
			if mode == "readonly" {
				source["rw"] = false
			}
			if mode == "hidden" {
				source["aiHiddenWorkspaces"] = []string{a.wsID()}
			}
			if mode == "disabled" {
				source["enabled"] = false
			}
			if mode == "moved" {
				os.MkdirAll(filepath.Join(a.workPath, "reference-b"), 0755)
				source["config"] = map[string]any{"path": "/workspace/reference-b"}
			}
			refs := []any{source}
			if mode == "removed" {
				refs = []any{}
			}
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": refs}), 200)
			if mode == "sandbox" {
				a.settings.SandboxMode = "read-only"
			}
			response := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil)
			if mode == "allowed" {
				requireStatus(t, response, 200)
				b, err := os.ReadFile(filepath.Join(dir, "report.md"))
				if err != nil || string(b) != "reference deliverable" {
					t.Fatal("reference destination missing")
				}
			} else {
				if response.Code != 409 && response.Code != 403 {
					t.Fatalf("unlocked via %s: %d %s", mode, response.Code, response.Body)
				}
				if _, err := os.Stat(filepath.Join(dir, "report.md")); !os.IsNotExist(err) {
					t.Fatal("locked reference written")
				}
			}
			if _, err := os.Stat(filepath.Join(a.workPath, "report.md")); !os.IsNotExist(err) {
				t.Fatal("silently fell back to workspace")
			}
		})
	}
}
func TestSourceProposalRejectsLockedCreation(t *testing.T) {
	for _, mode := range []string{"readonly", "hidden", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			os.MkdirAll(filepath.Join(a.workPath, "reference"), 0755)
			src := sourceBody("ref", "Reference", "local", map[string]any{"path": "/workspace/reference"}, true)
			if mode == "readonly" {
				src["rw"] = false
			}
			if mode == "hidden" {
				src["aiHiddenWorkspaces"] = []string{a.wsID()}
			}
			if mode == "disabled" {
				src["enabled"] = false
			}
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{src}}), 200)
			_, task := documentProposalTask(a)
			if _, err := a.recordToolProposal(task, nil, map[string]any{"type": "file", "source": "ref", "path": "report.md", "content": "x"}); err == nil {
				t.Fatal("locked source accepted proposal")
			}
		})
	}
}
func TestSourceProposalIndependentSFTP(t *testing.T) {
	a := testApp(t)
	reliabilitySSHStub(t, a, true)
	src := registerReliabilitySource(a, Source{ID: "remote-ref", RW: true, Config: SourceConfig{Path: "/remote/reference", Host: "fixture.example", Username: "fixture", Auth: "none"}})
	remote := t.TempDir()
	log := filepath.Join(t.TempDir(), "batches")
	a.sftpBin = filepath.Join(t.TempDir(), "sftp")
	writeStub(t, a.sftpBin, `#!/bin/sh
while IFS= read -r line; do
 printf '%s\n' "$line" >> `+shellQuote(log)+`
 eval "set -- $line"
 case "$1" in
 get) if [ ! -f `+shellQuote(remote)+`/"$(basename "$2")" ]; then printf 'File "%s" not found.\n' "$2" >&2; exit 1; fi; cp `+shellQuote(remote)+`/"$(basename "$2")" "$3" || exit 1 ;;
 put) cp "$3" `+shellQuote(remote)+`/"$(basename "$4")" || exit 1 ;;
 rename) mv `+shellQuote(remote)+`/"$(basename "$2")" `+shellQuote(remote)+`/"$(basename "$3")" || exit 1 ;;
 esac
done
`)
	s, task := documentProposalTask(a)
	if _, err := a.recordToolProposal(task, nil, map[string]any{"type": "file", "source": src.ID, "path": "report.md", "content": "remote document"}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	b, err := os.ReadFile(filepath.Join(remote, "report.md"))
	if err != nil || string(b) != "remote document" {
		t.Fatalf("SFTP destination: %s %v", b, err)
	}
	b, _ = os.ReadFile(log)
	if !strings.Contains(string(b), "/remote/reference/report.md") {
		t.Fatal("source root not used")
	}
}
