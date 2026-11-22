package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAssistantAutonomyWorkScope(t *testing.T) {
	for _, text := range []string{"今天天气怎么样？", "帮我看看 KDIGO 2024 CKD 指南中文版里面在讲什么。", "查询天津明天的天气", "请读取工作区文件并概括内容", "不要动待审任务，帮我看看工作区指南讲什么"} {
		if !assistantNeedsWorkTool(text) {
			t.Errorf("missing %q", text)
		}
	}
	for _, text := range []string{"你好", "今天心情很好", "不要查今天天气", "假如我说今天天气怎么样", "会话188的审批状态只查询", "我们只聊文件处理的原理", "怎么查天气", "天气查询的原理是什么"} {
		if assistantNeedsWorkTool(text) {
			t.Errorf("unexpected work %q", text)
		}
	}
}

func TestAssistantAutonomyMissingToolRetriesWithoutRawDispatch(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
			a := testApp(t)
			unlockAssistantSessionForTest(t, a)
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "我没有实时天气数据，请自行查看。"}}}})
			}))
			defer model.Close()
			a.mu.Lock()
			before := len(a.sessions)
			a.mu.Unlock()
			_, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, "今天天气怎么样？", "", "text")
			if err == nil || calls.Load() != 2 {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if len(a.sessions) != before {
				t.Fatal("unverified reply created a task")
			}
		})
	}
}

func TestAssistantAutonomyModelDelegatesAfterRepair(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "请把指南发给我。"}}}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"fixture_dispatch","type":"function","function":{"name":"dispatch_to_aide","arguments":"{\"summary\":\"在当前工作区查找并解读 KDIGO 2024 CKD 指南中文版，只读。\"}"}}]}}]}`))
	}))
	defer model.Close()
	d, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, "帮我看看 KDIGO 2024 CKD 指南中文版里面在讲什么。", "", "call")
	if err != nil || d.Action != "dispatch" || d.DispatchText == "" {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected requests %d", calls.Load())
	}
}

func TestAssistantAutonomyPreservePendingIsNotApprovalQuery(t *testing.T) {
	for _, text := range []string{"只读检查readme文件是否存在，不动待审任务，完成后回电话。", "不改待审任务，读取工作区文件", "不批准现有待审任务，检查 README 是否存在"} {
		if assistantApprovalStatusQuery(text) || !assistantNeedsWorkTool(text) {
			t.Errorf("preservation constraint hid file work: %q", text)
		}
	}
	for _, text := range []string{"不动待审任务，查询会话188审批状态", "不要批准，看看188的待审状态"} {
		if !assistantApprovalStatusQuery(text) {
			t.Errorf("lost genuine query %q", text)
		}
	}
}
