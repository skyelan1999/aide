package server

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
)

type issue107Logs struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *issue107Logs) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(p)
}
func (b *issue107Logs) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.text.String() }

func TestIssue107SnapshotFailureIsVisibleAndNextSaveIncludesPendingUsage(t *testing.T) {
	a := testApp(t)
	file := TokenStatsPath(a.dataPath)
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	var captured issue107Logs
	original := log.Writer()
	log.SetOutput(&captured)
	defer log.SetOutput(original)
	a.recordTokenUsage(TokenUsage{Model: "owned-private-model", Provider: "owned-private-provider", Prompt: 10, Completion: 2, Total: 12})
	logs := captured.String()
	failureCount := strings.Count(logs, "token_stats_persist_failed")
	if !strings.Contains(logs, "token_stats_persist_failed") {
		t.Error("snapshot failure was silently discarded")
	}
	if strings.Contains(logs, a.dataPath) || strings.Contains(logs, "owned-private-model") || strings.Contains(logs, "owned-private-provider") {
		t.Error("failure event exposed private accounting fields or path")
	}
	a.tokenStatsMu.Lock()
	calls := len(a.tokenCalls)
	a.tokenStatsMu.Unlock()
	if calls != 1 {
		t.Error("persistence failure discarded in-memory usage")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	a.recordTokenUsage(TokenUsage{Model: "owned-private-model", Provider: "owned-private-provider", Prompt: 20, Completion: 3, Total: 23})
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Calls []TokenCallRec `json:"calls"`
		Days  map[string]struct {
			Total int `json:"total"`
			Calls int `json:"calls"`
		} `json:"days"`
	}
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Calls) != 2 {
		t.Errorf("recovered snapshot persisted%d calls, want2", len(stored.Calls))
	}
	total, totalCalls := 0, 0
	for _, day := range stored.Days {
		total += day.Total
		totalCalls += day.Calls
	}
	if total != 35 || totalCalls != 2 {
		t.Error("recovered snapshot lost accumulated daily usage")
	}
	if strings.Count(captured.String(), "token_stats_persist_failed") != failureCount {
		t.Error("successful save emitted another persistence failure")
	}
}
