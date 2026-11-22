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

func TestPendingProposalReviewYieldsWithoutLosingFutureWork(t *testing.T) {
	task := &Task{
		Files:     []Change{{Path: "generator.py", Content: "print('fixture')\n"}},
		ToolUses:  []ToolUse{{Tool: "write_file", Result: "candidate only"}},
		AgentPlan: &AgentPlan{Items: []AgentPlanItem{{Step: "apply then execute", Status: "pending"}}},
	}
	if !agentReviewNeeded(task) {
		t.Fatal("pending source lost its initial completion review")
	}
	task.AgentReviewDone, task.AgentReviewCount = true, 1
	task.AgentReviewToolCount = agentExecutionCount(task)
	task.ToolUses = append(task.ToolUses, ToolUse{Tool: "read_proposal"}, ToolUse{Tool: "update_plan"})
	if agentReviewNeeded(task) {
		t.Fatal("unchanged pending proposal and future plan caused a repeated review")
	}
	task.ToolUses = append(task.ToolUses, ToolUse{Tool: "write_file", Result: "revised candidate"})
	if !agentReviewNeeded(task) {
		t.Fatal("candidate revision bypassed its completion review")
	}
	// An older saved counter already includes candidate reads. New real work
	// must still trigger review without migrating or resetting that counter.
	task.AgentReviewToolCount = agentExecutionCount(task)
	task.ToolUses = append(task.ToolUses, ToolUse{Tool: "read_proposal"}, ToolUse{Tool: "run_shell", Result: "new check"})
	if !agentReviewNeeded(task) {
		t.Fatal("new execution after a legacy counter was not reviewed")
	}
	task.AgentReviewToolCount = agentExecutionCount(task)
	task.Files[0].Applied = true
	if !agentReviewNeeded(task) {
		t.Fatal("applied files incorrectly suppressed unfinished authorized work")
	}
	task.Files[0].HistoryPending = true
	if agentReviewNeeded(task) {
		t.Fatal("pending history application gate caused redundant review")
	}
}

func TestPendingProposalCompletionHandoffLoop(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Prompt: "Prepare source for normal application before executing it", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1), WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		Files:     []Change{{Path: "generator.py", Content: "print('fixture')\n"}},
		ToolUses:  []ToolUse{{Tool: "write_file", Result: "candidate only"}},
		AgentPlan: &AgentPlan{Items: []AgentPlanItem{{Step: "execute after application", Status: "blocked"}, {Step: "finish assembly", Status: "pending"}}},
	}
	registerHarnessFixtureTask(t, a, task)
	const answer = "源码候选已准备，等待正常应用；实际执行与整体任务尚未完成。"
	count := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		count++
		msg := Message{Role: "assistant", Content: answer}
		if count == 2 {
			last := body.Messages[len(body.Messages)-1]
			if !strings.Contains(last.Content, "文件应用审批") || !strings.Contains(last.Content, "不表示用户总目标已完成") {
				t.Errorf("lost application boundary: %s", last.Content)
			}
			msg.Content = ""
			msg.ToolCalls = []ToolCall{readCall("read_proposal", `{"path":"generator.py"}`)}
		}
		if count > 3 {
			t.Errorf("repeated completion request %d while application is pending", count)
		}
		if count == 3 {
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "print('fixture')") || !strings.Contains(last.Content, `"applied":false`) {
				t.Errorf("candidate inspection did not return the pending source: %s", last.Content)
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
	defer provider.Close()
	out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, builtinTools, task, nil, 0)
	if err != nil || out != answer || count != 3 || task.AgentReviewCount != 1 {
		t.Fatalf("out=%q calls=%d reviews=%d err=%v", out, count, task.AgentReviewCount, err)
	}
	if task.Files[0].Applied || task.AgentPlan.Items[1].Status != "pending" {
		t.Fatal("handoff applied a file or completed the overall goal")
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "generator.py")); !os.IsNotExist(err) {
		t.Fatalf("handoff unexpectedly wrote candidate: %v", err)
	}
}
