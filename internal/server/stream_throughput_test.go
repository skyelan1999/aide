package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamThroughputResult struct {
	Streams           int     `json:"streams"`
	ChunksPerStream   int     `json:"chunksPerStream"`
	TotalChunks       int     `json:"totalChunks"`
	DeltaEvents       int     `json:"deltaEvents"`
	ReceivedTextBytes int     `json:"receivedTextBytes"`
	ElapsedMillis     float64 `json:"elapsedMillis"`
	ChunksPerSecond   float64 `json:"chunksPerSecond"`
	SlowestFirstMs    float64 `json:"slowestFirstMillis"`
	SlowestStreamMs   float64 `json:"slowestStreamMillis"`
	ContentIntegrity  bool    `json:"contentIntegrity"`
}

func TestRunEventsHighThroughputCoalescingPreservesText(t *testing.T) {
	const chunkCount = 512
	chunks := make([]string, chunkCount)
	for i := range chunks {
		chunks[i] = `{"choices":[{"delta":{"content":"x"}}]}`
	}
	provider := streamMock(t, chunks, true)
	defer provider.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "profile"}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var session Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]string{"mode": "chat", "prompt": "profile"})
	requireStatus(t, w, 202)
	var task Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	events := readSSE(t, server.URL+"/api/sessions/"+session.ID+"/runs/"+task.ID+"/events?access_token="+a.token)
	var text strings.Builder
	deltaEvents := 0
	for _, event := range events {
		if event.Event == "delta" {
			deltaEvents++
			text.WriteString(event.Text)
		}
	}
	if text.String() != strings.Repeat("x", chunkCount) {
		t.Fatalf("received %d of %d bytes", text.Len(), chunkCount)
	}
	if deltaEvents >= chunkCount {
		t.Fatalf("expected coalesced SSE events, got %d for %d chunks", deltaEvents, chunkCount)
	}
}

// TestStreamThroughputProfile uses an opt-in deterministic provider to measure
// Aide's own single-stream and concurrent-stream forwarding cost. The provider
// releases identical pre-encoded output only after every SSE client is ready.
func TestStreamThroughputProfile(t *testing.T) {
	if os.Getenv("AIDE_STREAM_PROFILE") == "" {
		t.Skip("set AIDE_STREAM_PROFILE=1 to run the deterministic throughput profile")
	}
	results := []streamThroughputResult{
		runStreamThroughputProfile(t, 1, 2000),
		runStreamThroughputProfile(t, 4, 2000),
	}
	report := map[string]any{
		"label":      os.Getenv("AIDE_STREAM_PROFILE_LABEL"),
		"goMaxProcs": runtime.GOMAXPROCS(0),
		"results":    results,
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stream throughput profile: %s", encoded)
	if path := os.Getenv("AIDE_STREAM_PROFILE_OUT"); path != "" {
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runStreamThroughputProfile(t *testing.T, streams, chunks int) streamThroughputResult {
	t.Helper()
	gate := make(chan struct{})
	payload := strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n", chunks) +
		fmt.Sprintf("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", chunks, chunks+1) +
		"data: [DONE]\n\n"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		streaming, _ := body["stream"].(bool)
		if !streaming {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "profile"}}}})
			return
		}
		<-gate
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(payload))
	}))
	defer provider.Close()

	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "profile"}
	server := httptest.NewServer(a.Handler())
	defer server.Close()

	type runTarget struct{ sessionID, runID string }
	targets := make([]runTarget, 0, streams)
	for i := 0; i < streams; i++ {
		w := request(a, "POST", "/api/sessions", map[string]string{})
		requireStatus(t, w, 201)
		var session Session
		if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		w = request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]string{"mode": "chat", "prompt": "profile"})
		requireStatus(t, w, 202)
		var task Task
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, runTarget{sessionID: session.ID, runID: task.ID})
	}

	type observation struct {
		deltas     int
		text       string
		firstDelta time.Duration
		elapsed    time.Duration
		err        error
	}
	observations := make(chan observation, streams)
	var ready sync.WaitGroup
	ready.Add(streams)
	startedAt := time.Time{}
	for _, target := range targets {
		go func(target runTarget) {
			obs := observation{}
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Get(server.URL + "/api/sessions/" + target.sessionID + "/runs/" + target.runID + "/events?access_token=" + a.token)
			ready.Done()
			if err != nil {
				obs.err = err
				observations <- obs
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				obs.err = fmt.Errorf("events status %d", resp.StatusCode)
				observations <- obs
				return
			}
			var text strings.Builder
			eventName := ""
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				switch {
				case strings.HasPrefix(line, "event: "):
					eventName = strings.TrimPrefix(line, "event: ")
				case strings.HasPrefix(line, "data: ") && eventName == "delta":
					var event streamEvent
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
						obs.err = err
						observations <- obs
						return
					}
					if obs.deltas == 0 {
						obs.firstDelta = time.Since(startedAt)
					}
					obs.deltas++
					text.WriteString(event.Text)
				case line == "":
					eventName = ""
				}
			}
			obs.elapsed = time.Since(startedAt)
			obs.text = text.String()
			if obs.err == nil {
				obs.err = scanner.Err()
			}
			observations <- obs
		}(target)
	}
	ready.Wait()
	startedAt = time.Now()
	close(gate)

	result := streamThroughputResult{Streams: streams, ChunksPerStream: chunks, TotalChunks: streams * chunks, ContentIntegrity: true}
	expected := strings.Repeat("x", chunks)
	for i := 0; i < streams; i++ {
		obs := <-observations
		if obs.err != nil {
			t.Fatal(obs.err)
		}
		if obs.text != expected {
			result.ContentIntegrity = false
		}
		result.DeltaEvents += obs.deltas
		result.ReceivedTextBytes += len(obs.text)
		result.SlowestFirstMs = max(result.SlowestFirstMs, float64(obs.firstDelta)/float64(time.Millisecond))
		result.SlowestStreamMs = max(result.SlowestStreamMs, float64(obs.elapsed)/float64(time.Millisecond))
	}
	result.ElapsedMillis = result.SlowestStreamMs
	result.ChunksPerSecond = float64(result.TotalChunks) / (result.ElapsedMillis / 1000)
	return result
}
