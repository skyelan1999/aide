package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRetryOlderTaskExcludesLaterConversation(t *testing.T) {
	a := testApp(t)
	provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) { return "Retry answer.", nil }})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	prior := &Task{ID: "prior", Prompt: "EARLIER-CONTEXT", Status: "completed"}
	original := &Task{ID: "original", Mode: "chat", Prompt: "OLD-RETRY-MARKER", Status: "completed", Model: "fixture", Strategy: "manual", Profile: "default"}
	later := &Task{ID: "later", Prompt: "FUTURE-ROS2-REQUEST", Status: "completed"}
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Runs = []*Task{prior, original, later}
	s.Messages = []Message{
		{Role: "user", Content: prior.Prompt, SourceRunID: prior.ID},
		{Role: "assistant", Content: "EARLIER-ANSWER", SourceRunID: prior.ID},
		{Role: "user", Content: original.Prompt, SourceRunID: original.ID},
		{Role: "assistant", Content: "OLD-ANSWER-TO-REGENERATE", SourceRunID: original.ID},
		{Role: "user", Content: later.Prompt, SourceRunID: later.ID},
	}
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/original/retry", nil), 202)
	deadline := time.Now().Add(5 * time.Second)
	var input []Message
	for {
		var current Session
		if err := json.Unmarshal(request(a, "GET", "/api/sessions/"+s.ID, nil).Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if len(current.Runs) == 4 && current.Runs[3].Status != "running" {
			if current.Runs[3].Status != "completed" || current.Runs[2].Status != "completed" {
				t.Fatalf("unexpected retry/later status: %s/%s (error: %s)", current.Runs[3].Status, current.Runs[2].Status, current.Runs[3].Error)
			}
			if len(current.Runs[3].RequestSnapshots) > 0 {
				input = current.Runs[3].RequestSnapshots[0].Messages
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(input) == 0 {
		t.Fatal("provider was never called")
	}
	b, _ := json.Marshal(input)
	if strings.Contains(string(b), later.Prompt) || strings.Contains(string(b), "OLD-ANSWER-TO-REGENERATE") {
		t.Fatal("retry sent later conversation or the answer being regenerated")
	}
	if !strings.Contains(string(b), prior.Prompt) || !strings.Contains(string(b), "EARLIER-ANSWER") {
		t.Fatal("retry lost conversation preceding the original input")
	}
	if last := input[len(input)-1]; last.Role != "user" || !strings.HasPrefix(last.Content, original.Prompt) {
		t.Fatal("original request is not the final user input")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(s.Messages) < 5 || s.Messages[4].Content != later.Prompt || len(s.Runs) != 4 {
		t.Fatal("projection mutated existing session history")
	}
}

func TestRetryConversationLegacyOccurrences(t *testing.T) {
	first := &Task{ID: "one", Prompt: "same"}
	target := &Task{ID: "two", Prompt: "same"}
	later := &Task{ID: "three", Prompt: "later"}
	s := &Session{Runs: []*Task{first, target, later}, Messages: []Message{
		{Role: "user", Content: "same"}, {Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "same"}, {Role: "assistant", Content: "second answer"},
		{Role: "user", Content: "later"},
	}}
	view, err := retryConversation(s, target)
	if err != nil || len(view.Messages) != 2 || len(view.Runs) != 1 || view.Messages[1].Content != "first answer" {
		t.Fatalf("legacy occurrence mapping failed: %#v %v", view, err)
	}
	if len(s.Messages) != 5 || len(s.Runs) != 3 {
		t.Fatal("legacy projection modified source history")
	}
}

func TestRetryConversationCompactedOriginalSnapshot(t *testing.T) {
	prior := &Task{ID: "prior", Prompt: "preceding input", Status: "cancelled", Steps: []Step{{Name: "chat", Content: "preceding answer"}}}
	recovered, _ := interruptedOutputMessage(prior)
	target := &Task{ID: "target", Prompt: "original request", RequestSnapshots: []RequestSnapshot{{Messages: []Message{
		{Role: "system", Content: "OLD-SYSTEM-HEADER"},
		{Role: "system", Content: "历史摘要（已压缩 3 条消息）:\nORIGINAL-SUMMARY"},
		{Role: "user", Content: "preceding input"}, {Role: "assistant", Content: recovered.Content},
		{Role: "user", Content: "original request\n\n## 本轮普通对话要求\nold guidance"},
	}}}}
	s := &Session{Compact: "FUTURE-SUMMARY", CompactedMessages: 99, Runs: []*Task{prior, target}, Messages: []Message{{Role: "user", Content: "later input"}}}
	view, err := retryConversation(s, target)
	if err != nil || view.Compact != "ORIGINAL-SUMMARY" || view.CompactedMessages != 3 || len(view.Messages) != 2 {
		t.Fatalf("snapshot fallback lost original prefix: %#v %v", view, err)
	}
	if history := conversationHistory(view); len(history) != 2 || history[1].Content != recovered.Content {
		t.Fatal("snapshot replay duplicated previously recovered partial output")
	}
	b, _ := json.Marshal(view)
	for _, forbidden := range []string{"OLD-SYSTEM-HEADER", "FUTURE-SUMMARY", "old guidance", "later input"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("snapshot replay retained %s", forbidden)
		}
	}
	if s.Compact != "FUTURE-SUMMARY" || len(target.RequestSnapshots[0].Messages) != 5 {
		t.Fatal("snapshot fallback modified live storage")
	}
}

func TestRetryMissingHistoryRejectsWithoutAddingTask(t *testing.T) {
	a := testApp(t)
	provider := newToolProvider(t, nil)
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Runs = []*Task{{ID: "old", Mode: "chat", Prompt: "missing input", Status: "completed", Model: "fixture"}}
	s.Messages = []Message{{Role: "user", Content: "later request"}}
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/old/retry", nil), 409)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(s.Runs) != 1 || len(s.Messages) != 1 || len(a.cancels) != 0 || len(provider.snapshotRequests()) != 0 {
		t.Fatal("rejected retry created work or changed history")
	}
}
