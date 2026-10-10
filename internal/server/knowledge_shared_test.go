package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sharedSnapshot(t *testing.T, a *App) knowledgeGraph {
	t.Helper()
	w := request(a, "GET", "/api/knowledge-map", nil)
	requireStatus(t, w, 200)
	var g knowledgeGraph
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	return g
}
func sharedTargetCount(g knowledgeGraph) int {
	count := 0
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Kind == "file" && strings.HasPrefix(n.Path, "restart-") {
			count++
		}
	}
	return count
}
func TestKnowledgeSharedSnapshotSearchAndUpdates(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	first := sharedSnapshot(t, a)
	second := sharedSnapshot(t, a)
	if sharedTargetCount(first) < 1 || sharedTargetCount(second) <= sharedTargetCount(first) {
		t.Fatal("snapshots did not continue", sharedTargetCount(first), sharedTargetCount(second))
	}
	var g knowledgeGraph
	for i := 0; i < 25; i++ {
		g = sharedSnapshot(t, a)
		if sharedTargetCount(g) == 120 {
			break
		}
	}
	if sharedTargetCount(g) != 120 {
		t.Fatal("catalogue did not accumulate", sharedTargetCount(g))
	}
	var target knowledgeNode
	for _, n := range g.Nodes {
		if n.Path == "restart-119.txt" && n.Root == "workspace" {
			target = n
		}
	}
	if target.ID == "" {
		t.Fatal("late file absent")
	}
	w := request(a, "POST", "/api/knowledge-map/documents/search", documentRequest{Query: "confidential", Mode: "original", Workspace: g.Workspace, IDs: []string{target.ID}})
	requireStatus(t, w, 200)
	var result documentResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].File.ID != target.ID {
		t.Fatal("search lost accumulated node", w.Body.String())
	}
	hit := result.Hits[0]
	w = request(a, "POST", "/api/knowledge-map/documents/reference", map[string]any{"id": target.ID, "workspace": g.Workspace, "origin": target.Origin, "locator": hit.Locator, "offset": hit.Offset, "hash": hit.Hash})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/knowledge-map?id="+target.ID+"&workspace="+g.Workspace, nil)
	requireStatus(t, w, 200)
	if count := localUpdateCount(t, a); count != 120 {
		t.Fatal("updates forked catalogue", count)
	}
	// A fresh service can resume the catalogue created entirely by direct APIs.
	restarted, err := New(a.workPath, a.refPath, a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if count := sharedTargetCount(sharedSnapshot(t, restarted)); count != 120 {
		t.Fatal("direct catalogue not persisted", count)
	}
	// External policy edits invalidate memory as well as persisted identity.
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 50
	p.ScopePaths = map[string][]string{"workspace": {"restart-119.txt"}}
	if err := atomicJSON(restarted.knowledgeIndexPolicyPath(), p); err != nil {
		t.Fatal(err)
	}
	if count := sharedTargetCount(sharedSnapshot(t, restarted)); count != 1 {
		t.Fatal("old policy revived", count)
	}
}
func TestKnowledgeSharedCancellationAndConcurrency(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	sharedSnapshot(t, a)
	files, _ := filepath.Glob(filepath.Join(a.dataPath, "cache", "knowledge-cursors", "local-*.json"))
	before := map[string]string{}
	for _, p := range files {
		b, _ := os.ReadFile(p)
		before[p] = string(b)
	}
	a.knowledgeUpdates.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, err := a.knowledgeCurrentGraph(ctx, false)
	cancel()
	<-a.knowledgeUpdates.gate
	if err == nil {
		t.Fatal("busy cancelled scan succeeded")
	}
	for p, b := range before {
		now, _ := os.ReadFile(p)
		if string(now) != b {
			t.Fatal("cancelled wait changed checkpoint")
		}
	}
	// Snapshot and update requests use one gate and independent immutable responses.
	var wg sync.WaitGroup
	failures := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := "/api/knowledge-map"
			if i%2 == 0 {
				url += "/updates"
			}
			w := request(a, "GET", url, nil)
			if w.Code != 200 {
				failures <- w.Body.String()
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for msg := range failures {
		t.Error(msg)
	}
	g := sharedSnapshot(t, a)
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if ids[n.ID] {
			t.Fatal("duplicate", n.ID)
		}
		ids[n.ID] = true
	}
	if sharedTargetCount(g) < sharedTargetCount(a.knowledgeUpdates.views[0].graph) {
		t.Fatal("snapshot lost published nodes")
	}
}
