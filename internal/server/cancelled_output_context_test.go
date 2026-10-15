package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelledOutputLegacyContext(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "legacy-stop", Prompt: "生成长文", Mode: "chat", Status: "cancelled", Steps: []Step{{Name: "chat", Content: "TAIL-PARTIAL", Reasoning: "PRIVATE-REASONING", OutputSegments: []OutputSegment{{ID: "seg1", Content: "STAGE-ONE"}}}}}
	s := &Session{ID: "legacy", Messages: []Message{{Role: "user", Content: task.Prompt}}, Runs: []*Task{task}}
	p := a.buildContextPreview(s, "继续正文", "chat", "", nil, Settings{Model: "test"}, ProfileParams{}, true)
	joined := ""
	for _, m := range p.Messages {
		joined += m.Content
	}
	for _, want := range []string{"STAGE-ONE", "TAIL-PARTIAL", "cancelled"} {
		if !strings.Contains(joined, want) {
			t.Errorf("context missing %q", want)
		}
	}
	if strings.Contains(joined, "PRIVATE-REASONING") {
		t.Fatal("reasoning leaked into body history")
	}
	if len(s.Messages) != 1 {
		t.Fatal("read-only preview mutated saved history")
	}
}

func TestCancelledOutputProviderKeepsPartialWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"WAIT-PARTIAL\"}}]}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer provider.Close()
	out, _, _, _, err := completeStream(ctx, Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: "生成"}}, ProfileParams{}, nil, nil, func(string) { cancel() }, nil)
	if !errors.Is(err, context.Canceled) || out != "WAIT-PARTIAL" {
		t.Fatalf("out=%q err=%v; visible text must survive cancellation", out, err)
	}
}

func TestCancelledOutputBoundaryProjection(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "stop", Prompt: "same", Mode: "chat", Status: "cancelled", Steps: []Step{{Name: "chat", Content: "second", OutputSegments: []OutputSegment{{Content: "first"}, {Content: "second"}}}, {Name: "propose", Content: "INTERNAL-JSON"}}}
	s := &Session{ID: "own", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: "same", SourceRunID: task.ID}}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.rememberTaskOutputLocked(s, task, "")
	a.rememberTaskOutputLocked(s, task, "")
	a.mu.Unlock()
	if len(s.Messages) != 2 || strings.Count(s.Messages[1].Content, "second") != 1 || strings.Contains(s.Messages[1].Content, "INTERNAL-JSON") {
		t.Fatalf("bad remembered history: %+v", s.Messages)
	}
	data, _ := json.Marshal(outgoingMessages(s.Messages))
	if strings.Contains(string(data), "sourceRunId") {
		t.Fatal("local provenance leaked to provider protocol")
	}
	history := conversationHistory(s)
	if len(history) != 2 || s.Messages[1].SourceRunID != "stop" {
		t.Fatal("preview duplicated history or mutated stored metadata")
	}
	s.Deleted = true
	if len(conversationHistory(s)) != 0 {
		t.Fatal("deleted session replayed")
	}
	s.Deleted = false
	s.Messages = nil
	s.CompactedMessages = 2
	if len(conversationHistory(s)) != 0 {
		t.Fatal("compacted turn resurrected")
	}
	task.Steps = []Step{{Name: "chat", Reasoning: "PRIVATE"}}
	s.Messages = []Message{{Role: "user", Content: "same"}}
	if len(conversationHistory(s)) != 1 {
		t.Fatal("reasoning-only interruption created a body message")
	}
}

func TestCancelledOutputLegacyOrderAndFingerprint(t *testing.T) {
	a := testApp(t)
	one := &Task{ID: "one", Prompt: "repeat", Mode: "chat", Status: "cancelled", Steps: []Step{{Name: "chat", Content: "FIRST"}}}
	two := &Task{ID: "two", Prompt: "repeat", Mode: "chat", Status: "failed", Steps: []Step{{Name: "chat", Content: "SECOND"}}}
	s := &Session{ID: "repeat", Runs: []*Task{one, two}, Messages: []Message{{Role: "user", Content: "repeat"}, {Role: "user", Content: "repeat"}}}
	history := conversationHistory(s)
	if len(history) != 4 || !strings.Contains(history[1].Content, "FIRST") || !strings.Contains(history[3].Content, "SECOND") {
		t.Fatalf("legacy order lost: %+v", history)
	}
	before := a.buildContextPreview(s, "continue", "chat", "", nil, Settings{Model: "test"}, ProfileParams{}, true)
	two.Steps[0].Content = "CHANGED-SECOND"
	after := a.buildContextPreview(s, "continue", "chat", "", nil, Settings{Model: "test"}, ProfileParams{}, true)
	if before.Fingerprint == after.Fingerprint || after.Breakdown.HistoryAssistant.Bytes <= 0 {
		t.Fatal("recovered output absent from fingerprint/accounting")
	}
	s.Messages = append(s.Messages, Message{Role: "user", Content: "repeat"})
	if len(conversationHistory(s)) != 3 {
		t.Fatal("ambiguous historical prompt ownership was guessed")
	}
}

