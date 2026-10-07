package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAvatarArgs = `{"scene":"completed","variant":2,"emotion":"happy","intensity":2,"reply":"完成了。"}`

func TestAvatarCueValidationAndBudget(t *testing.T) {
	for scene, variants := range avatarSceneVariants {
		for variant := 1; variant <= variants; variant++ {
			cue, _ := parseAvatarCue(fmt.Sprintf(`{"scene":%q,"variant":%d,"emotion":"calm","intensity":1}`, scene, variant))
			if cue == nil {
				t.Fatalf("valid cue rejected: %s/%d", scene, variant)
			}
		}
	}
	for _, args := range []string{
		`{"scene":"delete_files","variant":1,"emotion":"calm","intensity":1}`,
		`{"scene":"reading","variant":3,"emotion":"calm","intensity":1}`,
		`{"scene":"idle","variant":0,"emotion":"calm","intensity":1}`,
		`{"scene":"idle","variant":1,"emotion":"evil","intensity":1}`,
		`{"scene":"idle","variant":1,"emotion":"calm","intensity":4}`,
		`{"scene":"idle","variant":1,"emotion":"calm","intensity":1,"script":"alert(1)"}`,
		testAvatarArgs + `{}`, strings.Repeat(" ", 64001),
	} {
		if cue, _ := parseAvatarCue(args); cue != nil {
			t.Fatalf("invalid cue accepted: %q", args)
		}
	}
	used := 0
	for i := 0; i < 5; i++ {
		out, calls, cue := consumeAvatarCues("", []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}, true, &used)
		if out != "完成了。" || len(calls) != 0 {
			t.Fatal("cue-only answer must preserve reply without another round")
		}
		if (cue != nil) != (i < avatarCueBudget) {
			t.Fatal("cue budget not enforced")
		}
	}
	used = 0
	_, disabled, cue := consumeAvatarCues("plain", []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}, false, &used)
	if len(disabled) != 1 || cue != nil || used != 0 {
		t.Fatal("disabled cue mutated response")
	}
	_, mixed, cue := consumeAvatarCues("answer", []ToolCall{asstToolCall("read_file", `{"path":"a"}`), asstToolCall(avatarCueToolName, testAvatarArgs)}, true, &used)
	if len(mixed) != 1 || mixed[0].Function.Name != "read_file" || cue == nil {
		t.Fatal("ordinary tool call lost")
	}
}

func avatarTestProvider(t *testing.T, enabled bool, calls []ToolCall, content string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		found := false
		for _, tool := range body.Tools {
			if tool.Function.Name == avatarCueToolName {
				found = true
			}
		}
		if found != enabled {
			t.Errorf("avatar tool enabled=%v, want %v", found, enabled)
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content, ToolCalls: calls}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}})
	}))
	return srv, count
}

func TestAvatarToolLoopSingleCompletionAndDisabled(t *testing.T) {
	for _, format := range []string{"", "text", "json_object"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprint(enabled)+"/"+format, func(t *testing.T) {
				wantEnabled := enabled && format != "json_object"
				a := testApp(t)
				calls := []ToolCall(nil)
				content := "普通回答"
				if wantEnabled {
					calls = []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}
					content = ""
				}
				srv, count := avatarTestProvider(t, wantEnabled, calls, content)
				defer srv.Close()
				task := &Task{ID: newID(), AvatarFeedback: enabled, Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
				events, cancel := a.subscribeStream(task.ID)
				defer cancel()
				registerHarnessFixtureTask(t, a, task)
				out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, []Message{{Role: "user", Content: "你好"}}, ProfileParams{ResponseFormat: format}, nil, task, map[string]Change{}, 0)
				if err != nil {
					t.Fatal(err)
				}
				if count.Load() != 1 {
					t.Fatalf("cosmetic feedback added model round: %d", count.Load())
				}
				want := content
				if wantEnabled {
					want = "完成了。"
				}
				if out != want {
					t.Fatalf("reply=%q want %q", out, want)
				}
				if task.Usage.Prompt != 100 || task.Usage.Completion != 20 {
					t.Fatalf("usage lost: %+v", task.Usage)
				}
				for _, msg := range chain {
					if len(msg.ToolCalls) > 0 || msg.Role == "tool" {
						t.Fatal("unpaired cosmetic tool call in history")
					}
				}
				found := false
				for len(events) > 0 {
					if e := <-events; e.Event == "avatar" {
						found = e.AvatarCue != nil && e.AvatarCue.Scene == "completed"
					}
				}
				if found != wantEnabled {
					t.Fatalf("SSE cue=%v want %v", found, wantEnabled)
				}
			})
		}
	}
}

