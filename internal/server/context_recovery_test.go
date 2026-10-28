package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestContextRecoveryRetainsUncompactedHistory(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "recovery", Models: []ModelRef{{ID: "recovery", ContextWindow: 262144}}}
	s := &Session{ID: "recovery", Messages: []Message{{Role: "user", Content: "EARLY CONSTRAINT", SourceRunID: "original-run", CreatedAt: "2026-10-28T18:00:00Z"}, {Role: "assistant", Content: strings.Repeat("x", 65000)}}}
	before := cloneCompactMessages(s.Messages)
	p := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 512}, true)
	if p.Breakdown.HistoryMessages != 2 || !strings.Contains(p.Messages[1].Content, "EARLY CONSTRAINT") {
		t.Fatalf("uncompacted history disappeared: count%d", p.Breakdown.HistoryMessages)
	}
	if !reflect.DeepEqual(before, s.Messages) {
		t.Fatal("preview mutated source")
	}
	a.settings.Models[0].ContextWindow = 32768
	p = a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{MaxTokens: 20000}, true)
	if !p.OverLimit || p.Breakdown.HistoryMessages != 2 {
		t.Fatal("overflow hidden instead of reported")
	}
}
func TestContextRecoveryCompactionKeepsToolExchange(t *testing.T) {
	a := testApp(t)
	c1, c2 := testToolCall("read_file", `{"path":"one"}`), testToolCall("read_file", `{"path":"two"}`)
	c2.ID = "call-2"
	s := &Session{Messages: []Message{{Role: "user", Content: strings.Repeat("older", 1800)}, {Role: "assistant", ToolCalls: []ToolCall{c1, c2}, Content: strings.Repeat("thinking", 800)}, {Role: "tool", ToolCallID: c1.ID, Content: "one"}, {Role: "tool", ToolCallID: c2.ID, Content: "two"}, {Role: "assistant", Content: "answer"}}}
	snap, split := a.snapshotForCompactBudget(s, 1024)
	if split != 1 || len(snap.messages) != 1 {
		t.Fatalf("split%d separates call/result exchange", split)
	}
}
func TestContextRecoverySummarySeesMiddleContent(t *testing.T) {
	a := testApp(t)
	markers := []string{"MIDDLE-CONTENT-580", "MIDDLE-ARGS-580"}
	var seen [2]bool
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		total := 0
		for _, m := range body.Messages {
			total += contextMessageTokens(m)
			for i, marker := range markers {
				if strings.Contains(m.Content, marker) {
					seen[i] = true
				}
			}
		}
		if total > compactSummaryInputBudget+500 {
			t.Errorf("oversized map/reduce request: %d", total)
		}
		requests.Add(1)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"ledger","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	call := testToolCall("read_file", strings.Repeat("a", 10000)+markers[1]+strings.Repeat("b", 10000))
	source := []Message{{Role: "assistant", Content: strings.Repeat("x", 10000) + markers[0] + strings.Repeat("y", 10000), ToolCalls: []ToolCall{call}}}
	before := cloneCompactMessages(source)
	_, e := a.buildCompactionSummary(context.Background(), source, Settings{BaseURL: provider.URL, Model: "fixture"}, "")
	if e != nil {
		t.Fatal(e)
	}
	if !seen[0] || !seen[1] || requests.Load() < 2 {
		t.Fatalf("source never reached summarizer: %v requests%d", seen, requests.Load())
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("summary mutated source")
	}
}
func TestContextRecoveryStreamEOFIsNotSuccess(t *testing.T) {
	for _, terminal := range []string{"", "data: [DONE]\n\n", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"} {
		t.Run(terminal, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" + terminal))
			}))
			defer provider.Close()
			text, _, _, _, e := completeStream(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, nil, ProfileParams{}, nil, nil, nil, nil)
			if text != "partial" || (terminal == "" && !errors.Is(e, io.ErrUnexpectedEOF)) || (terminal != "" && e != nil) {
				t.Fatalf("text%q terminal%q err%v", text, terminal, e)
			}
		})
	}
}
func TestContextRecoveryCompactionInvalidatesOldUsageAnchor(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "fixture", Models: []ModelRef{{ID: "fixture", ContextWindow: 65536}}}
	s := &Session{ID: "calibration", Messages: []Message{{Role: "user", Content: "question"}}}
	base := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{}, true)
	s.Runs = []*Task{{ID: "run", ContextAnchor: &ContextUsageAnchor{Usage: TokenUsage{Prompt: 50000}, PromptEstimate: 100, HeaderFingerprint: contextHeaderFingerprint(a.settings, base.Messages[0].Content, base.Tools)}}}
	before := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{}, false)
	if before.Breakdown.UsageCalibration == 0 {
		t.Fatal("control calibration missing")
	}
	s.Compact = "short summary"
	s.CompactedMessages = 10
	s.CompactedAt = "2026-10-28T18:00:00Z"
	after := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{}, false)
	if after.Breakdown.UsageCalibration != 0 {
		t.Fatalf("stale generation calibration%d", after.Breakdown.UsageCalibration)
	}
	// Even if a request completed after the generation changed, its old input
	// cannot calibrate the new summary. A genuinely new request can calibrate.
	anchor := s.Runs[0].ContextAnchor
	anchor.CompactedAt = s.CompactedAt
	stale := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{}, true)
	if stale.Breakdown.UsageCalibration != 0 {
		t.Fatal("concurrent old input calibrated new summary")
	}
	anchor.SummaryFingerprint = contextSummaryFingerprint(stale.Messages)
	fresh := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, ProfileParams{}, false)
	if fresh.Breakdown.UsageCalibration == 0 {
		t.Fatal("new generation calibration lost")
	}

}
func TestContextRecoveryActiveLoopStopsBeforeOverflow(t *testing.T) {
	a := testApp(t)
	var requests atomic.Int32
	partial := strings.Repeat("Z", 18000)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": Message{Role: "assistant", Content: partial}}}})
	}))
	defer provider.Close()
	a.settings.Models = []ModelRef{{ID: "fixture", ContextWindow: 4096}}
	task := &Task{ID: newID(), Prompt: "write", Mode: "chat", Status: "running", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
	registerHarnessFixtureTask(t, a, task)
	out, chain, e := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 128}, nil, task, nil, 0)
	if e == nil || requests.Load() != 1 || !strings.Contains(out, partial) || len(chain) < 2 {
		t.Fatalf("overflow not stopped with partial retained: requests%d outLen%d chain%d err%v", requests.Load(), len(out), len(chain), e)
	}
}

func TestContextRecoveryTruncatedToolCallNeverDispatches(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"owned","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}` + "\n\n"))
	}))
	defer provider.Close()
	_, calls, usage, _, err := completeStream(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, nil, ProfileParams{}, nil, nil, nil, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) || len(calls) != 0 || usage.Total == 0 {
		t.Fatalf("truncated calls escaped or usage lost: calls%d usage%d err%v", len(calls), usage.Total, err)
	}
}