func TestCancelledOutputHTTPFollowupAndPauseResume(t *testing.T) {
	for _, action := range []string{"cancel", "pause"} {
		t.Run(action, func(t *testing.T) {
			a := testApp(t)
			second := make(chan struct{})
			var requests atomic.Int32
			ready := make(chan struct{})
			observed := make(chan []Message, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream   bool      `json:"stream"`
					Messages []Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if !body.Stream {
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "fixture"}}}})
					return
				}
				n := requests.Add(1)
				if n == 1 {
					<-ready
				}
				if n == 3 {
					observed <- body.Messages
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				text, finish := "STAGE-FIRST", "length"
				if n == 2 {
					text, finish = "TAIL-SECOND", ""
				}
				if n >= 3 {
					text, finish = "CONTINUED-THIRD", "stop"
				}
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}, "finish_reason": finish}}})
				w.Write(append(append([]byte("data: "), raw...), []byte("\n")...))
				fl.Flush()
				if n == 2 {
					close(second)
					<-r.Context().Done()
					return
				}
				w.Write([]byte("\ndata: [DONE]\n\n"))
				fl.Flush()
			}))
			defer provider.Close()
			defer func() {
				select {
				case <-ready:
				default:
					close(ready)
				}
			}()
			a.settings = Settings{BaseURL: provider.URL, Model: "test"}
			w := request(a, "POST", "/api/sessions", map[string]string{"title": "fixed-title"})
			requireStatus(t, w, 201)
			var s Session
			json.Unmarshal(w.Body.Bytes(), &s)
			w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"prompt": "请给出正文", "mode": "chat"})
			requireStatus(t, w, 202)
			var run Task
			json.Unmarshal(w.Body.Bytes(), &run)
			events, unsubscribe := a.subscribeStream(run.ID)
			defer unsubscribe()
			close(ready)
			seenTail := false
			for !seenTail {
				select {
				case event := <-events:
					seenTail = event.Event == "delta" && strings.Contains(event.Text, "TAIL-SECOND")
				case <-time.After(10 * time.Second):
					t.Fatal("partial delta not observed")
				}
			}
			select {
			case <-second:
			case <-time.After(10 * time.Second):
				t.Fatal("second round never started")
			}
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+run.ID+"/"+action, map[string]any{}), map[string]int{"cancel": 200, "pause": 202}[action])
			wait := func(id string) Task {
				for start := time.Now(); time.Since(start) < 10*time.Second; {
					w := request(a, "GET", "/api/sessions/"+s.ID, nil)
					var current Session
					json.Unmarshal(w.Body.Bytes(), &current)
					for _, r := range current.Runs {
						if r.ID == id && r.Status != "running" {
							return *r
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("task did not settle")
				return Task{}
			}
			stopped := wait(run.ID)
			if action == "pause" && (!stopped.CanResume || stopped.Status != "paused") {
				t.Fatalf("not resumable: %+v", stopped)
			}
			w = request(a, "GET", "/api/sessions/"+s.ID, nil)
			var saved Session
			json.Unmarshal(w.Body.Bytes(), &saved)
			found := false
			for _, m := range saved.Messages {
				if m.Role == "assistant" && strings.Contains(m.Content, "STAGE-FIRST") && strings.Contains(m.Content, "TAIL-SECOND") {
					found = true
				}
			}
			if !found {
				t.Fatalf("partial response missing in persisted messages: %+v", saved.Messages)
			}
			a.mu.Lock()
			diskPath := SessionPath(a.dataPath, s.ID, sessionBucketFor(a.sessions[s.ID]))
			a.mu.Unlock()
			disk, err := os.ReadFile(diskPath)
			if err != nil || !strings.Contains(string(disk), "TAIL-SECOND") {
				t.Fatalf("partial missing on disk: %v", err)
			}
			if action == "pause" {
				w = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+run.ID+"/resume", map[string]any{})
			} else {
				w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"prompt": "接着正文继续", "mode": "chat"})
			}
			requireStatus(t, w, 202)
			var next Task
			json.Unmarshal(w.Body.Bytes(), &next)
			wait(next.ID)
			var messages []Message
			select {
			case messages = <-observed:
			case <-time.After(5 * time.Second):
				t.Fatal("no followup provider request")
			}
			joined := ""
			for _, m := range messages {
				joined += m.Content + "\n"
			}
			for _, want := range []string{"STAGE-FIRST", "TAIL-SECOND"} {
				if strings.Count(joined, want) != 1 {
					t.Errorf("followup needs exactly one %s: %s", want, joined)
				}
			}
			if action == "cancel" && !strings.Contains(joined, "cancelled") {
				t.Error("cancelled result presented without interrupted status")
			}
			if action == "pause" && !strings.Contains(joined, "暂停") {
				t.Error("resume has no continuation instruction")
			}
		})
	}
}

