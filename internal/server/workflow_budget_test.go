package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestToolLoopAllowsMoreThan24Calls(t *testing.T) {
	a := testApp(t)
	calls := make([]ToolCall, 26)
	for i := range calls {
		calls[i] = readCall("list_files", fmt.Sprintf(`{"path":"missing-%d"}`, i))
		calls[i].ID = fmt.Sprintf("call-%d", i)
	}
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) { return "", calls },
		func() (string, []ToolCall) { return "排查完成", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", ToolMaxRounds: 60}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "连续检查工作目录"}), 202)
	waitTaskDone(t, a, s.ID)
	response := request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	if err := json.Unmarshal(response.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	run := sess.Runs[0]
	if run.Status != "completed" || len(run.ToolUses) != 26 {
		t.Fatalf("24 calls still blocked: status=%s calls=%d content=%s", run.Status, len(run.ToolUses), run.Steps[0].Content)
	}
}

func TestToolLoopStopsIdenticalRemoteProbesWithExplanation(t *testing.T) {
	a := testApp(t)
	calls := make([]ToolCall, 12)
	for i := range calls {
		calls[i] = readCall("list_files", `{"path":"."}`)
		calls[i].ID = fmt.Sprintf("probe-%d", i)
	}
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) { return "", calls },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", ToolMaxRounds: 60}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "检查目录"}), 202)
	waitTaskDone(t, a, s.ID)
	response := request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	if err := json.Unmarshal(response.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	run := sess.Runs[0]
	if run.Status != "completed" || len(run.ToolUses) != 8 {
		t.Fatalf("repeat guard status=%s calls=%d", run.Status, len(run.ToolUses))
	}
	if !strings.Contains(run.ToolUses[2].Result, "相同工具、参数和结果已出现 3 次") || !strings.Contains(run.Steps[0].Content, "重复 8 次") {
		t.Fatalf("repeat warning or explanation missing: %s / %s", run.ToolUses[2].Result, run.Steps[0].Content)
	}
}

func TestToolCallBudgetFollowsConfiguredRounds(t *testing.T) {
	if got := maxToolCallsForRounds(60); got != 240 {
		t.Fatalf("default call budget=%d", got)
	}
	if got := maxToolCallsForRounds(5); got != 64 {
		t.Fatalf("minimum call budget=%d", got)
	}
	if got := maxToolCallsForRounds(200); got != 800 {
		t.Fatalf("maximum configured call budget=%d", got)
	}
}
