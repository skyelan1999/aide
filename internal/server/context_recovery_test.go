package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestContextRecoveryRetainsUncompactedHistory(t *testing.T) {
	a := testApp(t)
	cfg := Settings{Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 131072}}}
	s := &Session{Messages: []Message{{Role: "user", Content: "EARLY_CONSTRAINT_NEVER_DELETE"}, {Role: "assistant", Content: strings.Repeat("x", 64000)}}}
	p := a.buildContextPreview(s, "continue", "chat", "", nil, cfg, ProfileParams{MaxTokens: 512}, true)
	found := false
	for _, m := range p.Messages {
		if strings.Contains(m.Content, "EARLY_CONSTRAINT_NEVER_DELETE") {
			found = true
		}
	}
	if !found || p.Breakdown.HistoryMessages != len(s.Messages) {
		t.Fatal("uncompacted history silently discarded")
	}
}

func TestContextRecoveryCompactionKeepsToolExchange(t *testing.T) {
	a := testApp(t)
	s := &Session{Messages: []Message{{Role: "user", Content: "old question"}, {Role: "assistant", ToolCalls: []ToolCall{testToolCall("read_file", strings.Repeat("x", 8000))}}, {Role: "tool", ToolCallID: "call-1", Content: "result"}, {Role: "user", Content: "new question"}}}
	_, split := a.snapshotForCompactBudget(s, 1024)
	if split > 0 && s.Messages[split].Role == "tool" {
		t.Fatal("compaction retained orphan tool result")
	}
}

func TestContextRecoverySummarySeesMiddleContent(t *testing.T) {
	a := testApp(t)
	seen := false
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		for _, m := range b.Messages {
			if strings.Contains(m.Content, "MIDDLE_CONSTRAINT_731864") {
				seen = true
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"goal":"test","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer p.Close()
	_, err := a.buildCompactionSummary(context.Background(), []Message{{Role: "user", Content: strings.Repeat("a", 8000) + " MIDDLE_CONSTRAINT_731864 " + strings.Repeat("z", 8000)}}, Settings{BaseURL: p.URL, Model: "test"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("source middle discarded before summarizer saw it")
	}
}

func TestContextRecoveryStreamEOFIsNotSuccess(t *testing.T) {
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial answer\"}}]}\n\n"))
	}))
	defer p.Close()
	out, calls, _, _, err := completeStream(context.Background(), Settings{BaseURL: p.URL, Model: "test"}, []Message{{Role: "user", Content: "hi"}}, ProfileParams{}, nil, nil, nil, nil)
	if err == nil || out != "partial answer" || len(calls) != 0 {
		t.Fatal("unterminated stream was treated as a complete answer", err)
	}
}

