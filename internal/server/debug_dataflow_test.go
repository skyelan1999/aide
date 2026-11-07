package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugSessionDataflowDumpIsAuthenticatedAndContentFree(t *testing.T) {
	a := testApp(t)
	const sessionID, runID, secret = "session-dataflow", "run-dataflow", "DATAFLOW_SECRET_MARKER"
	task := &Task{
		ID: runID, Mode: "chat", Prompt: secret, Status: "failed", Created: "2026-11-04T00:00:00Z",
		Model: "test-model", ExecutionSequence: 1, Error: secret,
		Steps:                []Step{{Name: "analysis", Status: "failed", Content: secret}},
		ToolUses:             []ToolUse{{Tool: "read_file", Args: secret, Result: secret}},
		ToolExecutionIntents: []ToolExecutionIntent{{CallID: "provider-call-secret", Tool: "read_file", State: "dispatched"}},
		RequestSnapshots: []RequestSnapshot{{
			Purpose: "analysis#1", Model: "test-model", Messages: []Message{{Role: "user", Content: secret}},
			Body: json.RawMessage(`{"prompt":"` + secret + `"}`), Outcome: "failed", FailureCode: "CONNECTION_FAILED",
			At: "2026-11-04T00:00:00Z", CompletedAt: "2026-11-04T00:00:01Z", ElapsedMillis: 1000,
		}},
		OrchestrationEvents: []OrchestrationEvent{{Kind: "model_request", Stage: "analysis", StartedAt: "2026-11-04T00:00:00Z", CompletedAt: "2026-11-04T00:00:01Z", ElapsedMillis: 1000, Outcome: "failed"}},
		Parallel:            &ParallelProgress{Parts: []ParallelPart{{Title: secret, Content: secret, Status: "completed"}}},
	}
	a.mu.Lock()
	a.sessions[sessionID] = &Session{
		ID: sessionID, Title: secret, Messages: []Message{{Role: "user", Content: secret}}, Runs: []*Task{task},
	}
	a.mu.Unlock()
	journalDir := filepath.Join(a.dataPath, "execution-events")
	if err := os.MkdirAll(journalDir, 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := json.Marshal(ExecutionEvent{Version: 1, TaskID: runID, Sequence: 1, Kind: "tool_dispatch", Step: "analysis", CallID: "provider-call-secret", Tool: "read_file", Digest: "sha256:abc", At: "2026-11-04T00:00:01Z"})
	if err != nil {
		t.Fatal(err)
	}
	journal = append(journal, '\n')
	if err := os.WriteFile(filepath.Join(journalDir, runID+".jsonl"), journal, 0600); err != nil {
		t.Fatal(err)
	}

	plainToken := enableDebugAccess(t, a)
	unauthorized := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/debug/sessions/"+sessionID+"/runs/"+runID+"/dataflow", nil))
	requireStatus(t, unauthorized, http.StatusUnauthorized)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/debug/sessions/"+sessionID+"/runs/"+runID+"/dataflow", nil)
	r.Header.Set("Authorization", "Bearer "+plainToken)
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, http.StatusOK)
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Fatalf("missing download disposition: %q", got)
	}
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "provider-call-secret") {
		t.Fatal("dataflow dump leaked message, prompt, provider body, tool payload, or raw provider call ID")
	}
	var dump debugDataflowDump
	if err := json.Unmarshal(w.Body.Bytes(), &dump); err != nil {
		t.Fatal(err)
	}
	if dump.Session.MessageContentBytes == 0 || len(dump.ProviderCalls) != 1 || dump.ProviderCalls[0].Status != "failed" {
		t.Fatalf("missing content-free provider/session metadata: %+v", dump)
	}
	if len(dump.Orchestration) != 1 || len(dump.Receipts) != 1 || dump.Receipts[0].ScopeHash == "" || dump.Receipts[0].Verified {
		t.Fatalf("timing or unverified parallel receipt missing: %+v", dump)
	}
	if len(dump.Journal.Events) != 1 || dump.Journal.Events[0].CallID != "call-1" || dump.Journal.Events[0].Step != "step-1" {
		t.Fatalf("journal identifiers were not projected safely: %+v", dump.Journal)
	}
	if !strings.Contains(dump.Journal.Events[0].Kind, "tool_dispatch") || len(dump.Anomalies) == 0 {
		t.Fatalf("journal event or anomaly analysis missing: %+v", dump)
	}
}
