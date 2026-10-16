package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func backgroundKnowledgeFixture(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.mu.Lock()
	a.sourceRegistry.Sources = nil
	p := a.knowledgeIndexPolicyPath()
	a.mu.Unlock()
	policy := defaultKnowledgeIndexPolicy()
	policy.FileBudget, policy.DirectoryBudget = 50, 20
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(p, policy); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 170; i++ {
		name := fmt.Sprintf("entry%04d.md", i)
		if err := os.WriteFile(filepath.Join(a.workPath, name), []byte("See [next](entry0169.md)"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestKnowledgeBackgroundCompletesWithoutFurtherRequests(t *testing.T) {
	a := backgroundKnowledgeFixture(t)
	first := request(a, "GET", "/api/knowledge-map/updates?background=1", nil)
	requireStatus(t, first, 200)
	var initial knowledgeUpdateResponse
	if err := json.Unmarshal(first.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if !knowledgeGraphScanning(knowledgeGraph{Sources: initial.Sources}) {
		t.Fatal("fixture must require subsequent batches")
	}
	// No more HTTP calls: only observe immutable publications. There is no
	// browser timer, animation frame, focus event or active request driving it.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		published := a.knowledgeUpdates.published[0].Load()
		if published != nil && !knowledgeGraphScanning(published.view.graph) {
			g := published.view.graph
			if progressiveFileCount(g) != 170 || published.view.revision == initial.Revision {
				t.Fatalf("incomplete background graph: %d", progressiveFileCount(g))
			}
			mentions := 0
			for _, e := range g.Edges {
				if e.Kind == "mention" {
					mentions++
				}
			}
			if mentions < 160 {
				t.Fatalf("late-discovered target did not acquire links: %d", mentions)
			}
			a.Close() // Also waits for the tracked worker to release its gate.
			if a.knowledgeUpdates.continuing[0] != nil {
				t.Fatal("completed worker retained its owner")
			}
			t.Logf("without further requests: %d files, %d mention edges", progressiveFileCount(g), mentions)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("background catalogue stalled without browser activity")
}

func TestKnowledgeBackgroundOwnershipCancellation(t *testing.T) {
	for _, changed := range []string{"workspace", "policy", "shutdown"} {
		t.Run(changed, func(t *testing.T) {
			a := backgroundKnowledgeFixture(t)
			requireStatus(t, request(a, "GET", "/api/knowledge-map/updates?background=1", nil), 200)
			s := &a.knowledgeUpdates
			s.gate <- struct{}{}
			owner := s.continuing[0]
			a.knowledgeContinueLocked(false)
			if owner == nil || s.continuing[0] != owner {
				t.Fatal("duplicate request replaced the running owner")
			}
			before := s.published[0].Load()
			switch changed {
			case "workspace":
				a.mu.Lock()
				a.wsRevision++
				a.mu.Unlock()
			case "policy":
				p := defaultKnowledgeIndexPolicy()
				p.FileBudget = 100
				if err := atomicJSON(a.knowledgeIndexPolicyPath(), p); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				a.bgCancel()
			}
			<-s.gate
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				s.gate <- struct{}{}
				done := s.continuing[0] == nil
				<-s.gate
				if done {
					if s.published[0].Load() != before {
						t.Fatal("old owner published after identity change")
					}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("old worker did not stop")
		})
	}
}

func TestKnowledgeBackgroundRequestCancellationIsIndependent(t *testing.T) {
	a := backgroundKnowledgeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	view, release, err := a.knowledgeAcquireView(ctx, false, true)
	if err != nil || !knowledgeGraphScanning(view.graph) {
		t.Fatalf("initial batch: %v", err)
	}
	a.knowledgeContinueLocked(false)
	release()
	cancel()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p := a.knowledgeUpdates.published[0].Load()
		if p != nil && !knowledgeGraphScanning(p.view.graph) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("request cancellation stopped app-owned continuation")
}

func TestKnowledgeBackgroundCancelledFirstBatchResumes(t *testing.T) {
	a := backgroundKnowledgeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &a.knowledgeUpdates
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	s.gate <- struct{}{}
	_, err := a.knowledgeAcquireViewLocked(ctx, false, true)
	<-s.gate
	if err == nil {
		t.Fatal("cancelled first scan unexpectedly published")
	}
	a.knowledgeContinueFailed(false)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p := a.knowledgeUpdates.published[0].Load()
		if p != nil && !knowledgeGraphScanning(p.view.graph) {
			if progressiveFileCount(p.view.graph) != 170 {
				t.Fatal("incomplete resumed scan")
			}
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("first failed scan still needed a browser retry")
}

func TestKnowledgeBackgroundFreezesCompletedSources(t *testing.T) {
	a := backgroundKnowledgeFixture(t)
	requireStatus(t, request(a, "GET", "/api/knowledge-map/updates?background=1", nil), 200)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p := a.knowledgeUpdates.published[0].Load()
		if p != nil && !knowledgeGraphScanning(p.view.graph) {
			a.Close()
			for _, v := range a.knowledgeUpdates.views[0].cache.discovery {
				if v.Cycle != 1 {
					t.Fatalf("finished source was repeatedly rescanned: cycle %d", v.Cycle)
				}
			}
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("unequal source sizes never reached a common completed view")
}
