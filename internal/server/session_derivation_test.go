package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func deriveRequest(t *testing.T, a *App, sessionID, kind string) (*httptest.ResponseRecorder, Session) {
	t.Helper()
	resp := request(a, "POST", "/api/sessions/"+sessionID+"/derive", map[string]any{"kind": kind})
	var derived Session
	if err := json.Unmarshal(resp.Body.Bytes(), &derived); err != nil {
		t.Fatalf("decode derive response: %v; body=%s", err, resp.Body.String())
	}
	return resp, derived
}

func TestDeriveSessionCopiesContextWithoutSharingRuns(t *testing.T) {
	a := testApp(t)
	source := &Session{
		ID: "parent", Title: "上下文来源", Compact: "压缩摘要", CompactedMessages: 2,
		Messages: []Message{{Role: "user", Content: "原始问题", SourceRunID: "old-run"}, {Role: "assistant", Content: "原始回答", SourceRunID: "old-run"}},
		Runs:     []*Task{{ID: "old-run", Prompt: "原始问题", Status: "completed", Steps: []Step{{Name: "chat", Content: "原始回答"}}}},
	}
	a.sessions[source.ID] = source
	status, branch := deriveRequest(t, a, source.ID, "session")
	requireStatus(t, status, 201)
	if branch.ID == source.ID || branch.ParentID != "" || branch.Title != "分支: 上下文来源" {
		t.Fatalf("invalid independent branch: %+v", branch)
	}
	if branch.Compact != source.Compact || branch.CompactedMessages != 2 || len(branch.Messages) != 2 || len(branch.Runs) != 1 {
		t.Fatalf("conversation context was not preserved: %+v", branch)
	}
	if branch.Runs[0].ID == source.Runs[0].ID || branch.Messages[0].SourceRunID != branch.Runs[0].ID {
		t.Fatalf("copied run identity was not isolated: %+v", branch.Runs[0])
	}
	if source.Runs[0].ID != "old-run" || source.Messages[0].SourceRunID != "old-run" {
		t.Fatal("derivation mutated source history")
	}
	branch.Runs[0].Steps[0].Content = "分支修改"
	if source.Runs[0].Steps[0].Content != "原始回答" {
		t.Fatal("branch shares run history with source")
	}
}

func TestDeriveSubagentRejectsNestedAgentsAndRunningSource(t *testing.T) {
	a := testApp(t)
	a.sessions["parent"] = &Session{ID: "parent", Title: "主会话", Runs: []*Task{}}
	status, child := deriveRequest(t, a, "parent", "subagent")
	requireStatus(t, status, 201)
	if child.ParentID != "parent" || child.Title != "子: 主会话" {
		t.Fatalf("invalid child identity: %+v", child)
	}
	status, _ = deriveRequest(t, a, child.ID, "subagent")
	requireStatus(t, status, 400)

	child.Runs = []*Task{{ID: "child-run", Status: "completed"}}
	if _, _, err := a.spawnSubagent(child.Runs[0], "nested task", ""); err == nil {
		t.Fatal("sub-agent spawned a nested sub-agent through the tool path")
	}
	bindTaskToSessionLineage(&child, child.Runs[0])
	if child.Runs[0].ParentSessionID != "parent" || child.Runs[0].ChildSessionID != child.ID || child.Runs[0].AgentDepth != 1 {
		t.Fatalf("manual child run lineage missing: %+v", child.Runs[0])
	}
	child.Runs[0].AgentToolsSet = true
	child.Runs[0].AgentTools = []string{"read_file", "spawn_subagent"}
	if !taskToolDenied(child.Runs[0], "spawn_subagent") || taskToolDenied(child.Runs[0], "read_file") {
		t.Fatal("nested spawn tool was not denied without restricting ordinary child tools")
	}

	for _, activeStatus := range []string{"running", "awaiting_approval", "awaiting_clarification", "paused"} {
		a.sessions["active"] = &Session{ID: "active", Title: "未结束", Runs: []*Task{{ID: "active-run", Status: activeStatus}}}
		status, _ = deriveRequest(t, a, "active", "session")
		requireStatus(t, status, 409)
	}
}