func TestContextRecoveryActiveBudgetPreservesScopeAndProtocol(t *testing.T) {
	a := testApp(t)
	cfg := Settings{Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 4096}}}
	a.settings = cfg
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"goal":"test","decisions":[],"files":[],"facts":["MIDDLE_CONSTRAINT_731864"],"pending":[]}`}}}})
	}))
	defer p.Close()
	cfg.BaseURL = p.URL
	original := []Message{{Role: "system", Content: "policy"}, {Role: "user", Content: "CURRENT_SCOPE"}, {Role: "assistant", ToolCalls: []ToolCall{testToolCall("read_file", strings.Repeat("x", 9000))}}, {Role: "tool", ToolCallID: "call-1", Content: strings.Repeat("z", 9000)}, {Role: "user", Content: "LATEST_CORRECTION"}}
	task := &Task{ID: newID(), Prompt: "CURRENT_SCOPE"}
	got, e := a.fitActiveContext(context.Background(), cfg, original, ProfileParams{MaxTokens: 512}, nil, task)
	if e != nil {
		t.Fatal(e)
	}
	if estimateProviderPromptTokens(got, nil)+512 > 4096 {
		t.Fatal("candidate still overflows")
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.Content] = true
		if m.Role == "tool" {
			t.Fatal("retained orphan result")
		}
	}
	if !seen["CURRENT_SCOPE"] || !seen["LATEST_CORRECTION"] || len(original[2].ToolCalls) != 1 {
		t.Fatal("scope or source mutated")
	}
}

func TestContextRecoveryActiveFailurePreservesInput(t *testing.T) {
	a := testApp(t)
	cfg := Settings{Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 4096}}}
	a.settings = cfg
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "not JSON"}}}})
	}))
	defer p.Close()
	cfg.BaseURL = p.URL
	input := []Message{{Role: "system", Content: "policy"}, {Role: "assistant", Content: strings.Repeat("old", 8000)}, {Role: "user", Content: "current"}}
	before, _ := json.Marshal(input)
	got, e := a.fitActiveContext(context.Background(), cfg, input, ProfileParams{MaxTokens: 512}, nil, nil)
	after, _ := json.Marshal(got)
	if e == nil || string(before) != string(after) {
		t.Fatal("failed compaction destroyed original context")
	}
}

func TestContextRecoveryOversizedLastMessageCanBeSummarized(t *testing.T) {
	a := testApp(t)
	s := &Session{Messages: []Message{{Role: "assistant", Content: strings.Repeat("x", 24000)}}}
	_, split := a.snapshotForCompactBudget(s, 1024)
	if split != 1 {
		t.Fatal("a single oversized completed answer cannot be compacted")
	}
	call := testToolCall("read_file", "{}")
	if cut := safeCompactionSplit([]Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}, 1); cut != 0 {
		t.Fatal("unfinished tool exchange must stay raw")
	}
}

func TestContextRecoveryPrematureStreamKeepsResumableCheckpoint(t *testing.T) {
	a := testApp(t)
	var streamCalls atomic.Int32
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "topic"}}}})
			return
		}
		if streamCalls.Add(1) > 1 {
			sseSeq(w, w.(http.Flusher), []string{`{"choices":[{"delta":{"content":"continued answer"},"finish_reason":"stop"}]}`})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial answer\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":5,\"total_tokens\":45}}\n\n"))
	}))
	defer p.Close()
	a.settings = Settings{BaseURL: p.URL, Model: "test", ActiveModel: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"prompt": "hello", "mode": "chat"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	run := a.sessions[s.ID].Runs[0]
	bad := run.Status != "paused" || !run.CanResume || run.Usage.Total < 45
	runID := run.ID
	n := 0
	for _, m := range run.CheckpointMessages {
		if m.Role == "assistant" && m.Content == "partial answer" {
			n++
		}
	}
	a.mu.Unlock()
	if bad {
		t.Fatal("incomplete answer cannot safely resume or usage was lost")
	}
	if n != 1 {
		t.Fatal("partial output not preserved exactly once")
	}
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+runID+"/resume", map[string]any{})
	requireStatus(t, w, 202)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		next := a.sessions[s.ID].Runs[len(a.sessions[s.ID].Runs)-1]
		done := next.Status == "completed"
		a.mu.Unlock()
		if done {
			if streamCalls.Load() != 2 {
				t.Fatal("visible stream was replayed")
			}
			time.Sleep(100 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("incomplete stream could not resume to completion")
}

func TestContextRecoveryCompactionInvalidatesOldUsageAnchor(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 65536}}}
	s := &Session{ID: "anchor", Messages: []Message{{Role: "user", Content: "new"}}}
	params := ProfileParams{MaxTokens: 512}
	base := a.buildContextPreview(s, "next", "chat", "", nil, a.settings, params, true)
	anchor := &ContextUsageAnchor{Usage: TokenUsage{Prompt: 50000, Completion: 100}, PromptEstimate: 100, CompletionEstimate: 20, HeaderFingerprint: contextHeaderFingerprint(a.settings, base.Messages[0].Content, base.Tools)}
	s.Runs = []*Task{{ID: "old", ContextAnchor: anchor}}
	if a.buildContextPreview(s, "next", "chat", "", nil, a.settings, params, false).Breakdown.UsageCalibration == 0 {
		t.Fatal("fixture lacks baseline usage calibration")
	}
	s.Compact = `{"goal":"new short summary"}`
	s.CompactedMessages = 20
	s.CompactedAt = "generation-after-compaction"
	if a.buildContextPreview(s, "next", "chat", "", nil, a.settings, params, false).Breakdown.UsageCalibration != 0 {
		t.Fatal("compacted history still inherits pre-compaction token pressure")
	}
	anchor.CompactedAt = s.CompactedAt
	if a.buildContextPreview(s, "next", "chat", "", nil, a.settings, params, false).Breakdown.UsageCalibration == 0 {
		t.Fatal("new-generation calibration was disabled")
	}
	anchor.Scope = "active_compact"
	if a.buildContextPreview(s, "next", "chat", "", nil, a.settings, params, false).Breakdown.UsageCalibration != 0 {
		t.Fatal("transient active context inflated the next request")
	}
}

func TestContextRecoverySummaryNormalizesTextShapesOnly(t *testing.T) {
	summary, e := parseCompactSummary(`{"goal":["first goal","second goal"],"constraints":"no writes","decisions":[],"files":[],"facts":"code R731861","pending":[]}`)
	if e != nil {
		t.Fatal(e)
	}
	var record compactSummaryRecord
	_ = json.Unmarshal([]byte(summary), &record)
	if !strings.Contains(record.Goal, "first goal") || !strings.Contains(record.Goal, "second goal") || len(record.Constraints) != 1 || len(record.Facts) != 1 {
		t.Fatal("text normalization lost content")
	}
	for _, bad := range []string{`{"goal":42,"decisions":[],"files":[],"facts":[],"pending":[]}`, `{"goal":[{"value":"unsafe"}],"decisions":[],"files":[],"facts":[],"pending":[]}`} {
		if _, e := parseCompactSummary(bad); e == nil {
			t.Fatal("non-text summary shape accepted")
		}
	}
}

func TestContextRecoveryPreflightCompactsMultipleHistoryBatches(t *testing.T) {
	a := testApp(t)
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Stream {
			sseSeq(w, w.(http.Flusher), []string{`{"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}`})
			return
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"goal":"continue","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer p.Close()
	a.settings = Settings{BaseURL: p.URL, Model: "test", ActiveModel: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID]
	for i := 0; i < 450; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("x", 1000)})
	}
	_ = a.save(s)
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "continue"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.CompactedMessages <= 400 || s.Runs[0].Status != "completed" {
		t.Fatal("long history did not progress through multiple compaction passes")
	}
}

func TestContextRecoverySummaryFingerprintInvalidatesOldAnchor(t *testing.T) {
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