func TestCancelledOutputLegacyResumedLineage(t *testing.T) {
	root := &Task{ID: "root", Prompt: "old request", Status: "resumed", Mode: "chat"}
	child := &Task{ID: "child", Prompt: root.Prompt, ResumedFrom: root.ID, Status: "paused", Mode: "chat", Steps: []Step{{Name: "chat", Content: "RESUMED-PARTIAL"}}}
	s := &Session{Runs: []*Task{root, child}, Messages: []Message{{Role: "user", Content: root.Prompt}}}
	history := conversationHistory(s)
	if len(history) != 2 || !strings.Contains(history[1].Content, "RESUMED-PARTIAL") {
		t.Fatalf("resumed ownership not recovered: %+v", history)
	}
	child.ResumedFrom = child.ID
	if len(conversationHistory(s)) != 1 {
		t.Fatal("cyclic ownership recovered without a root")
	}
}

func TestCancelledOutputSuccessfulStagesReachHistory(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "stages", Mode: "chat", Status: "completed", Steps: []Step{{Name: "chat", Content: "FINAL-STAGE", OutputSegments: []OutputSegment{{Content: "EARLIER-STAGE"}, {Content: "FINAL-STAGE"}}}}}
	s := &Session{ID: "stages", Runs: []*Task{task}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.rememberTaskOutputLocked(s, task, "FINAL-STAGE")
	a.mu.Unlock()
	if len(s.Messages) != 1 || s.Messages[0].Content != "EARLIER-STAGE\n\nFINAL-STAGE" {
		t.Fatalf("success replaced earlier delivery: %+v", s.Messages)
	}
}

func TestCancelledOutputMaterializeBeforeCompaction(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "old-stop", Prompt: "old request", Mode: "chat", Status: "cancelled", Steps: []Step{{Name: "chat", Content: strings.Repeat("HISTORY-PIECE ", 800)}}}
	recent := &Task{ID: "recent", Prompt: "recent request", Status: "completed", Mode: "chat"}
	s := &Session{ID: "materialize", Runs: []*Task{task, recent}, Messages: []Message{{Role: "user", Content: task.Prompt}, {Role: "user", Content: "recent request"}, {Role: "assistant", Content: strings.Repeat("R", 3900)}}}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[s.ID] = s
	if err := a.materializeConversationLocked(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Messages) != 4 || s.Messages[1].SourceRunID != task.ID {
		t.Fatalf("missing durable recovery: %+v", s.Messages)
	}
	snap, split := a.snapshotForCompactBudget(s, 1024)
	found := false
	for _, m := range snap.messages {
		if strings.Contains(m.Content, "HISTORY-PIECE") {
			found = true
		}
	}
	if !found || split == 0 {
		t.Fatalf("compaction omitted recovered text: split%d", split)
	}
	candidate := compactedSessionCandidate(s, snap, split, "摘要已标记未完成")
	if len(conversationHistory(&candidate)) != len(candidate.Messages) {
		t.Fatal("compacted interruption resurrected")
	}
	failSession := &Session{ID: "fail-materialize", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[failSession.ID] = failSession
	oldPath := a.dataPath
	a.dataPath = oldPath + "/blocked"
	defer func() { a.dataPath = oldPath }()
	if err := os.WriteFile(a.dataPath, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.materializeConversationLocked(failSession); err == nil || len(failSession.Messages) != 1 {
		t.Fatal("failed recovery mutated original history")
	}
}