func TestAvatarAssistantSingleCompletion(t *testing.T) {
	a := testApp(t)
	srv, count := avatarTestProvider(t, true, []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}, "")
	defer srv.Close()
	dec, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, "你好", "", "text", true)
	if err != nil {
		t.Fatal(err)
	}
	if count.Load() != 1 || dec.Reply != "完成了。" || dec.AvatarCue == nil || dec.Action != "chat" {
		t.Fatalf("unexpected decision: %+v (%d requests)", dec, count.Load())
	}
	entry := decisionToVoiceEntry(dec, "你好")
	if entry.AvatarCue == nil || entry.AvatarCue.Variant != 2 {
		t.Fatal("voice response lost cue")
	}
}

func TestAvatarAssistantHTTPOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			a := testApp(t)
			calls := []ToolCall(nil)
			content := "你好。"
			if enabled {
				calls = []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}
				content = ""
			}
			srv, count := avatarTestProvider(t, enabled, calls, content)
			defer srv.Close()
			a.mu.Lock()
			a.settings.BaseURL = srv.URL
			a.settings.Model = "test"
			a.mu.Unlock()
			unlockAssistantSessionForTest(t, a)
			w := request(a, "POST", "/api/sessions/"+assistantSessionID(t, a)+"/assistant-message", map[string]any{"text": "你好", "avatarFeedback": enabled})
			requireStatus(t, w, 200)
			var body struct {
				AvatarCue *AvatarCue `json:"avatarCue"`
				Reply     string     `json:"reply"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if (body.AvatarCue != nil) != enabled || body.Reply == "" || count.Load() != 1 {
				t.Fatalf("HTTP opt-in failed: %+v; requests=%d", body, count.Load())
			}
		})
	}
}

func TestAvatarContextPreviewIncludesToolCost(t *testing.T) {
	for _, format := range []string{"", "text", "json_object"} {
		a := testApp(t)
		base := a.buildContextPreview(nil, "你好", "chat", "", nil, Settings{Model: "test"}, ProfileParams{ResponseFormat: format}, true)
		on := a.buildContextPreview(nil, "你好", "chat", "", nil, Settings{Model: "test"}, ProfileParams{ResponseFormat: format}, true, true)
		if format == "json_object" {
			if on.InputEstimate != base.InputEstimate || on.Fingerprint != base.Fingerprint {
				t.Fatal("JSON format must not add a cosmetic schema or cost")
			}
		} else if len(on.Tools) != len(base.Tools)+1 || on.InputEstimate <= base.InputEstimate || on.Breakdown.ToolSchemas.Tokens <= base.Breakdown.ToolSchemas.Tokens || on.Fingerprint == base.Fingerprint {
			t.Fatal("enabled avatar schema missing from context budget/fingerprint")
		}
		off := a.buildContextPreview(nil, "你好", "chat", "", nil, Settings{Model: "test"}, ProfileParams{ResponseFormat: format}, true, false)
		if off.InputEstimate != base.InputEstimate || off.Fingerprint != base.Fingerprint {
			t.Fatal("disabled avatar costs tokens")
		}
	}
}

func TestAvatarFinishStreamPreservesTerminalStatus(t *testing.T) {
	for _, status := range []string{"completed", "failed", "paused", "cancelled", "interrupted", "awaiting_approval"} {
		a := testApp(t)
		events, cancel := a.subscribeStream("avatar-terminal")
		a.finishStream("avatar-terminal", status, "")
		found := false
		for ev := range events {
			if ev.Event == "done" {
				found = true
				if ev.Status != status {
					t.Fatalf("done=%q want %q", ev.Status, status)
				}
			}
		}
		cancel()
		if !found {
			t.Fatal("missing done")
		}
	}
}

func TestAvatarBudgetSpansWorkflowSteps(t *testing.T) {
	a := testApp(t)
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		i := count.Add(1)
		encoded, _ := json.Marshal(body.Tools)
		if strings.Contains(string(encoded), avatarCueToolName) != (i <= 3) {
			t.Errorf("step %d violated run budget", i)
		}
		calls := []ToolCall(nil)
		out := "第四阶段完成"
		if i <= 3 {
			calls = []ToolCall{asstToolCall(avatarCueToolName, testAvatarArgs)}
			out = ""
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: out, ToolCalls: calls}}}})
	}))
	defer srv.Close()
	task := &Task{ID: newID(), AvatarFeedback: true, Steps: []Step{{Name: "one"}, {Name: "two"}, {Name: "three"}, {Name: "four"}}, Steer: make(chan string, 1)}
	for step := 0; step < 4; step++ {
		registerHarnessFixtureTask(t, a, task)
		_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, []Message{{Role: "user", Content: "继续"}}, ProfileParams{}, nil, task, nil, step)
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 4 || task.AvatarCuesUsed != 3 || task.AvatarCue == nil {
		t.Fatalf("budget/cue not persisted: requests=%d task=%+v", count.Load(), task)
	}
}

func TestAvatarRetryResumeHonorCurrentOptOut(t *testing.T) {
	for _, operation := range []string{"retry", "resume"} {
		t.Run(operation, func(t *testing.T) {
			a := testApp(t)
			srv, _ := avatarTestProvider(t, false, nil, "完成。")
			defer srv.Close()
			a.settings = Settings{BaseURL: srv.URL, Model: "test"}
			original := &Task{ID: "original", Mode: "chat", Prompt: "继续", Status: "paused", CanResume: true, AvatarFeedback: true, AvatarCuesUsed: 2, AvatarCue: &AvatarCue{Scene: "thinking", Variant: 1, Emotion: "calm", Intensity: 1}, Steps: []Step{{Name: "chat", Status: "paused"}}, CheckpointStep: "chat", CheckpointMessages: []Message{{Role: "user", Content: "继续"}}}
			session := &Session{ID: newID(), Runs: []*Task{original}}
			a.mu.Lock()
			a.sessions[session.ID] = session
			_ = a.save(session)
			a.mu.Unlock()
			w := request(a, "POST", "/api/sessions/"+session.ID+"/runs/original/"+operation, map[string]any{"avatarFeedback": false})
			requireStatus(t, w, 202)
			var task Task
			_ = json.Unmarshal(w.Body.Bytes(), &task)
			if task.AvatarFeedback || task.AvatarCue != nil {
				t.Fatalf("old opt-in/cue leaked into %s: %+v", operation, task)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				a.mu.Lock()
				status := session.Runs[len(session.Runs)-1].Status
				a.mu.Unlock()
				if status == "completed" {
					return
				}
				if status == "failed" {
					t.Fatal("new run failed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("new run did not finish")
		})
	}
}

func TestAvatarLateSSEReplaysCueAndStatus(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "finished", Status: "failed", AvatarCue: &AvatarCue{Scene: "error", Variant: 2, Emotion: "frustrated", Intensity: 2}}
	session := &Session{ID: newID(), Runs: []*Task{task}}
	a.mu.Lock()
	a.sessions[session.ID] = session
	a.mu.Unlock()
	w := request(a, "GET", "/api/sessions/"+session.ID+"/runs/finished/events", nil)
	requireStatus(t, w, 200)
	var events []streamEvent
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var e streamEvent
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) == nil {
				events = append(events, e)
			}
		}
	}
	if len(events) != 3 || events[0].Event != "avatar" || events[0].AvatarCue == nil || events[1].Status != "failed" || events[2].Event != "done" || events[2].Status != "failed" {
		t.Fatalf("late SSE lost cue/status: %+v", events)
	}
}
