package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// 构造父会话 + 已归档子会话，写入内存存储，返回父会话 ID。
func seedAggregationSessions(t *testing.T, a *App) string {
	t.Helper()
	parent := &Session{
		ID:      "parent-1",
		Title:   "父会话",
		Created: "2026-09-26T10:00:00Z",
		Runs: []*Task{
			{
				ID: "p-run-1", Created: "2026-09-26T10:00:05Z", Status: "completed",
				ToolUses: []ToolUse{
					{Tool: "read_file", Args: `{"path":"a.go"}`, Result: "file content", Preview: "file content", Who: "main"},
					{Tool: "spawn_subagent", Args: `{"task":"子任务"}`, Result: "子会话已创建: sub-1", Preview: "子会话已创建: sub-1", Who: "main"},
				},
			},
		},
	}
	child := &Session{
		ID:       "sub-1",
		Title:    "子: 干活",
		Number:   7,
		ParentID: "parent-1",
		Created:  "2026-09-26T10:01:00Z",
		Archived: true, // 子会话完成后自动归档
		Runs: []*Task{
			{
				ID: "c-run-1", Created: "2026-09-26T10:01:10Z", Status: "completed",
				ToolUses: []ToolUse{
					{Tool: "list_files", Result: "ok", Preview: "ok", Who: "sub", ChildSessionID: "sub-1", ChildNumber: 7, ChildTitle: "子: 干活"},
					{Tool: "run_shell", Result: "⚠ 命令执行失败", Preview: "⚠ 命令执行失败", Who: "sub", ChildSessionID: "sub-1", ChildNumber: 7, ChildTitle: "子: 干活"},
				},
			},
		},
	}
	a.mu.Lock()
	a.sessions["parent-1"] = parent
	a.sessions["sub-1"] = child
	a.mu.Unlock()
	return "parent-1"
}

// TestToolCallsAggregateMainAndSub：父会话调用记录包含主 Agent（含 spawn_subagent）与子 Agent 调用。
func TestToolCallsAggregateMainAndSub(t *testing.T) {
	a := testApp(t)
	pid := seedAggregationSessions(t, a)

	w := request(a, "GET", "/api/sessions/"+pid+"/tool-calls", nil)
	requireStatus(t, w, 200)
	var out toolCallsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 主 2（read_file + spawn_subagent）+ 子 2 = 4
	if out.Stats.MainCount != 2 {
		t.Fatalf("mainCount = %d want 2", out.Stats.MainCount)
	}
	if out.Stats.ChildCount != 2 {
		t.Fatalf("childCount = %d want 2", out.Stats.ChildCount)
	}
	if out.Stats.FailCount != 1 { // run_shell 命中「失败」
		t.Fatalf("failCount = %d want 1", out.Stats.FailCount)
	}
	if len(out.Calls) != 4 {
		t.Fatalf("calls = %d want 4", len(out.Calls))
	}
	// spawn_subagent 归主 Agent
	var spawn *toolCallRecord
	for i := range out.Calls {
		if out.Calls[i].Tool == "spawn_subagent" {
			spawn = &out.Calls[i]
		}
	}
	if spawn == nil || spawn.Who != "main" {
		t.Fatalf("spawn_subagent 未归主 Agent: %+v", spawn)
	}
	// 子调用带会话编号/标题
	for _, c := range out.Calls {
		if c.Who == "sub" {
			if c.ChildSessionID != "sub-1" || c.ChildNumber != 7 || c.ChildTitle != "子: 干活" {
				t.Fatalf("子调用缺归属: %+v", c)
			}
		}
	}
	// 时间线升序：主 run(10:00:05) 先于 子 run(10:01:10)
	if out.Calls[0].Time != "2026-09-26T10:00:05Z" || out.Calls[len(out.Calls)-1].Time != "2026-09-26T10:01:10Z" {
		t.Fatalf("时间线未按时间排序: %+v", out.Calls)
	}
}

// TestToolCallsFilterWho：按 主/子 过滤。
func TestToolCallsFilterWho(t *testing.T) {
	a := testApp(t)
	pid := seedAggregationSessions(t, a)

	for _, tc := range []struct {
		who       string
		main, sub int
	}{
		{"all", 2, 2},
		{"main", 2, 0},
		{"sub", 0, 2},
	} {
		w := request(a, "GET", "/api/sessions/"+pid+"/tool-calls?who="+tc.who, nil)
		requireStatus(t, w, 200)
		var out toolCallsResponse
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if out.Stats.MainCount != tc.main || out.Stats.ChildCount != tc.sub {
			t.Fatalf("who=%s main=%d sub=%d want main=%d sub=%d", tc.who, out.Stats.MainCount, out.Stats.ChildCount, tc.main, tc.sub)
		}
		for _, c := range out.Calls {
			if tc.who == "main" && c.Who != "main" {
				t.Fatalf("who=main 过滤仍含子调用: %+v", c)
			}
			if tc.who == "sub" && c.Who != "sub" {
				t.Fatalf("who=sub 过滤仍含主调用: %+v", c)
			}
		}
	}
}

