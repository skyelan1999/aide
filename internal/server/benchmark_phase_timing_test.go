package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type benchmarkPhaseSpan struct {
	Task, Actor, Phase, Label string
	StartNS, EndNS            int64
}

type benchmarkPhaseRecorder struct {
	mu       sync.Mutex
	spans    []benchmarkPhaseSpan
	stop     chan struct{}
	wg       sync.WaitGroup
	open     map[string]benchmarkPhaseSpan
	apps     []*App
	unpaired int
}

func newBenchmarkPhaseRecorder() *benchmarkPhaseRecorder {
	return &benchmarkPhaseRecorder{stop: make(chan struct{}), open: map[string]benchmarkPhaseSpan{}}
}
func (r *benchmarkPhaseRecorder) span(task, actor, phase, label string) func() {
	start := time.Now().UnixNano()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.spans = append(r.spans, benchmarkPhaseSpan{Task: task, Actor: actor, Phase: phase, Label: label, StartNS: start, EndNS: time.Now().UnixNano()})
			r.mu.Unlock()
		})
	}
}

func benchmarkToolPhase(name string) string {
	switch name {
	case "environment_run_queue":
		return "queue_execution"
	case "read_file", "list_files", "search_text", "semantic_search":
		return "read_and_persist"
	case "write_file":
		return "proposals_and_persist"
	case "bench_check":
		return "verification"
	case "environment_submit", "environment_submit_batch":
		return "schedule"
	case "environment_collect", "environment_collect_ready":
		return "collect_and_wait"
	case "environment_exchange":
		return "coordination"
	default:
		return "other_tool"
	}
}

func (r *benchmarkPhaseRecorder) event(task string, ev streamEvent) {
	now := time.Now().UnixNano()
	key := task + ":" + ev.CallID
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev.Event {
	case "intent":
		r.open[key] = benchmarkPhaseSpan{Task: task, Actor: "main", Phase: benchmarkToolPhase(ev.Tool), Label: ev.Tool, StartNS: now}
	case "tool":
		if span, ok := r.open[key]; ok {
			span.EndNS = now
			r.spans = append(r.spans, span)
			delete(r.open, key)
		} else {
			r.unpaired++
		}
	case "compaction":
		key = task + ":compaction"
		if ev.Phase == "started" {
			if _, exists := r.open[key]; !exists {
				start := now
				if ev.StartedAt > 0 {
					start = ev.StartedAt * int64(time.Millisecond)
				}
				r.open[key] = benchmarkPhaseSpan{Task: task, Actor: "main", Phase: "context_compaction", StartNS: start}
			}
		} else if ev.Phase == "completed" || ev.Phase == "failed" {
			if span, ok := r.open[key]; ok {
				span.EndNS = now
				r.spans = append(r.spans, span)
				delete(r.open, key)
			} else {
				r.unpaired++
			}
		}
	}
}

func (r *benchmarkPhaseRecorder) observe(a *App) {
	r.mu.Lock()
	r.apps = append(r.apps, a)
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		seen := map[string]bool{}
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ticker.C:
				a.mu.Lock()
				ids := []string{}
				for _, s := range a.sessions {
					for _, task := range s.Runs {
						if !seen[task.ID] {
							ids = append(ids, task.ID)
						}
					}
				}
				a.mu.Unlock()
				for _, id := range ids {
					seen[id] = true
					ch, cancel := a.subscribeStream(id)
					r.wg.Add(1)
					go func(id string, ch <-chan streamEvent, cancel func()) {
						defer r.wg.Done()
						defer cancel()
						for {
							select {
							case <-r.stop:
								return
							case ev, ok := <-ch:
								if !ok {
									return
								}
								r.event(id, ev) // payload/reasoning text is never retained.
							}
						}
					}(id, ch, cancel)
				}
			}
		}
	}()
}

type benchmarkTimingTransport struct {
	base http.RoundTripper
	r    *benchmarkPhaseRecorder
}

func (tr *benchmarkTimingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/responses") && !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return tr.base.RoundTrip(req)
	}
	task, actor := "", "worker_or_auxiliary"
	if pool, _ := req.Context().Value(executionPoolKey{}).(*executionPool); pool != nil {
		task, actor = pool.task.ID, "main"
	}
	phase := "model_request"
	if req.Header.Get("Accept") != "text/event-stream" {
		phase = "compaction_or_auxiliary_api"
	}
	end := tr.r.span(task, actor, phase, req.URL.Path)
	response, err := tr.base.RoundTrip(req)
	if err != nil {
		end()
		return response, err
	}
	response.Body = &benchmarkTimingBody{ReadCloser: response.Body, end: end}
	return response, nil
}

