package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoCompactionCircuitCooldownAndHalfOpenRecovery(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= compactionMaxAttempts {
			http.Error(w, "fixture-secret-provider-detail", http.StatusServiceUnavailable)
			return
		}
		parallelReply(w, `{"goal":"recovered continuity","decisions":[],"files":[],"facts":["ID-R42"],"pending":[]}`)
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "test", Models: []ModelRef{{ID: "test", ContextWindow: 16000}}}
	a.mu.Lock()
	s = a.sessions[s.ID]
	for i := 0; i < 4; i++ {
		s.Messages = append(s.Messages,
			Message{Role: "user", Content: strings.Repeat("user context ", 900)},
			Message{Role: "assistant", Content: strings.Repeat("assistant context ", 900)},
		)
	}
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.settings = cfg
	original := cloneCompactMessages(s.Messages)
	a.mu.Unlock()

	ch, stop := a.subscribeStream("compaction-circuit-test")
	defer stop()
	a.maybeAutoCompact(context.Background(), s, cfg, "compaction-circuit-test")
	if got := calls.Load(); got != compactionMaxAttempts {
		t.Fatalf("first automatic attempt calls=%d, want bounded %d", got, compactionMaxAttempts)
	}
	key := compactionCircuitKey{sessionID: s.ID, model: cfg.Model}
	a.mu.Lock()
	state := a.contextCompactionCircuits[key]
	cooledMessages := cloneCompactMessages(a.sessions[s.ID].Messages)
	a.mu.Unlock()
	if state.failures != 1 || !state.retryAfter.After(time.Now()) {
		t.Fatalf("failure did not open cooldown: %+v", state)
	}
	if !compactSnapshotMatches(cooledMessages, original) {
		t.Fatal("failed automatic compaction changed source history")
	}
	var sawRetry, sawSafeFailure bool
	for len(ch) > 0 {
		event := <-ch
		if event.Event != "compaction" {
			continue
		}
		if event.RetryAttempt > 1 && event.RetryDelayMs > 0 {
			sawRetry = true
		}
		if event.Phase == "failed" && strings.Contains(event.Text, "HTTP 503") && strings.Contains(event.Text, "后台将在") && !strings.Contains(event.Text, "fixture-secret") {
			sawSafeFailure = true
		}
	}
	if !sawRetry || !sawSafeFailure {
		t.Fatalf("missing retry metadata or safe terminal failure: retry=%t safeFailure=%t", sawRetry, sawSafeFailure)
	}

	a.maybeAutoCompact(context.Background(), s, cfg, "compaction-circuit-test")
	if got := calls.Load(); got != compactionMaxAttempts {
		t.Fatalf("cooldown allowed an immediate retry: calls=%d", got)
	}
	a.mu.Lock()
	otherModelWait := a.autoCompactionRetryAfterLocked(compactionCircuitKey{sessionID: s.ID, model: "another-model"}, time.Now())
	state.retryAfter = time.Now().Add(-time.Second)
	a.contextCompactionCircuits[key] = state
	a.mu.Unlock()
	if otherModelWait != 0 {
		t.Fatalf("cooldown leaked to another model: %s", otherModelWait)
	}

	// The first automatic attempt after the deadline is the half-open probe.
	a.maybeAutoCompact(context.Background(), s, cfg, "compaction-circuit-test")
	a.mu.Lock()
	_, stillCooling := a.contextCompactionCircuits[key]
	compact := a.sessions[s.ID].Compact
	a.mu.Unlock()
	if stillCooling || compact == "" || calls.Load() <= compactionMaxAttempts {
		t.Fatalf("half-open probe did not recover: calls=%d cooling=%t compact=%q", calls.Load(), stillCooling, compact)
	}
}

func TestCompactionCircuitBoundsAndExpiresState(t *testing.T) {
	a := testApp(t)
	now := time.Now()
	for i := 0; i < compactionCircuitMaxKeys+10; i++ {
		key := compactionCircuitKey{sessionID: string(rune(i + 1)), model: "test"}
		a.recordAutoCompactionFailureLocked(key, now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(a.contextCompactionCircuits) != compactionCircuitMaxKeys {
		t.Fatalf("circuit keys=%d, want %d", len(a.contextCompactionCircuits), compactionCircuitMaxKeys)
	}
	key := compactionCircuitKey{sessionID: "expired", model: "test"}
	a.recordAutoCompactionFailureLocked(key, now)
	if wait := a.autoCompactionRetryAfterLocked(key, now.Add(compactionCircuitMaxAge)); wait != 0 {
		t.Fatalf("expired circuit still blocks recovery: %s", wait)
	}
}
