package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssue73DebugSnapshotsExcludeContent(t *testing.T) {
	a := testApp(t)
	token := enableDebugAccess(t, a)
	const secret = "owned-private-content-canary"
	task := &Task{ID: "redaction-run", Status: "failed", Error: secret, Steps: []Step{{Name: secret, Status: "failed", Content: secret, Reasoning: secret}}, ToolUses: []ToolUse{{Tool: "read_file", Args: secret, Result: secret, Preview: secret}}}
	a.sessions["redaction-session"] = &Session{ID: "redaction-session", Title: secret, Runs: []*Task{task}}
	a.pushError("runtime", "redaction-session", task.ID, secret)
	for _, endpoint := range []string{"/api/debug/sessions", "/api/debug/sessions/redaction-session/runs/redaction-run", "/api/debug/errors", "/api/debug/actions/diagnostic-bundle"} {
		t.Run(endpoint, func(t *testing.T) {
			method := "GET"
			if strings.Contains(endpoint, "/actions/") {
				method = "POST"
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, endpoint, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			a.Handler().ServeHTTP(w, r)
			requireStatus(t, w, 200)
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("diagnostic response disclosed private content: %s", w.Body.String())
			}
			if !strings.Contains(w.Body.String(), task.ID) {
				t.Fatal("diagnostic metadata lost run identity")
			}
		})
	}
	if task.Error != secret || task.ToolUses[0].Preview != secret {
		t.Fatal("redaction mutated saved run")
	}
}

func TestIssue73DebugEventsExcludeContent(t *testing.T) {
	for _, queryToken := range []bool{false, true} {
		t.Run(map[bool]string{false: "bearer", true: "query"}[queryToken], func(t *testing.T) {
			a := testApp(t)
			token := enableDebugAccess(t, a)
			task := &Task{ID: "live-redaction", Status: "running"}
			a.sessions["redaction-session"] = &Session{ID: "redaction-session", Runs: []*Task{task}}
			server := httptest.NewServer(a.Handler())
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			endpoint := server.URL + "/api/debug/sessions/redaction-session/runs/live-redaction/events"
			if queryToken {
				endpoint += "?access_token=" + token
			}
			req, _ := http.NewRequest("GET", endpoint, nil)
			if !queryToken {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal(res.Status)
			}
			const secret = "owned-live-private-canary"
			for _, event := range []string{"delta", "reasoning", "tool", "intent", "step", "clarification", "segment", "avatar", "unknown-future-event"} {
				a.publishStream(task.ID, streamEvent{Event: event, Text: secret, Reasoning: secret, Preview: secret, Args: secret, Question: secret, Error: secret, Step: secret, Tool: "read_file", Status: "running"})
			}
			a.finishStream(task.ID, "failed", secret)
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), secret) {
				t.Fatalf("debug SSE disclosed private content: %s", body)
			}
			if !strings.Contains(string(body), "event: done") || !strings.Contains(string(body), "read_file") {
				t.Fatalf("diagnostic SSE lost completion/tool metadata: %s", body)
			}
			// The ordinary owner stream retains its authorized detailed errors.
			a.mu.Lock()
			task.Status = "failed"
			task.Error = secret
			a.mu.Unlock()
			w := request(a, "GET", "/api/sessions/redaction-session/runs/live-redaction/events", nil)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), secret) {
				t.Fatal("owner stream was accidentally redacted")
			}
		})
	}
}
