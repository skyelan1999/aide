package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentReviewUnfinishedWritingPlan(t *testing.T) {
	task := &Task{AgentPlan: &AgentPlan{Items: []AgentPlanItem{{Step: "写完40节文档", Status: "in_progress"}}}}
	if !agentReviewNeeded(task) {
		t.Fatal("unfinished writing plan was treated as a final delivery without a completion check")
	}
	task.AgentReviewDone = true
	task.AgentReviewCount = 1
	if !agentReviewNeeded(task) {
		t.Fatal("unfinished plan must allow another bounded check without fabricated tool executions")
	}
	task.AgentReviewCount = taskExecutionPolicy(task).CompletionReviews
	if agentReviewNeeded(task) {
		t.Fatal("unfinished plan exceeded completion review budget")
	}
	for _, status := range []string{"completed", "blocked"} {
		task.AgentReviewDone = false
		task.AgentReviewCount = 0
		task.AgentPlan.Items[0].Status = status
		if agentReviewNeeded(task) {
			t.Fatalf("%s plan with no execution should not add a review", status)
		}
	}
	if agentReviewNeeded(&Task{}) {
		t.Fatal("simple answer without tools or plan should not acquire a review")
	}
}

func TestCompletionReviewPreservesOriginalDelivery(t *testing.T) {
	a := testApp(t)
	task := &Task{Prompt: "只回答文件中的Marker", ToolUses: []ToolUse{{Tool: "read_file", Args: `{"path":"fixture.txt"}`, Result: "Marker: TEST"}}}
	review := a.agentCompletionReview(task)
	for _, want := range []string{task.Prompt, "不要把本次自检当作新的用户请求", "不要重复读取", "直接面向原用户交付"} {
		if !strings.Contains(review, want) {
			t.Fatalf("completion review lost delivery instruction %q", want)
		}
	}
}

func TestUnfinishedWritingPlanContinuesThroughReview(t *testing.T) {
	a := testApp(t)
	count := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		count++
		msg := Message{Role: "assistant"}
		switch count {
		case 1:
			msg.ToolCalls = []ToolCall{readCall("update_plan", `{"plan":[{"step":"交付两节正文","status":"in_progress"}]}`)}
		case 2:
			msg.Content = "第1节：已交付第一部分，继续第二部分。"
		case 3:
			last := body.Messages[len(body.Messages)-1]
			if !strings.Contains(last.Content, "任务收尾自检") || !strings.Contains(last.Content, "写两节正文") {
				t.Errorf("missing original writing goal: %s", last.Content)
			}
			msg.ToolCalls = []ToolCall{readCall("update_plan", `{"plan":[{"step":"交付两节正文","status":"completed"}]}`)}
		case 4:
			msg.Content = "第2节：第二部分完整正文。"
		default:
			t.Errorf("unexpected request %d", count)
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
	defer provider.Close()
	task := &Task{ID: newID(), Prompt: "写两节正文", Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
	registerHarnessFixtureTask(t, a, task)
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, []any{updatePlanTool}, task, nil, 0)
	if err != nil || count != 4 || out != "第2节：第二部分完整正文。" || task.AgentReviewCount != 1 {
		t.Fatalf("out=%q calls=%d reviews=%d err=%v", out, count, task.AgentReviewCount, err)
	}
	found := false
	for _, m := range chain {
		if strings.Contains(m.Content, "第1节：") {
			found = true
		}
	}
	if !found {
		t.Fatal("earlier partial delivery disappeared from conversation")
	}
}
