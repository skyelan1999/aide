package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOutputTokenMeterIsChunkInvariant(t *testing.T) {
	const text = "A😀你好，stream"
	var whole, chunked outputTokenMeter
	want := whole.add(text)
	var got int
	for _, part := range []string{"A", "😀", "你好", "，st", "ream"} {
		got += chunked.add(part)
	}
	if got == 0 || got != want {
		t.Fatalf("chunked tokens = %d, whole = %d", got, want)
	}
	if extra := chunked.add(""); extra != 0 {
		t.Fatalf("empty delta produced %d tokens", extra)
	}
}

func TestParallelCallStreamsOnlyOutputRate(t *testing.T) {
	a, _, task := parallelFixture(t, 0)
	var streamRequested bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		streamRequested = body.Stream
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"你好，", "world"} {
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": part}}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()

	events, cancel := a.subscribeStream(task.ID)
	defer cancel()
	const purpose = "parallel/wave-1/part-1"
	out, err := a.parallelCall(a.withSessionOutputRate(context.Background(), "session", task.ID), task, Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "test"}}, ProfileParams{MaxTokens: 128}, purpose, nil)
	if err != nil || out != "你好，world" {
		t.Fatalf("parallelCall = %q, %v", out, err)
	}
	if !streamRequested {
		t.Fatal("parallel provider request did not enable streaming")
	}
	var tokens, rateEvents int
	for len(events) > 0 {
		event := <-events
		if event.Event == "delta" {
			t.Fatal("parallel output leaked into the primary answer delta stream")
		}
		if event.Event != "output-rate" {
			continue
		}
		rateEvents++
		if event.StreamID == "" || event.OutputTokens == 0 {
			t.Fatalf("invalid output-rate event: %+v", event)
		}
		tokens += event.OutputTokens
	}
	want := contextMessageTokens(Message{Role: "assistant", Content: out})
	if rateEvents == 0 || tokens != want {
		t.Fatalf("rate events = %d, tokens = %d, want %d", rateEvents, tokens, want)
	}
}

func TestSessionOutputRateReconcilesAllGeneratedOutput(t *testing.T) {
	a, _, task := parallelFixture(t, 0)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []map[string]any{
			{"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": "hidden reasoning"}}}},
			{"choices": []any{map[string]any{"delta": map[string]any{"content": "visible"}}}},
			{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "run_shell", "arguments": `{"command":"pwd"}`}}}}}}},
			{"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 37, "total_tokens": 47}, "choices": []any{}},
		}
		for _, chunk := range chunks {
			payload, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()

	events, cancel := a.subscribeStream(task.ID)
	defer cancel()
	ctx := a.withSessionOutputRate(context.Background(), "session", task.ID)
	_, calls, usage, _, err := completeStream(ctx, Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "test"}}, ProfileParams{MaxTokens: 128}, nil, nil, nil, nil)
	if err != nil || usage.Completion != 37 || len(calls) != 1 {
		t.Fatalf("completeStream usage=%+v calls=%d err=%v", usage, len(calls), err)
	}
	tokens := 0
	for len(events) > 0 {
		event := <-events
		if event.Event == "output-rate" {
			tokens += event.OutputTokens
		}
	}
	if tokens != 37 {
		t.Fatalf("session output tokens = %d, want provider completion 37", tokens)
	}
}

func TestSessionOutputRateCombinesConcurrentNonStreamRequests(t *testing.T) {
	a, _, task := parallelFixture(t, 0)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"background session work"}}],"usage":{"prompt_tokens":5,"completion_tokens":11,"total_tokens":16}}`)
	}))
	defer provider.Close()

	events, cancel := a.subscribeStream(task.ID)
	defer cancel()
	ctx := a.withSessionOutputRate(context.Background(), "session", task.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := complete(ctx, Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "test"}}, ProfileParams{MaxTokens: 64}, nil, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	tokens := 0
	streams := map[string]bool{}
	for len(events) > 0 {
		event := <-events
		if event.Event == "output-rate" {
			tokens += event.OutputTokens
			streams[event.StreamID] = true
		}
	}
	if tokens != 22 || len(streams) != 2 {
		t.Fatalf("session tokens=%d streams=%d, want 22 across 2 requests", tokens, len(streams))
	}
}

func TestSessionOutputRateFallsBackToSessionEventsAfterRunCloses(t *testing.T) {
	a, _, task := parallelFixture(t, 0)
	ch := make(chan string, 1)
	a.eventMu.Lock()
	a.globalSubs = map[chan string]struct{}{ch: {}}
	a.eventMu.Unlock()
	defer func() {
		a.eventMu.Lock()
		delete(a.globalSubs, ch)
		a.eventMu.Unlock()
	}()
	a.publishSessionOutputRate("session-late", task.ID, streamEvent{Event: "output-rate", StreamID: "session/request/9", OutputTokens: 7})
	msg := <-ch
	if !strings.HasPrefix(msg, "output-rate:") {
		t.Fatalf("unexpected global event: %s", msg)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(msg, "output-rate:")), &event); err != nil {
		t.Fatal(err)
	}
	if event["sessionId"] != "session-late" || event["runId"] != task.ID || event["streamId"] != "session/request/9" || event["outputTokens"] != float64(7) {
		t.Fatalf("unexpected session output event: %#v", event)
	}
}
