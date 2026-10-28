package server

import (
	"context"
	"strings"
	"testing"
)

func TestAssistantNaturalDispatchStyle(t *testing.T) {
	if got := assistantStartedReply("查询北京明天气温"); got != "好，我去看一下，查到就告诉你。" {
		t.Fatal(got)
	}
	a := testApp(t)
	id := assistantSessionID(t, a)
	a.mu.Lock()
	a.recordAgenticExchangeLocked("我说北京", assistantDecision{Action: "dispatch", DispatchText: "查北京天气", Reply: assistantStartedReply("查询北京天气")}, "text")
	last := a.sessions[id].Messages[len(a.sessions[id].Messages)-1]
	a.mu.Unlock()
	if last.Content != "好，我去看一下，查到就告诉你。" || strings.Contains(last.Content, "转交") {
		t.Fatal(last.Content)
	}
}

func TestAssistantConciseResultKeepsFacts(t *testing.T) {
	run := &Task{Steps: []Step{{Name: "chat", Content: "任务已交付，无需补充执行。\n**结果**：北京明天最低10℃，最高17℃；以多云为主。\n**依据**：工具记录1，API。\n**缺口**：未核对。"}}}
	event := assistantWatchEvent{Status: "completed", Text: "任务 #65「北京天气」已完成。"}
	got := assistantResultText(event, run)
	if !strings.HasPrefix(got, "查到了，北京明天最低10℃") || strings.Contains(got, "工具记录") || strings.Contains(got, "任务已交付") || len([]rune(got)) > 260 {
		t.Fatal(got)
	}
	// Presentation changes must not cause an old result receipt to be invalidated.
	if !strings.Contains(assistantResultEvidenceText(event, run), "工具记录1") {
		t.Fatal("source evidence lost")
	}
}

func TestMixSoulUnverifiedDispatchRetriesWithTool(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	model := agenticScriptServer(t, [][2]any{{"已转交 aide：改为查询北京明天的天气。", nil}, {"", []ToolCall{asstToolCall("dispatch_to_aide", `{"summary":"查询北京明天的天气","mode":"queue"}`)}}})
	defer model.Close()
	got, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, "我说北京", "", "text")
	if err != nil || got.Action != "dispatch" || !strings.Contains(got.DispatchText, "北京") || !strings.Contains(strings.Join(got.ToolsUsed, ","), "dispatch_to_aide") {
		t.Fatalf("decision=%+v err=%v", got, err)
	}
}
