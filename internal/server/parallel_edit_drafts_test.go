package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParallelEditDraftValidation(t *testing.T) {
	for _, text := range []string{`{"path":"../x.go","content":"x"}`, `{"path":"a.go","content":""}`, `{"path":"a.go","content":"x","command":"touch x"}`, `{"path":"a.go","content":"x"} junk`, `{"path":"a.go","content":"x"}{}`} {
		if _, err := parseParallelEditDraft(text, "a.go"); err == nil {
			t.Fatalf("invalid draft accepted: %s", text)
		}
	}
	if d, err := parseParallelEditDraft(`{"path":"a.go","content":"package main\n"}`, "a.go"); err != nil || d.Content != "package main\n" {
		t.Fatal(d, err)
	}
	prompt := "修复相互独立的文件"
	if len(parallelEditTargets(prompt, []string{"[CASE01] a.go 修改", "[CASE02] a.go 修改"})) != 0 {
		t.Fatal("duplicate file accepted")
	}
	if len(parallelEditTargets("独立分析草案，不执行工具", []string{"[CASE01] a.go 修改", "[CASE02] b.go 修改"})) != 0 {
		t.Fatal("no-tool draft turned into edits")
	}
}
func TestParallelEditDraftsRequireReadsAndNormalApproval(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	task.WorkspaceMode = "local"
	task.WorkspaceID = a.wsID()
	task.WorkspaceRev = a.wsRevision
	task.Prompt = "修复以下相互独立的文件，使用write_file生成待审批提案：\n[CASE01] one.go 第一项\n[CASE02] two.go 第二项"
	for _, p := range []string{"one.go", "two.go"} {
		if err := a.workspace.WriteFile(p, []byte("old "+p), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var workers atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		if !strings.HasPrefix(last, "[parallel edit draft]") || len(body.Tools) != 0 {
			t.Error("worker draft protocol/tool boundary changed")
		}
		workers.Add(1)
		p := "one.go"
		if strings.Contains(last, "two.go") {
			p = "two.go"
		}
		if !strings.Contains(last, "old "+p) {
			t.Error("full original source was not pinned to worker assignment")
		}
		raw, _ := json.Marshal(parallelEditDraft{Path: p, Content: "updated " + p})
		parallelReply(w, string(raw))
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	input := []Message{{Role: "user", Content: task.Prompt}}
	if _, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 512}); err != nil {
		t.Fatal(err)
	}
	if workers.Load() != 0 || len(task.Files) != 0 {
		t.Fatal("drafting began without full reads")
	}
	for _, p := range []string{"one.go", "two.go"} {
		call := readToolCall("read-"+p, "read_file", `{"path":"`+p+`"}`)
		text := a.executeToolCall(context.Background(), call, task, nil)
		input = append(input, Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Message{Role: "tool", ToolCallID: call.ID, Content: text})
	}
	budget := &parallelEditBudget{Remaining: 2}
	chain, err := a.prepareParallel(context.WithValue(context.Background(), parallelEditBudgetKey{}, budget), s, task, cfg, input, ProfileParams{MaxTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	if workers.Load() != 2 || len(task.Files) != 2 || budget.Used != 2 || !strings.Contains(chain[len(chain)-1].Content, "pending proposals") {
		t.Fatalf("drafts not registered through main: workers=%d files=%d used=%d", workers.Load(), len(task.Files), budget.Used)
	}
	for _, f := range task.Files {
		b, _ := a.workspace.ReadFile(f.Path)
		if string(b) != "old "+f.Path || f.Applied || f.BaseHash == "" {
			t.Fatal("draft changed disk or lost version check")
		}
	}
	if len(task.ToolUses) != 2 || task.ToolUses[0].Tool != "write_file" || task.Parallel.DraftProposals != 2 {
		t.Fatal("normal tool audit missing")
	}
	task.Status = "awaiting_approval"
	if err := a.workspace.WriteFile("one.go", []byte("outside change"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 409)
	if err := a.workspace.WriteFile("one.go", []byte("old one.go"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	for _, p := range []string{"one.go", "two.go"} {
		b, _ := a.workspace.ReadFile(p)
		if string(b) != "updated "+p {
			t.Fatal("approved proposal was not applied")
		}
	}
}
func TestParallelEditEligibilityPreservesPolicy(t *testing.T) {
	a, _, task := parallelFixture(t, 2)
	task.WorkspaceMode = "local"
	task.WorkspaceID = a.wsID()
	task.WorkspaceRev = a.wsRevision
	targets := map[string]string{"a": "a.go", "b": "b.go"}
	if allowed, bases := a.parallelEditBases(task, targets); !allowed || bases != nil {
		t.Fatal("missing reads were not distinguished")
	}
	a.settings.DisabledTools = []string{"write_file"}
	if allowed, _ := a.parallelEditBases(task, targets); allowed {
		t.Fatal("disabled tool bypassed")
	}
	a.settings.DisabledTools = nil
	task.WorkspaceMode = "ssh"
	if allowed, _ := a.parallelEditBases(task, targets); allowed {
		t.Fatal("remote writes admitted")
	}
	task.WorkspaceMode = "local"
	task.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Enabled: true, Event: "before_tool", Tool: "write_file"}}}
	if allowed, _ := a.parallelEditBases(task, targets); allowed {
		t.Fatal("write hooks bypassed")
	}
}

func TestParallelEditDraftBatchRejectsBeforeRegistration(t *testing.T) {
	for _, mode := range []string{"bad second target", "changed snapshot", "clipped snapshot", "denied", "budget", "scope", "missing part", "workspace", "hook", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, _, task := parallelFixture(t, 2)
			task.WorkspaceMode = "local"
			task.WorkspaceID = a.wsID()
			task.WorkspaceRev = a.wsRevision
			targets := map[string]string{"one": "one.go", "two": "two.go"}
			parts := []ParallelPart{}
			for _, title := range []string{"one", "two"} {
				p := targets[title]
				_ = a.workspace.WriteFile(p, []byte("original"), 0600)
				a.rememberWorkspaceRead(task, p, []byte("original"))
				raw, _ := json.Marshal(parallelEditDraft{Path: p, Content: "candidate"})
				parts = append(parts, ParallelPart{Title: title, Status: "completed", Scope: "current", DraftPath: p, Content: string(raw)})
			}
			_, bases := a.parallelEditBases(task, targets)
			ctx := context.Background()
			budget := &parallelEditBudget{Remaining: 2}
			switch mode {
			case "bad second target":
				parts[1].Content = `{"path":"outside.go","content":"candidate"}`
			case "changed snapshot":
				a.rememberWorkspaceRead(task, "one.go", []byte("changed"))
			case "clipped snapshot":
				a.mu.Lock()
				v := task.readSnapshots["one.go"]
				v.Before = "clipped preview"
				task.readSnapshots["one.go"] = v
				a.mu.Unlock()
			case "denied":
				a.settings.DisabledTools = []string{"write_file"}
			case "budget":
				budget.Remaining = 1
			case "scope":
				parts[1].Scope = "earlier"
			case "missing part":
				parts = parts[:1]
			case "workspace":
				task.WorkspaceID = "another"
			case "hook":
				task.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Enabled: true, Event: "after_tool", Tool: "write_file"}}}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			chain, registered, _ := a.registerParallelEditDrafts(context.WithValue(ctx, parallelEditBudgetKey{}, budget), task, Settings{Model: "fixture"}, nil, parts, targets, bases, "current")
			if mode == "cancel" {
				results := 0
				for _, m := range chain {
					if m.Role == "tool" {
						results++
					}
				}
				if results != 2 {
					t.Fatal("cancelled drafts left orphaned tool calls")
				}
			}
			if registered || len(task.Files) != 0 || len(task.ToolUses) != 0 || budget.Used != 0 {
				t.Fatalf("invalid batch mutated pending proposals: registered=%v files=%d audit=%d used=%d", registered, len(task.Files), len(task.ToolUses), budget.Used)
			}
		})
	}
}