// TestToolCallsFilterChildSessionId：多子会话时按子会话过滤。
func TestToolCallsFilterChildSessionId(t *testing.T) {
	a := testApp(t)
	pid := seedAggregationSessions(t, a)
	// 再加第二个子会话
	a.mu.Lock()
	a.sessions["sub-2"] = &Session{
		ID: "sub-2", Title: "子: 第二件事", Number: 8, ParentID: pid, Created: "2026-09-26T10:02:00Z",
		Runs: []*Task{{ID: "c2", Created: "2026-09-26T10:02:05Z", ToolUses: []ToolUse{
			{Tool: "read_memory", Result: "mem", Preview: "mem", Who: "sub", ChildSessionID: "sub-2", ChildNumber: 8, ChildTitle: "子: 第二件事"},
		}}},
	}
	a.mu.Unlock()

	w := request(a, "GET", "/api/sessions/"+pid+"/tool-calls?who=sub&childSessionId=sub-2", nil)
	requireStatus(t, w, 200)
	var out toolCallsResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Stats.ChildCount != 1 || len(out.Calls) != 1 {
		t.Fatalf("childSessionId 过滤 = %+v", out)
	}
	if out.Calls[0].ChildSessionID != "sub-2" || out.Calls[0].ChildNumber != 8 {
		t.Fatalf("过滤到错误子会话: %+v", out.Calls[0])
	}
}

// TestToolCallsArchivedChildStillQueriable：子会话归档后父会话仍可查。
func TestToolCallsArchivedChildStillQueriable(t *testing.T) {
	a := testApp(t)
	pid := seedAggregationSessions(t, a) // child.Archived=true
	w := request(a, "GET", "/api/sessions/"+pid+"/tool-calls?who=sub", nil)
	requireStatus(t, w, 200)
	var out toolCallsResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Stats.ChildCount != 2 {
		t.Fatalf("归档子会话调用未聚合: %+v", out)
	}
}

// TestToolCallsLegacyWhoDefaultsMain：历史记录 Who 为空按主 Agent 处理。
func TestToolCallsLegacyWhoDefaultsMain(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.sessions["legacy"] = &Session{
		ID: "legacy", Title: "老会话", Created: "2026-01-01T00:00:00Z",
		Runs: []*Task{{ID: "l1", Created: "2026-01-01T00:00:01Z", ToolUses: []ToolUse{
			{Tool: "read_file", Result: "x", Preview: "x"}, // 无 Who 字段
		}}},
	}
	a.mu.Unlock()
	w := request(a, "GET", "/api/sessions/legacy/tool-calls", nil)
	requireStatus(t, w, 200)
	var out toolCallsResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Stats.MainCount != 1 || out.Stats.ChildCount != 0 {
		t.Fatalf("历史记录归属错误: %+v", out)
	}
}

// TestToolCallsNotFound：不存在会话 404。
func TestToolCallsNotFound(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/api/sessions/nope/tool-calls", nil)
	requireStatus(t, w, 404)
}

// TestToolCallsExportIncludesSubCalls：导出包含子 Agent 调用与归属（who/childSessionId 落盘）。
func TestToolCallsExportIncludesSubCalls(t *testing.T) {
	a := testApp(t)
	_ = seedAggregationSessions(t, a)

	w := request(a, "GET", "/api/export", nil)
	requireStatus(t, w, 200)
	var payload struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("export decode: %v", err)
	}
	var child *Session
	for i := range payload.Sessions {
		if payload.Sessions[i].ID == "sub-1" {
			child = &payload.Sessions[i]
		}
	}
	if child == nil {
		t.Fatal("导出未含子会话 sub-1")
	}
	if len(child.Runs) != 1 || len(child.Runs[0].ToolUses) != 2 {
		t.Fatalf("子会话 run/toolUses 缺失: %+v", child.Runs)
	}
	tu := child.Runs[0].ToolUses[0]
	if tu.Who != "sub" || tu.ChildSessionID != "sub-1" || tu.ChildNumber != 7 {
		t.Fatalf("导出子调用未带归属: %+v", tu)
	}
	if strings.Contains(w.Body.String(), a.token) {
		t.Fatal("导出泄漏访问令牌")
	}
}
