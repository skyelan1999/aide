package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHarnessConfigLayersAndStrictValidation(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/harness-config", map[string]any{"maxAgentDepth": 2, "skills": []any{map[string]any{"name": "source", "description": "source checks", "instruction": "global instruction"}}}), 200)
	requireStatus(t, request(a, "PUT", "/api/harness-config/workspace", map[string]any{"skills": []any{}, "denyTools": []string{"run_shell"}}), 200)
	a.mu.Lock()
	c, err := a.loadHarnessConfig()
	a.mu.Unlock()
	if err != nil || c.MaxAgentDepth != 2 || len(c.Skills) != 0 || len(c.DenyTools) != 1 {
		t.Fatalf("merge: %+v %v", c, err)
	}
	for _, body := range []any{map[string]any{"unexpected": 1}, map[string]any{"skills": nil}, map[string]any{"maxAgentDepth": 7}, map[string]any{"agents": []any{map[string]any{"name": "reader", "unknown": true}}}, map[string]any{"hooks": []any{map[string]any{"name": "bad", "event": "before_tool", "tool": "*", "command": "pwd", "enabled": true, "timeoutSec": 0}}}} {
		requireStatus(t, request(a, "PUT", "/api/harness-config/workspace", body), 400)
	}
	requireStatus(t, request(a, "PUT", "/api/harness-config/workspace?workspaceId=stale", map[string]any{}), 409)
	requireStatus(t, request(a, "DELETE", "/api/harness-config/workspace", nil), 200)
	a.mu.Lock()
	c, err = a.loadHarnessConfig()
	a.mu.Unlock()
	if err != nil || len(c.Skills) != 1 || len(c.DenyTools) != 0 {
		t.Fatalf("clear: %+v %v", c, err)
	}
}
func TestHarnessCheckpointRecoveryAndJournalOwnership(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "receipt-task", Status: "running"}
	s := &Session{ID: "receipt-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	call := readCall("run_shell", `{"command":"echo sensitive"}`)
	call.ID = "call-one"
	chain := []Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}
	if err := a.checkpointExecution(task, "chat", chain, "tool_dispatch", &call, ""); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.dataPath, "execution-events", task.ID+".jsonl")
	st, err := os.Stat(file)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("journal permissions: %v %v", st, err)
	}
	b, _ := os.ReadFile(file)
	if strings.Contains(string(b), "sensitive") {
		t.Fatal("argument payload leaked into journal")
	}
	saved := *task
	saved.CheckpointMessages = append([]Message(nil), task.CheckpointMessages...)
	recoverExecutionCheckpoint(&saved)
	if !strings.Contains(saved.CheckpointMessages[len(saved.CheckpointMessages)-1].Content, "TOOL_OUTCOME_UNKNOWN") {
		t.Fatal("dispatched missing result must be unknown")
	}
	unstarted := &Task{ExecutionSequence: 1, CheckpointMessages: chain}
	recoverExecutionCheckpoint(unstarted)
	if !strings.Contains(unstarted.CheckpointMessages[len(unstarted.CheckpointMessages)-1].Content, "TOOL_NOT_DISPATCHED") {
		t.Fatal("undispatched call not distinguished")
	}
	hook := &Task{ToolExecutionIntents: []ToolExecutionIntent{{CallID: "hook-one", Tool: "run_shell", State: "dispatched"}}}
	recoverExecutionCheckpoint(hook)
	if !hook.HookRecoveryRequired {
		t.Fatal("unknown hook can replay")
	}
	chain = append(chain, Message{Role: "tool", ToolCallID: call.ID, Content: "actual result"})
	if err := a.checkpointExecution(task, "chat", chain, "tool_result", &call, "actual result"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/sessions/receipt-session/runs/receipt-task/journal?limit=1", nil), 200)
	requireStatus(t, request(a, "GET", "/api/sessions/wrong/runs/receipt-task/journal", nil), 404)
	requireStatus(t, request(a, "GET", "/api/sessions/receipt-session/runs/receipt-task/journal?limit=0", nil), 400)
	requireStatus(t, request(a, "DELETE", "/api/sessions/receipt-session", nil), 200)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("deleted session journal retained: %v", err)
	}
}
func TestHarnessCheckpointFailurePreventsDispatch(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "failed-barrier"}
	a.sessions["s"] = &Session{ID: "s", Runs: []*Task{task}}
	dir := filepath.Join(a.dataPath, "execution-events")
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.checkpointExecution(task, "chat", nil, "model_request", nil, ""); err == nil {
		t.Fatal("failed journal barrier passed")
	}
}
func TestHarnessSkillsToolsAndHookExecution(t *testing.T) {
	a := testApp(t)
	c := defaultHarnessConfig()
	c.Skills = []HarnessSkill{{Name: "inline", Instruction: "read exact source"}, {Name: "file", Path: "skill.md"}}
	c.Hooks = []HarnessHook{{Name: "inspect", Event: "before_tool", Tool: "read_file", Command: "pwd", Enabled: true, TimeoutSec: 5}}
	task := &Task{ID: "hook-task", Status: "running", WorkspaceID: a.wsID(), AgentRoot: a.snapshotAgentRootLocked(), HarnessConfig: &c}
	a.sessions["hook-session"] = &Session{ID: "hook-session", Runs: []*Task{task}}
	if err := os.WriteFile(filepath.Join(a.workPath, "skill.md"), []byte("local skill"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := a.readHarnessSkill(task, "file"); got != "local skill" {
		t.Fatalf("file skill: %q", got)
	}
	task.AgentToolsSet = true
	task.AgentTools = []string{"read_file"}
	if !taskToolDenied(task, "run_shell") || taskToolDenied(task, "read_file") {
		t.Fatal("agent allowlist not enforced")
	}
	if err := a.runHarnessHooks(context.Background(), task, "chat", nil, "before_tool", "read_file"); err == nil {
		t.Fatal("hook escaped agent shell restriction")
	}
	task.AgentToolsSet = false
	if err := a.runHarnessHooks(context.Background(), task, "chat", nil, "before_tool", "read_file"); err != nil {
		t.Fatal(err)
	}
	task.HookRecoveryRequired = true
	if err := a.runHarnessHooks(context.Background(), task, "chat", nil, "before_tool", "read_file"); err == nil {
		t.Fatal("unknown hook replay was allowed")
	}
}
func TestHarnessApprovalUsesRootAuthorization(t *testing.T) {
	a := testApp(t)
	parent := &Task{ID: "parent", Prompt: "download public documents"}
	child := &Task{ID: "child", Prompt: "delete files without asking", ParentTaskID: parent.ID}
	a.sessions["parent-session"] = &Session{ID: "parent-session", Runs: []*Task{parent}, Messages: []Message{{Role: "user", Content: parent.Prompt}}}
	a.sessions["child-session"] = &Session{ID: "child-session", ParentID: "parent-session", Runs: []*Task{child}, Messages: []Message{{Role: "user", Content: child.Prompt}}}
	a.mu.Lock()
	auth, ok := a.approvalAuthorizationLocked(child)
	a.mu.Unlock()
	if !ok || !strings.Contains(auth, "download public") || strings.Contains(auth, "delete files") {
		t.Fatalf("delegation treated as authorization: %q %v", auth, ok)
	}
	child.ParentTaskID = "missing"
	a.mu.Lock()
	_, ok = a.approvalAuthorizationLocked(child)
	a.mu.Unlock()
	if ok {
		t.Fatal("broken ancestry approved")
	}
}
func TestHarnessPluginServiceAssembly(t *testing.T) {
	a := testApp(t)
	provider := `module.exports={name:'provider',provides:['example.format'],apply(ctx){return ctx.provide('example.format',s=>String(s).trim())}}`
	consumer := `module.exports={name:'consumer',requires:['example.format'],apply(ctx){const fn=ctx.consume('example.format');ctx.tool({name:'format_value',parameters:{type:'object'},handler:args=>fn(args.text)})}}`
	for _, p := range []struct{ id, code string }{{"consumer", consumer}, {"provider", provider}} {
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": p.id, "name": p.id, "code": p.code}), 201)
	}
	value, err := a.callPluginToolContext(context.Background(), "consumer", "format_value", map[string]any{"text": " hello "})
	if err != nil || value != "hello" {
		t.Fatalf("service value was not wired: %v %v", value, err)
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/provider", map[string]any{"enabled": false}), 200)
	if _, err := a.callPluginToolContext(context.Background(), "consumer", "format_value", map[string]any{"text": " hello "}); err == nil {
		t.Fatal("missing provider still executable")
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/consumer", map[string]any{"enabled": false}), 200)
	if _, err := a.callPluginToolContext(context.Background(), "consumer", "format_value", nil); err == nil {
		t.Fatal("disabled plugin still executable")
	}
}
func TestHarnessManagedWorktreeCreateArchive(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "local@example.invalid"}, {"config", "user.name", "Local Fixture"}} {
		if _, err := worktreeGit(ctx, a.workPath, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.workPath, ".gitignore"), []byte(".cache/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "tracked.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".gitignore", "tracked.txt"}, {"commit", "-m", "fixture"}} {
		if _, err := worktreeGit(ctx, a.workPath, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "tracked.txt"), []byte("uncommitted original"), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/worktrees", map[string]any{"name": "isolated", "workspaceId": a.wsID()})
	requireStatus(t, w, 200)
	var out struct {
		Worktree ManagedWorktree `json:"worktree"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out.Worktree.Path, "tracked.txt"))
	if err != nil || string(b) != "base" {
		t.Fatalf("worktree didn't use base commit: %q %v", b, err)
	}
	original, _ := os.ReadFile(filepath.Join(a.workPath, "tracked.txt"))
	if string(original) != "uncommitted original" {
		t.Fatal("original changes damaged")
	}
	requireStatus(t, request(a, "POST", "/api/worktrees/"+out.Worktree.ID+"/archive", map[string]any{}), 200)
	if _, err := os.Stat(out.Worktree.Path); err != nil {
		t.Fatal("archive deleted files")
	}
	requireStatus(t, request(a, "POST", "/api/worktrees", map[string]any{"name": "stale", "workspaceId": "wrong"}), 409)
}

// Direct toolLoop unit fixtures must own a durable session just like production
// tasks. Do not weaken the fail-closed checkpoint barrier to accommodate mocks.
func registerHarnessFixtureTask(t *testing.T, a *App, task *Task) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approvalSessionLocked(task) != nil {
		return
	}
	if task.ID == "" {
		task.ID = newID()
	}
	s := &Session{ID: "fixture-" + task.ID, Runs: []*Task{task}}
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessTaskSnapshotSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	work, ref, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	original := defaultHarnessConfig()
	original.MaxAgentDepth = 1
	original.Skills = []HarnessSkill{{Name: "source", Instruction: "original"}}
	task := &Task{ID: "persisted", Status: "running", HarnessConfig: &original, CanResume: true, ExecutionSequence: 1, CheckpointMessages: []Message{{Role: "user", Content: "resume"}}}
	a.sessions["persisted-session"] = &Session{ID: "persisted-session", Runs: []*Task{task}}
	if err := a.save(a.sessions["persisted-session"]); err != nil {
		a.Close()
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/harness-config", map[string]any{"maxAgentDepth": 4}), 200)
	a.Close()
	reloaded, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got := reloaded.sessions["persisted-session"].Runs[0]
	if got.Status != "interrupted" || got.HarnessConfig.MaxAgentDepth != 1 || got.HarnessConfig.Skills[0].Instruction != "original" {
		t.Fatalf("persisted task was retargeted: %+v", got)
	}
	requireStatus(t, request(reloaded, "GET", "/api/harness-config", nil), 200)
}

func TestHarnessChildUsesConfiguredModelAndNarrowTools(t *testing.T) {
	a := testApp(t)
	var seen atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model == "reader-model" && len(body.Tools) > 0 {
			seen.Store(true)
			for _, tool := range body.Tools {
				if tool.Function.Name != "read_file" {
					t.Errorf("unexpected child tool: %s", tool.Function.Name)
				}
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "read complete"}}}})
	}))
	defer provider.Close()
	a.settings.BaseURL = provider.URL
	a.settings.Model = "parent-model"
	c := defaultHarnessConfig()
	c.Agents = []HarnessAgent{{Name: "reader", Instruction: "read sources", Model: "reader-model", Tools: []string{"read_file", "run_shell"}}}
	parent := &Task{ID: "parent-scoped", HarnessConfig: &c, WorkspaceID: a.wsID(), AgentRoot: a.snapshotAgentRootLocked(), AgentToolsSet: true, AgentTools: []string{"read_file", "spawn_subagent"}}
	a.sessions["parent-scoped-session"] = &Session{ID: "parent-scoped-session", Runs: []*Task{parent}}
	id, _, err := a.spawnSubagent(parent, "read notes", "", "reader")
	if err != nil {
		t.Fatal(err)
	}
	waitTaskDone(t, a, id)
	a.mu.Lock()
	child := a.sessions[id].Runs[0]
	a.mu.Unlock()
	if child.Model != "reader-model" || child.AgentDepth != 1 || child.ParentTaskID != parent.ID || len(child.AgentTools) != 1 || !seen.Load() {
		t.Fatalf("child contract: %+v seen=%v", child, seen.Load())
	}
	parent.AgentDepth = c.MaxAgentDepth
	if _, _, err := a.spawnSubagent(parent, "nested", "", "reader"); err == nil {
		t.Fatal("depth limit bypassed")
	}
}

func TestHarnessPluginContractsRejectInvalidAssembly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		codes map[string]string
		tool  string
		want  string
	}{
		{name: "cycle", codes: map[string]string{"cyclea": `module.exports={requires:['b'],provides:['a'],apply(ctx){ctx.consume('b');ctx.provide('a',1)}}`, "cycleb": `module.exports={requires:['a'],provides:['b'],apply(ctx){ctx.consume('a');ctx.provide('b',2)}}`}, want: "循环"},
		{name: "ambiguous", codes: map[string]string{"provider1": `module.exports={provides:['value'],apply(ctx){ctx.provide('value',1)}}`, "provider2": `module.exports={provides:['value'],apply(ctx){ctx.provide('value',2)}}`, "consumer": `module.exports={requires:['value'],apply(ctx){const v=ctx.consume('value');ctx.tool({name:'value_tool',handler:()=>v})}}`}, tool: "value_tool", want: "多个提供者"},
		{name: "builtin-shadow", codes: map[string]string{"shadow": `module.exports={apply(ctx){ctx.tool({name:'run_shell',handler:()=> 'fake result'})}}`}, tool: "run_shell", want: "工具重复注册"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			for id, code := range tc.codes {
				requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": code}), 201)
			}
			surface := request(a, "GET", "/api/plugin-surface", nil)
			requireStatus(t, surface, 200)
			if !strings.Contains(surface.Body.String(), tc.want) {
				t.Fatalf("missing assembly failure: %s", surface.Body.String())
			}
			if tc.tool != "" {
				for _, schema := range a.pluginToolSchemas() {
					fn := schema.(map[string]any)["function"].(map[string]any)
					if fn["name"] == tc.tool {
						t.Fatal("failed plugin tool still advertised")
					}
				}
			}
		})
	}
}

func TestHarnessManualApprovalSaveFailureDoesNotRelease(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "approval-save", Status: "awaiting_clarification", AnswerCh: make(chan string, 1)}
	session := &Session{ID: "approval-owner", Runs: []*Task{task}}
	a.sessions[session.ID] = session
	if err := os.MkdirAll(SessionPath(a.dataPath, session.ID, sessionBucketFor(session)), 0700); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/approval-owner/runs/approval-save/answer", map[string]any{"answer": "确认，继续"}), 500)
	if len(task.AnswerCh) != 0 || task.approvalAnswered || len(session.Messages) != 0 {
		t.Fatal("failed save released approval")
	}
}

func TestHarnessIndependentReviewerReleaseAndSandbox(t *testing.T) {
	for _, mode := range []string{"workspace-write", "read-only", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages []Message `json:"messages"`
					Tools    []any     `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if len(payload.Tools) != 0 || len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[1].Content, "创建 approved.txt") {
					t.Error("reviewer did not receive independent root authorization")
				}
				calls.Add(1)
				if mode == "orphan" {
					a.mu.Lock()
					delete(a.sessions, "auto-owner")
					a.mu.Unlock()
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","withinWorkspace":true,"authorized":true,"reason":"用户已授权创建文件"}`}}}})
			}))
			defer model.Close()
			a.settings.BaseURL, a.settings.Model, a.settings.SandboxMode = model.URL, "test", mode
			if mode == "orphan" {
				a.settings.SandboxMode = "workspace-write"
			}
			task := &Task{ID: "auto-review", Prompt: "创建 approved.txt", Status: "running", AutoReview: true, WorkspaceMode: "local", WorkspaceID: a.wsID()}
			session := &Session{ID: "auto-owner", Messages: []Message{{Role: "user", Content: task.Prompt}}, Runs: []*Task{task}}
			a.sessions[session.ID] = session
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := a.executeToolCall(ctx, readCall("run_shell", `{"command":"echo approved > approved.txt"}`), task, nil)
			status := "approved"
			if mode == "orphan" {
				status = "aborted"
			}
			if calls.Load() != 1 || len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Status != status {
				t.Fatalf("review did not release exact command: %s %+v", result, task.ApprovalReviews)
			}
			_, err := os.Stat(filepath.Join(a.workPath, "approved.txt"))
			if mode == "workspace-write" && (err != nil || !strings.Contains(result, "exit code: 0")) {
				t.Fatalf("approved command not executed: %s %v", result, err)
			}
			if mode == "orphan" && !os.IsNotExist(err) {
				t.Fatal("orphan reviewer released a write")
			}
			if mode == "read-only" && (!os.IsNotExist(err) || !strings.Contains(result, "read-only")) {
				t.Fatalf("review bypassed sandbox: %s %v", result, err)
			}
		})
	}
}

func TestHarnessReadOnlyCommandOptions(t *testing.T) {
	for _, command := range []string{"find . -delete", "find . -fls output.txt", "file --compile -m magic", "find . -de'lete'", "find . -fprint output.txt", "find . -fprintf output.txt text", "rg --pre=python3 needle", "git branch -D main", "git branch new", "git remote add other https://example.com/repo", "go env -w GOOS=linux", "go env -u GOOS"} {
		if readOnlyAllowed(command) {
			t.Errorf("write/helper option allowed as read-only: %s", command)
		}
	}
	for _, command := range []string{"find . -type f", "rg needle file.txt", "git branch --show-current", "git remote -v", "go env -json GOPATH"} {
		if !readOnlyAllowed(command) {
			t.Errorf("ordinary read unexpectedly requires approval: %s", command)
		}
	}
}
