package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeShutdownFlushesPrivacyEventAndCancelsWork(t *testing.T) {
	a := testApp(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("event subscription: %s", response.Status)
	}
	worker, stopWorker := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancels["shutdown-owned-task"] = stopWorker
	a.mu.Unlock()
	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	scanner := bufio.NewScanner(response.Body)
	found := false
	for scanner.Scan() {
		if scanner.Text() == "event: runtime-stopping" {
			found = true
		}
	}
	if !found || scanner.Err() != nil {
		t.Fatalf("missing flushed terminal privacy event: found=%v err=%v", found, scanner.Err())
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-worker.Done():
	default:
		t.Fatal("owned execution was not cancelled")
	}
	requireStatus(t, request(a, http.MethodPost, "/api/sessions", map[string]string{"title": "late"}), http.StatusServiceUnavailable)
}

func TestRuntimeShutdownPrioritizesPrivacyOverRefreshBacklog(t *testing.T) {
	a := &App{}
	ch := make(chan string, 32)
	for i := 0; i < cap(ch); i++ {
		ch <- "sessions:old"
	}
	a.globalSubs = map[chan string]struct{}{ch: {}}
	done := make(chan struct{})
	go func() {
		a.beginRuntimeShutdown()
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case message := <-ch:
			if strings.HasPrefix(message, "runtime-stopping:") {
				a.eventMu.Lock()
				delete(a.globalSubs, ch)
				a.eventMu.Unlock()
				<-done
				return
			}
		case <-deadline:
			t.Fatal("terminal privacy event was lost behind refreshes")
		}
	}
}

func TestSourceInstallationDisablesABEvenForReleaseBinary(t *testing.T) {
	t.Setenv("AIDE_INSTALLATION_KIND", "source")
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	if a.updateRuntimeMode() != "source" {
		t.Fatal("a Git installation must use Git updates regardless of image origin")
	}
	requireStatus(t, request(a, http.MethodPost, "/api/updates/switch", map[string]string{"target": "B"}), http.StatusConflict)
}

func TestReleaseInstallationRetainsABCapability(t *testing.T) {
	t.Setenv("AIDE_INSTALLATION_KIND", "release")
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	if a.updateRuntimeMode() != "release-image" {
		t.Fatal("Release package lost A/B capability")
	}
}
