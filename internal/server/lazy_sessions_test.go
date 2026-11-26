package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lazyHistoryFixture(t *testing.T) (*App, string, string, []byte) {
	t.Helper()
	t.Setenv("AIDE_LAZY_SESSIONS", "1")
	a := testApp(t)
	s := mustPostSession(t, a, "lazy history")
	a.mu.Lock()
	stored := a.sessions[s.ID]
	stored.Messages = []Message{{Role: "user", Content: "original historical evidence"}}
	stored.Runs = []*Task{{ID: newID(), Prompt: "latest user prompt", Status: "completed", Steps: []Step{{Name: "result", Content: "full tool evidence"}}}}
	if err := a.save(stored); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	path := SessionPath(a.dataPath, s.ID, "active")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if !restarted.sessions[s.ID].historyDeferred {
		t.Fatal("history was eagerly decoded")
	}
	return restarted, s.ID, path, raw
}

func TestLazySessionListDoesNotHydrateAndSaveRefusesPartialHistory(t *testing.T) {
	a, id, path, before := lazyHistoryFixture(t)
	w := request(a, "GET", "/api/sessions", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"preview":"latest user prompt"`) {
		t.Fatal("deferred session list omitted its bounded prompt preview")
	}
	a.mu.Lock()
	s := a.sessions[id]
	if !s.historyDeferred {
		t.Fatal("listing hydrated history")
	}
	err := a.save(s)
	a.mu.Unlock()
	if err == nil {
		t.Fatal("partial history overwrite accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("history changed")
	}
	if a.integrityRep.DeferredSessions == 0 || a.integrityRep.Status != IntegrityPending {
		t.Fatal("deferred validation was reported complete")
	}
}

func TestLazySessionGETAndMutationPreserveFullHistory(t *testing.T) {
	a, id, path, _ := lazyHistoryFixture(t)
	w := request(a, "GET", "/api/sessions/"+id+"?limit=20", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "original historical evidence") || !strings.Contains(w.Body.String(), "full tool evidence") {
		t.Fatal("full history omitted")
	}
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+id, map[string]any{"archived": true}), 200)
	b, err := os.ReadFile(SessionPath(a.dataPath, id, "archived"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("original historical evidence")) || !bytes.Contains(b, []byte("full tool evidence")) {
		t.Fatal("archive truncated history")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("old bucket copy retained")
	}
}

func TestLazySessionConcurrentLoadsDoNotBlockListing(t *testing.T) {
	a, id, _, _ := lazyHistoryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	var once sync.Once
	a.sessionHistoryReader = func(path, id string) (*Session, error) {
		count.Add(1)
		once.Do(func() { close(entered) })
		<-release
		return readSessionHistory(path, id)
	}
	var group sync.WaitGroup
	failures := make(chan int, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			w := request(a, "GET", "/api/sessions/"+id, nil)
			if w.Code != 200 {
				failures <- w.Code
			}
		}()
	}
	<-entered
	listed := make(chan *httptest.ResponseRecorder, 1)
	go func() { listed <- request(a, "GET", "/api/sessions", nil) }()
	select {
	case w := <-listed:
		requireStatus(t, w, 200)
	case <-time.After(time.Second):
		close(release)
		group.Wait()
		t.Fatal("listing blocked on history I/O")
	}
	close(release)
	group.Wait()
	close(failures)
	for status := range failures {
		t.Errorf("history status %d", status)
	}
	if count.Load() != 1 {
		t.Fatalf("duplicate history loads: %d", count.Load())
	}
}

func TestLazySessionSameStampCorruptionFailsClosed(t *testing.T) {
	a, id, path, before := lazyHistoryFixture(t)
	info, _ := os.Stat(path)
	bad := append([]byte(nil), before...)
	bad[0] = 'x'
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/sessions/"+id, nil), 503)
	requireStatus(t, request(a, "GET", "/api/export", nil), 503)
	if !a.sessions[id].historyDeferred {
		t.Fatal("corrupt history replaced metadata")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(bad, after) {
		t.Fatal("corrupt original overwritten")
	}
}

func TestLazySessionCatalogueTamperingAndStaleStampFallBack(t *testing.T) {
	for _, mode := range []string{"tamper", "missing", "changed"} {
		t.Run(mode, func(t *testing.T) {
			a, id, path, raw := lazyHistoryFixture(t)
			work, reference, data := a.workPath, a.refPath, a.dataPath
			a.Close()
			store := &lazySessionStore{data: data}
			index := store.cataloguePath(path)
			switch mode {
			case "missing":
				_ = os.Remove(index)
			case "tamper":
				var entry sessionCatalogueEntry
				b, _ := os.ReadFile(index)
				_ = json.Unmarshal(b, &entry)
				entry.Metadata.Title = "forged"
				_ = atomicJSON(index, entry)
			case "changed":
				var s Session
				_ = json.Unmarshal(raw, &s)
				s.Title = "new title"
				if err := atomicJSON(path, &s); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := New(work, reference, data)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			if restarted.sessions[id].Title == "forged" {
				t.Fatal("unauthenticated metadata accepted")
			}
			if mode == "changed" && restarted.sessions[id].Title != "new title" {
				t.Fatal("stale metadata retained")
			}
			if err := restarted.ensureSessionHistory(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			if len(restarted.sessions[id].Messages) != 1 {
				t.Fatal("fallback lost history")
			}
		})
	}
}

func TestLazySessionRecoveryIsPersistedWhenOpened(t *testing.T) {
	a, id, _, _ := lazyHistoryFixture(t)
	requireStatus(t, request(a, "GET", "/api/sessions/"+id, nil), 200)
	a.mu.Lock()
	s := a.sessions[id]
	s.Runs[0].Status = "running"
	s.Runs[0].StopRequested = true
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if !restarted.sessions[id].historyDeferred {
		t.Fatal("recovery forced eager corpus loading")
	}
	requireStatus(t, request(restarted, "GET", "/api/sessions/"+id, nil), 200)
	b, _ := os.ReadFile(SessionPath(data, id, "active"))
	var saved Session
	_ = json.Unmarshal(b, &saved)
	if saved.Runs[0].Status != "cancelled" || saved.Runs[0].CanResume {
		t.Fatal("stop recovery not durable")
	}
}

func TestLazySessionExportSearchAndCrossSessionAccessLoadBodies(t *testing.T) {
	a, id, _, _ := lazyHistoryFixture(t)
	w := request(a, "GET", "/api/search?q=historical", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), id) {
		t.Fatal("search skipped deferred history")
	}
	w = request(a, "GET", "/api/export", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "full tool evidence") {
		t.Fatal("export truncated history")
	}
	a.mu.Lock()
	s := a.resolveSessionRef("#" + strconv.Itoa(a.sessions[id].Number))
	a.mu.Unlock()
	if s == nil || len(s.Messages) != 1 {
		t.Fatal("numbered lookup missed body")
	}
}

func TestLazySessionCancelledLoadDoesNotPublishPartialState(t *testing.T) {
	a, id, path, before := lazyHistoryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	a.sessionHistoryReader = func(path, id string) (*Session, error) {
		close(entered)
		<-release
		return readSessionHistory(path, id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.ensureSessionHistory(ctx, id) }()
	<-entered
	cancel()
	close(release)
	if err := <-done; err != context.Canceled {
		t.Fatalf("want cancellation, got %v", err)
	}
	a.mu.Lock()
	deferred := a.sessions[id].historyDeferred
	a.mu.Unlock()
	if !deferred {
		t.Fatal("cancelled load published state")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled load changed original")
	}
}

func TestLazySessionNumberRepairLoadsBeforeWriting(t *testing.T) {
	a, id, path, raw := lazyHistoryFixture(t)
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	s.Number = 0
	b, _ := json.MarshalIndent(&s, "", "  ")
	if err := atomicJSONBytes(path, b); err != nil {
		t.Fatal(err)
	}
	(&lazySessionStore{data: data}).record(path, b, &s)
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	stored, _ := os.ReadFile(path)
	var got Session
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatal(err)
	}
	if got.Number <= 0 || len(got.Messages) != 1 || got.Messages[0].Content != "original historical evidence" {
		t.Fatal("number repair lost history")
	}
	if restarted.sessions[id].historyDeferred {
		t.Fatal("repaired session was not hydrated")
	}
}

func TestLazySessionRecoverySaveFailureKeepsDeferredOriginal(t *testing.T) {
	a, id, path, _ := lazyHistoryFixture(t)
	requireStatus(t, request(a, "GET", "/api/sessions/"+id, nil), 200)
	a.mu.Lock()
	s := a.sessions[id]
	s.Runs[0].Status = "running"
	s.Runs[0].StopRequested = true
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	before, _ := os.ReadFile(path)
	restarted.sessionHistoryReader = func(path, id string) (*Session, error) {
		loaded, err := readSessionHistory(path, id)
		if err != nil {
			return nil, err
		}
		if err := os.Rename(path, path+".original"); err != nil {
			return nil, err
		}
		if err := os.Mkdir(path, 0700); err != nil {
			return nil, err
		}
		return loaded, nil
	}
	requireStatus(t, request(restarted, "GET", "/api/sessions/"+id, nil), 503)
	if !restarted.sessions[id].historyDeferred {
		t.Fatal("failed recovery save discarded deferred original")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".original", path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed save changed original")
	}
	restarted.sessionHistoryReader = nil
	requireStatus(t, request(restarted, "GET", "/api/sessions/"+id, nil), 200)
}

func TestLazySessionPassiveStatusDoesNotLoadOldHistories(t *testing.T) {
	a, id, _, _ := lazyHistoryFixture(t)
	a.mu.Lock()
	_ = a.humanApprovalEventsLocked()
	_ = a.assistantWorkLedgerLocked()
	deferred := a.sessions[id].historyDeferred
	a.mu.Unlock()
	if !deferred {
		t.Fatal("passive polling eagerly loaded old history")
	}
}
