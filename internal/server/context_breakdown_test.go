package server

import (
	"encoding/json"
	"testing"
)

func testToolCall(name, arguments string) ToolCall {
	var call ToolCall
	call.ID = "call-1"
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

func TestContextBreakdownSeparatesRequestPartsAndSums(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "test-model", Models: []ModelRef{{ID: "test-model", ContextWindow: 32768}}}
	s := &Session{ID: "session-1", Messages: []Message{
		{Role: "user", Content: "earlier question"},
		{Role: "assistant", Content: "I will inspect it", ToolCalls: []ToolCall{testToolCall("read_file", `{"path":"a.txt"}`)}},
		{Role: "tool", ToolCallID: "call-1", Content: "tool output body"},
		{Role: "assistant", Content: "earlier answer"},
	}}
	preview := a.buildContextPreview(s, "new question", "chat", "<untrusted-file root=\"context\" path=\"brief.md\">reference</untrusted-file>", nil, a.settings, ProfileParams{MaxTokens: 512}, true)
	bd := preview.Breakdown

	if bd.System.Tokens == 0 || bd.HistoryUser.Tokens == 0 || bd.HistoryAssistant.Tokens == 0 || bd.ToolCalls.Tokens == 0 || bd.ToolResults.Tokens == 0 || bd.Prompt.Tokens == 0 || bd.Attachments.Tokens == 0 || bd.ToolSchemas.Tokens == 0 || bd.Protocol.Tokens == 0 {
		t.Fatalf("missing request category: %#v", bd)
	}
	if bd.HistoryMessages != 4 || bd.ToolCallCount != 1 || bd.AttachmentFiles != 1 || bd.MessageCount != len(preview.Messages) {
		t.Fatalf("unexpected counters: %#v", bd)
	}
	if got := bd.price(); got != preview.InputEstimate {
		t.Fatalf("breakdown=%d, preview=%d", got, preview.InputEstimate)
	}
	if preview.TotalEstimate != preview.InputEstimate+preview.OutputReserve {
		t.Fatalf("total=%d input=%d reserve=%d", preview.TotalEstimate, preview.InputEstimate, preview.OutputReserve)
	}
}

func TestContextPreviewBaselineAllowsEmptyPromptWithoutRequestBody(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "test-model", Models: []ModelRef{{ID: "test-model", ContextWindow: 32768}}}
	s := &Session{ID: "session-1", Messages: []Message{{Role: "user", Content: "previous input"}}}
	a.sessions[s.ID] = s
	w := request(a, "POST", "/api/context-preview", map[string]any{"sessionId": s.ID, "prompt": "", "mode": "chat", "baseline": true})
	requireStatus(t, w, 200)
	var preview ContextPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.InputEstimate == 0 || len(preview.Messages) != 0 || len(preview.Tools) != 0 {
		t.Fatalf("baseline should contain a metered request without returning request body: %#v", preview)
	}
	if preview.Breakdown.HistoryUser.Tokens == 0 || preview.Breakdown.Protocol.Tokens == 0 {
		t.Fatalf("baseline omitted history/protocol: %#v", preview.Breakdown)
	}
}

func TestContextPreviewAccumulatesHistoryAndReflectsCompaction(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "test-model", Models: []ModelRef{{ID: "test-model", ContextWindow: 32768}}}
	short := &Session{ID: "session-1", Messages: []Message{{Role: "user", Content: "first question"}}}
	shortPreview := a.buildContextPreview(short, "second question", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 512}, false)
	long := &Session{ID: "session-1", Messages: []Message{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "a detailed prior answer with decisions and explanation"},
		{Role: "user", Content: "another question"},
	}}
	longPreview := a.buildContextPreview(long, "second question", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 512}, false)
	if longPreview.InputEstimate <= shortPreview.InputEstimate {
		t.Fatalf("activity context should include earlier conversation: short=%d long=%d", shortPreview.InputEstimate, longPreview.InputEstimate)
	}
	long.Compact = "Summary of the earlier questions and decisions."
	long.CompactedMessages = 2
	long.Messages = []Message{{Role: "user", Content: "another question"}}
	compactedPreview := a.buildContextPreview(long, "second question", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 512}, false)
	if compactedPreview.InputEstimate >= longPreview.InputEstimate {
		t.Fatalf("compaction should reduce active context while retaining its summary: before=%d after=%d", longPreview.InputEstimate, compactedPreview.InputEstimate)
	}
}

func TestSessionHistoryPageIncludesLifetimeUsageAcrossAllRuns(t *testing.T) {
	s := &Session{Runs: []*Task{
		{Usage: TokenUsage{Prompt: 100, Completion: 20, Total: 120}},
		{Usage: TokenUsage{Prompt: 200, Completion: 30, Total: 230, Estimated: true}},
	}}
	view := makeSessionHistoryView(s, 1)
	if len(view.Runs) != 1 || view.RunsTotal != 2 {
		t.Fatalf("runs should be paginated while preserving total metadata: %#v", view)
	}
	if view.SessionUsage.Prompt != 300 || view.SessionUsage.Completion != 50 || view.SessionUsage.Total != 350 || !view.SessionUsage.Estimated {
		t.Fatalf("session usage must cover all runs and mark estimates: %#v", view.SessionUsage)
	}
}
