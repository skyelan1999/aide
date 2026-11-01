package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Mandatory in Delivery, opt-in in the ordinary race suite. No user workspaces,
// live model credentials, OS grants or production ports are used.
func TestReleaseBrowserMatrix(t *testing.T) {
	if os.Getenv("AIDE_RELEASE_BROWSER") != "1" {
		t.Skip("Delivery runs this separately with all three installed engines")
	}
	out := os.Getenv("AIDE_EVIDENCE_DIR")
	if out == "" {
		t.Fatal("AIDE_EVIDENCE_DIR required")
	}
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad JSON", 400)
			return
		}
		const text = "RELEASE_BROWSER_REPLY: **persisted** synthetic response"
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
		} else {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: text}}}})
		}
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "release-fixture", APIKey: "synthetic-release-key", SandboxMode: "workspace-write"}
	a.mu.Lock()
	a.storeModelAPIKeyPlaintextLocked("synthetic-release-key")
	a.mu.Unlock()
	tasks := make([]*Task, 0, 48)
	for i := 0; i < 48; i++ {
		question, _ := json.Marshal(map[string]any{"type": "confirm", "approvalKind": "shell", "command": "printf release-confirmation", "questionId": "release-question", "approvalRound": "1"})
		task := &Task{ID: fmt.Sprintf("approval-%d", i), Mode: "chat", Prompt: "Release manual approval", Status: "awaiting_clarification", WorkspaceID: a.wsID(), AnswerCh: make(chan string, 1), answerRound: 1, PendingQuestion: question}
		session := &Session{ID: fmt.Sprintf("release-%d", i), Number: 900 + i, Title: fmt.Sprintf("Release fixture %d", i), Created: time.Now().Format(time.RFC3339Nano), Updated: time.Now().Format(time.RFC3339Nano), Messages: []Message{}, Runs: []*Task{task}}
		a.sessions[session.ID] = session
		if err := a.save(session); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-release-matrix.cjs")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_TOKEN="+a.token)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("release browser matrix: %v\n%s", err, output)
	}
	for _, task := range tasks {
		if len(task.AnswerCh) != 1 || <-task.AnswerCh != "拒绝" {
			t.Errorf("manual approval did not reach exact owned round: %s", task.ID)
		}
	}
	t.Log(string(output))
}
