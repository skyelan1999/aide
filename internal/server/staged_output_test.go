package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestStagedOutputPersistsBeforeNextRound(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	registerHarnessFixtureTask(t, a, task)
	var rounds atomic.Int32
	second := make(chan struct{})
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := rounds.Add(1)
		if n == 2 {
			close(second)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		content, finish := "第一段已经查明。", "length"
		if n > 1 {
			content, finish = "第二段验证完成。", "stop"
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}, "finish_reason": finish}}})
		sseSeq(w, w.(http.Flusher), []string{string(chunk)})
	}))
	defer provider.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := a.toolLoop(ctx, Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: "分段验证"}}, ProfileParams{}, nil, task, nil, 0)
		done <- err
	}()
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("no next round")
	}
	a.mu.Lock()
	session := a.approvalSessionLocked(task)
	path := SessionPath(a.dataPath, session.ID, sessionBucketFor(session))
	if len(task.Steps[0].OutputSegments) != 1 {
		t.Errorf("first segment missing before round 2")
	}
	a.mu.Unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Session
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if got := saved.Runs[0].Steps[0].OutputSegments; len(got) != 1 || got[0].Content != "第一段已经查明。" {
		t.Fatalf("disk snapshot: %+v", got)
	}
	w := request(a, "GET", "/api/sessions/"+session.ID+"/runs/"+task.ID+"/outcome", nil)
	requireStatus(t, w, 200)
	var outcome map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	outputs := outcome["outputs"].([]any)
	if len(outputs) != 1 || len(outputs[0].(map[string]any)["segments"].([]any)) != 1 || outcome["status"] != "running" {
		t.Fatal("saved conclusion missing before held second model round", outcome)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel blocked")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(task.Steps[0].OutputSegments) != 1 {
		t.Fatal("cancel overwrote committed segment")
	}
}

func TestStagedOutputFinalAndResumeAppend(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat"}}}
	registerHarnessFixtureTask(t, a, task)
	provider := roundMock(t)
	defer provider.Close()
	task.Steer <- "新要求"
	out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: "开始"}}, ProfileParams{}, nil, task, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out != "回答2" || len(task.Steps[0].OutputSegments) != 2 {
		t.Fatalf("out=%s segments=%+v", out, task.Steps[0].OutputSegments)
	}
	firstID := task.Steps[0].OutputSegments[0].ID
	_, _, err = a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "test"}, []Message{{Role: "user", Content: "继续"}}, ProfileParams{}, nil, task, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	parts := task.Steps[0].OutputSegments
	if len(parts) != 3 || parts[0].ID != firstID || parts[2].ID == firstID {
		t.Fatalf("resume replaced history: %+v", parts)
	}
}

func TestStagedOutputExcludesInternalAndFailsClosed(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "workflow", Steps: []Step{{Name: "propose"}}}
	if err := a.saveOutputSegment(task, 0, 0, "internal JSON"); err != nil {
		t.Fatal(err)
	}
	if len(task.Steps[0].OutputSegments) != 0 {
		t.Fatal("internal workflow response leaked")
	}
	task.Mode = "chat"
	task.Steps[0].Name = "chat"
	registerHarnessFixtureTask(t, a, task)
	oldPath := a.dataPath
	a.dataPath = oldPath + "/not-a-directory"
	if err := os.WriteFile(a.dataPath, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.saveOutputSegment(task, 0, 0, "unsaved"); err == nil {
		t.Fatal("save failure swallowed")
	}
	if len(task.Steps[0].OutputSegments) != 0 {
		t.Fatal("unpersisted segment advertised")
	}
}
