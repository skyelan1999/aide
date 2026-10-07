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
)

func TestModelLedWorkflowCompatibility(t *testing.T) {
	for _, tc := range []struct {
		task Task
		want bool
	}{
		{Task{Mode: "workflow", Strategy: "auto"}, true},
		{Task{Mode: "workflow", Strategy: "auto", WorkflowPhase: "auto"}, true},
		{Task{Mode: "workflow", Strategy: "manual"}, false},
		{Task{Mode: "chat", Strategy: "auto"}, false},
		{Task{Mode: "workflow", Strategy: "auto", WorkflowPhase: "propose"}, false},
		{Task{Mode: "workflow", Strategy: "auto", Steps: []Step{{Name: "plan"}}}, false},
	} {
		if got := modelLedWorkflow(&tc.task); got != tc.want {
			t.Fatalf("%+v: got %v", tc.task, got)
		}
	}
}

func TestModelLedPlanValidationAndJSONPersistence(t *testing.T) {
	a := testApp(t)
	task := &Task{ToolUses: []ToolUse{{Tool: "read_file", Result: "observed"}}}
	valid := `{"plan":[{"step":"核对","status":"completed","evidence":[1]},{"step":"整理","status":"in_progress"}]}`
	if got := a.updateAgentPlan(task, valid); !strings.Contains(got, "计划已更新") {
		t.Fatal(got)
	}
	for _, raw := range []string{`{}`, `{"plan":[{"step":"x","status":"bad"}]}`, `{"plan":[{"step":"x","status":"completed","evidence":[2]}]}`, `{"plan":[{"step":"x","status":"in_progress"},{"step":"y","status":"in_progress"}]}`} {
		if got := a.updateAgentPlan(task, raw); !strings.HasPrefix(got, "错误") {
			t.Fatal(got)
		}
	}
	task.AgentReviewDone = true
	task.WorkflowPhase = "auto"
	raw, _ := json.Marshal(task)
	var restored Task
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.AgentPlan == nil || len(restored.AgentPlan.Items) != 2 || !restored.AgentReviewDone || restored.WorkflowPhase != "auto" {
		t.Fatalf("persistence lost: %+v", restored)
	}
	if got := a.agentCompletionReview(&restored); !strings.Contains(got, "工具记录1 read_file 参数：") || !strings.Contains(got, "返回：observed") {
		t.Fatal(got)
	}
}

func TestModelLedLoopPlansReceiptsAndSingleReview(t *testing.T) {
	a := testApp(t)
	var count atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		n := count.Add(1)
		msg := Message{Role: "assistant", Content: "已完成"}
		switch n {
		case 1:
			msg.Content = ""
			msg.ToolCalls = []ToolCall{readCall("update_plan", `{"plan":[{"step":"整理目标","status":"in_progress"}]}`)}
		case 2:
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "[工具记录1]") {
				t.Errorf("missing receipt: %+v", last)
			}
			msg.Content = ""
			msg.ToolCalls = []ToolCall{readCall("read_file", `{"path":"review.txt"}`)}
		case 3:
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "observed") {
				t.Errorf("missing actual read result: %+v", last)
			}
		case 4:
			last := body.Messages[len(body.Messages)-1]
			if !strings.Contains(last.Content, "任务收尾自检") || !strings.Contains(last.Content, "当前计划") {
				t.Errorf("missing review: %+v", last)
			}
		case 5: // resume final response
		default:
			t.Errorf("unexpected request %d", n)
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
	defer provider.Close()
	a.settings.ToolMaxRounds = 6
	if err := os.WriteFile(filepath.Join(a.workPath, "review.txt"), []byte("observed"), 0600); err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: newID(), Mode: "workflow", Strategy: "auto", Steps: []Step{{Name: "agent"}}, Steer: make(chan string, 1)}
	registerHarnessFixtureTask(t, a, task)
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: "整理目标"}}, ProfileParams{}, append(append([]any{}, builtinTools...), updatePlanTool), task, nil, 0)
	if err != nil || out != "已完成" {
		t.Fatalf("%q %v", out, err)
	}
	if count.Load() != 4 || !task.AgentReviewDone || task.AgentPlan == nil || len(task.ToolUses) != 2 {
		t.Fatalf("unexpected state: calls=%d task=%+v", count.Load(), task)
	}
	// A resumed chain retains review state and does not add another review.
	registerHarnessFixtureTask(t, a, task)
	_, _, err = a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, chain, ProfileParams{}, nil, task, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count.Load() != 5 {
		t.Fatalf("repeat review: %d", count.Load())
	}
}
