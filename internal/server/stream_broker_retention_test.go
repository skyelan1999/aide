package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestIssue145BrokerGlobalRetention(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("active", "", streamRunning)
	b.Publish("active", "keep partial", "")
	for i := range 400 {
		id := fmt.Sprintf("ended-%d", i)
		b.Publish(id, "", streamRunning)
		b.Publish(id, strings.Repeat("x", 1024), "")
		b.Publish(id, "", streamInterrupted)
	}
	if len(b.runs) > 256 {
		t.Fatalf("retained %d runs, want at most 256", len(b.runs))
	}
	if text, status, ok := b.GetSessionLiveOutput("active"); !ok || status != streamRunning || text != "keep partial" {
		t.Fatalf("active partial lost: %q %q %v", text, status, ok)
	}
	if text, status, ok := b.GetSessionLiveOutput("ended-399"); !ok || status != streamInterrupted || len(text) != 1024 {
		t.Fatalf("recent interrupted output lost: %q %v", status, ok)
	}
}

func TestIssue145BrokerActiveCapacityAndLRU(t *testing.T) {
	b := NewStreamBroker()
	for i := range liveOutputMaxRuns {
		id := fmt.Sprint(i)
		b.Publish(id, "", streamRunning)
		b.Publish(id, "partial", "")
	}
	b.Publish("overflow", "", streamRunning)
	b.Publish("overflow", "not retained", "")
	if _, _, ok := b.GetSessionLiveOutput("overflow"); ok || len(b.runs) != liveOutputMaxRuns {
		t.Fatal("all-active saturation must not evict existing output or exceed capacity")
	}
	for i := range liveOutputMaxRuns {
		if text, _, ok := b.GetSessionLiveOutput(fmt.Sprint(i)); !ok || text != "partial" {
			t.Fatal("active mirror evicted")
		}
		b.Publish(fmt.Sprint(i), "", streamInterrupted)
	}
	b.GetSessionLiveOutput("0") // Most recently read terminal stays available.
	b.Publish("overflow", "", streamRunning)
	b.Publish("overflow", "new partial", "")
	if _, _, ok := b.GetSessionLiveOutput("1"); ok {
		t.Fatal("least recently used terminal was not evicted")
	}
	if text, status, ok := b.GetSessionLiveOutput("0"); !ok || text != "partial" || status != streamInterrupted {
		t.Fatal("recent interrupted mirror lost")
	}
	if text, _, ok := b.GetSessionLiveOutput("overflow"); !ok || text != "new partial" {
		t.Fatal("new mirror not admitted after terminal became evictable")
	}
}

func TestIssue145DeleteFencesConcurrentCallbacks(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "concurrent-delete"}
	a.sessions[s.ID] = s
	a.beginLiveRun(s.ID, "old")
	a.beginLiveRun(s.ID, "new")
	a.publishLiveDelta("old", "stale")
	a.finishLiveRun(s.ID, "old", "completed")
	a.publishLiveDelta("new", "fresh")
	if text, status, _ := a.liveBroker.GetSessionLiveOutput(s.ID); text != "fresh" || status != streamRunning {
		t.Fatal("superseded callbacks changed the replacement run", text, status)
	}
	var ready, finished sync.WaitGroup
	proceed := make(chan struct{})
	for range 8 {
		ready.Add(1)
		finished.Add(1)
		go func() {
			defer finished.Done()
			a.publishLiveDelta("new", "before")
			ready.Done()
			<-proceed
			for range 1000 {
				a.publishLiveDelta("new", "racing")
			}
		}()
	}
	ready.Wait()
	close(proceed)
	request := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+s.ID, nil)
	request.SetPathValue("id", s.ID)
	response := httptest.NewRecorder()
	a.deleteSession(response, request)
	finished.Wait()
	a.finishLiveRun(s.ID, "new", "cancelled")
	a.beginLiveRun(s.ID, "too-late")
	a.publishLiveDelta("too-late", "no resurrection")
	if response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	if _, _, ok := a.liveBroker.GetSessionLiveOutput(s.ID); ok {
		t.Fatal("deleted session was recreated by a concurrent or delayed callback")
	}
	if len(a.liveTaskSess) != 0 {
		t.Fatal("live task bindings retained after deletion", a.liveTaskSess)
	}
}

func TestIssue145ResetDropsLiveOutput(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "reset-live"}
	a.sessions[s.ID] = s
	a.beginLiveRun(s.ID, "reset-run")
	a.publishLiveDelta("reset-run", "private partial")
	a.mu.Lock()
	err := a.applySessionsAndMemoryReset()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	a.publishLiveDelta("reset-run", "late")
	a.finishLiveRun(s.ID, "reset-run", "cancelled")
	if _, _, ok := a.liveBroker.GetSessionLiveOutput(s.ID); ok {
		t.Fatal("reset left deleted live output")
	}
	if len(a.liveTaskSess) != 0 {
		t.Fatal("reset retained task bindings")
	}
}

func TestIssue145SessionDeleteEvictsOutput(t *testing.T) {
	for _, archivedBulk := range []bool{false, true} {
		t.Run(fmt.Sprint(archivedBulk), func(t *testing.T) {
			a := testApp(t)
			s := &Session{ID: "broker-delete", Archived: archivedBulk}
			a.sessions[s.ID] = s
			a.beginLiveRun(s.ID, "broker-run")
			a.liveBroker.Publish(s.ID, "deleted partial", "")
			request := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+s.ID, nil)
			request.SetPathValue("id", s.ID)
			response := httptest.NewRecorder()
			if archivedBulk {
				a.deleteAllArchived(response, request)
			} else {
				a.deleteSession(response, request)
			}
			if response.Code != http.StatusOK {
				t.Fatal(response.Code, response.Body.String())
			}
			if _, _, found := a.liveBroker.GetSessionLiveOutput(s.ID); found {
				t.Fatal("deleted session still retains live output")
			}
			a.finishLiveRun(s.ID, "broker-run", "cancelled")
			a.beginLiveRun(s.ID, "late-run")
			if _, _, found := a.liveBroker.GetSessionLiveOutput(s.ID); found {
				t.Fatal("late callbacks resurrected deleted live output")
			}
		})
	}
}