type benchmarkTimingBody struct {
	io.ReadCloser
	end func()
}

func (b *benchmarkTimingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.end()
	}
	return n, err
}
func (b *benchmarkTimingBody) Close() error {
	e := b.ReadCloser.Close()
	b.end()
	return e
}

type benchmarkRoundTripFunc func(*http.Request) (*http.Response, error)

func (f benchmarkRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBenchmarkPhaseRecorderNoPayloadAndNoDuplicateIntervals(t *testing.T) {
	r := newBenchmarkPhaseRecorder()
	tr := &benchmarkTimingTransport{r: r, base: benchmarkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("SECRET_BODY")), Header: http.Header{}}, nil
	})}
	ctx := context.WithValue(context.Background(), executionPoolKey{}, &executionPool{task: &Task{ID: "test-main"}})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", strings.NewReader("SECRET_PROMPT"))
	req.Header.Set("Authorization", "SECRET_KEY")
	req.Header.Set("Accept", "text/event-stream")
	response, e := tr.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	r.event("test-main", streamEvent{Event: "intent", Tool: "read_file", CallID: "r", Args: "SECRET_PATH"})
	r.event("test-main", streamEvent{Event: "reasoning", Reasoning: "SECRET_REASONING"})
	r.event("test-main", streamEvent{Event: "tool", Tool: "read_file", CallID: "r", Preview: "SECRET_RESULT"})
	b, _ := json.Marshal(r.spans)
	if len(r.spans) != 2 || strings.Contains(string(b), "SECRET") || r.spans[0].Actor != "main" || r.spans[0].EndNS < r.spans[0].StartNS {
		t.Fatal("trace leaked payload, duplicated EOF/Close or lost attribution", string(b))
	}
}

func TestLargeEngineeringPhaseTimingLiveLuna(t *testing.T) {
	data, out := os.Getenv("AIDE_STAGE_LIVE_DATA"), os.Getenv("AIDE_STAGE_TRACE")
	if data == "" || out == "" {
		t.Skip("explicit paid phase timing opt-in required")
	}
	t.Setenv("AIDE_LARGE_LIVE_DATA", data)
	if only := os.Getenv("AIDE_STAGE_CASES"); only != "" {
		t.Setenv("AIDE_LARGE_LIVE_ONLY", only)
	} else {
		t.Setenv("AIDE_LARGE_LIVE_ONLY", "medium-1X")
	}
	t.Setenv("AIDE_LARGE_LIVE_PROOF", strings.TrimSuffix(out, ".json")+"-proof.json")
	r := newBenchmarkPhaseRecorder()
	base := http.DefaultTransport
	http.DefaultTransport = &benchmarkTimingTransport{base: base, r: r}
	largeBenchAppObserver = r.observe
	largeBenchVerifyObserver = func(a *App, source, suite string, holdout bool) func() {
		if a == nil {
			return func() {}
		}
		a.mu.Lock()
		task := ""
		for _, s := range a.sessions {
			if len(s.Runs) > 0 {
				task = s.Runs[len(s.Runs)-1].ID
			}
		}
		a.mu.Unlock()
		actor := "main"
		if source != a.workPath {
			actor = "worker_verifier"
		}
		return r.span(task, actor, "verification", suite)
	}
	defer func() {
		largeBenchAppObserver, largeBenchVerifyObserver = nil, nil
		close(r.stop)
		r.wg.Wait()
		http.DefaultTransport = base
		r.mu.Lock()
		defer r.mu.Unlock()
		if e := atomicJSON(out, map[string]any{"condition": os.Getenv("AIDE_STAGE_CONDITION"), "spans": r.spans, "unpairedEvents": r.unpaired, "openIntervals": len(r.open), "scope": "timestamps/phase/task/actor/tool labels only; no prompts, keys, source or reasoning. Model request includes network, upstream queue and inference; worker_or_auxiliary is not per-worker attribution."}); e != nil {
			t.Error(e)
		}
	}()
	TestLargeEngineeringLiveLuna(t)
}
