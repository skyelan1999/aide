package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func progressiveFileCount(g knowledgeGraph) int {
	n := 0
	for _, v := range g.Nodes {
		if v.Root == "workspace" && v.Kind == "file" {
			n++
		}
	}
	return n
}
func TestKnowledgeProgressiveGraphAccumulatesAndRemoves(t *testing.T) {
	a := testApp(t)
	policy := defaultKnowledgeIndexPolicy()
	policy.FileBudget = 50
	a.mu.Lock()
	policyPath := a.knowledgeIndexPolicyPath()
	a.mu.Unlock()
	os.MkdirAll(filepath.Dir(policyPath), 0700)
	if e := atomicJSON(policyPath, policy); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 160; i++ {
		if e := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("f%03d.txt", i)), []byte("text"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	var previous *knowledgeScanCache
	count := 0
	var graph knowledgeGraph
	for i := 0; i < 40; i++ {
		c := newKnowledgeScanCache(previous)
		graph = a.knowledgeSnapshotModeCached(context.Background(), false, c)
		if c.err != nil {
			t.Fatal(c.err)
		}
		now := progressiveFileCount(graph)
		if now < count {
			t.Fatal("lost existing nodes", count, now)
		}
		count = now
		previous = c
	}
	if count < 160 {
		t.Fatal("did not accumulate", count)
	}
	source := coverageSource(t, graph, "workspace")
	if !source.Coverage.Progressive || source.Coverage.CatalogLimit == 0 {
		t.Fatal(source)
	}
	if e := os.Remove(filepath.Join(a.workPath, "f000.txt")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(a.workPath, "f159.txt"), []byte("changed-longer"), 0600); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 40; i++ {
		c := newKnowledgeScanCache(previous)
		graph = a.knowledgeSnapshotModeCached(context.Background(), false, c)
		if c.err != nil {
			t.Fatal(c.err)
		}
		previous = c
	}
	found := false
	for _, n := range graph.Nodes {
		if n.Path == "f000.txt" {
			t.Fatal("deleted node retained after cycle")
		}
		if n.Path == "f159.txt" {
			found = true
			if n.Text != "changed-longer" {
				t.Fatal(n)
			}
		}
	}
	if !found {
		t.Fatal("missing changed file")
	}
}

func TestKnowledgeProgressiveCatalogAccumulatesBeyondOneLargeDirectory(t *testing.T) {
	a := testApp(t)
	// Give the workspace the full saved budget so this exercises a realistic
	// multi-cycle catalogue rather than the direct one-shot snapshot path.
	a.mu.Lock()
	reference, sources := a.reference, a.sourceRegistry.Sources
	a.reference, a.sourceRegistry.Sources = nil, nil
	policyPath := a.knowledgeIndexPolicyPath()
	a.mu.Unlock()
	t.Cleanup(func() {
		a.mu.Lock()
		a.reference, a.sourceRegistry.Sources = reference, sources
		a.mu.Unlock()
	})
	policy := defaultKnowledgeIndexPolicy()
	policy.FileBudget = 4000
	policy.DirectoryBudget = 500
	if err := os.MkdirAll(filepath.Dir(policyPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(policyPath, policy); err != nil {
		t.Fatal(err)
	}
	const want = 3800
	flat := filepath.Join(a.workPath, "flat")
	if err := os.MkdirAll(flat, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < want; i++ {
		name := filepath.Join(flat, fmt.Sprintf("f%04d.txt", i))
		if err := os.WriteFile(name, []byte("text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var previous *knowledgeScanCache
	lastCount := 0
	var graph knowledgeGraph
	started := time.Now()
	for cycle := 0; cycle < 10; cycle++ {
		candidate := newKnowledgeScanCache(previous)
		graph = a.knowledgeSnapshotModeCached(context.Background(), false, candidate)
		if candidate.err != nil {
			t.Fatal(candidate.err)
		}
		count := 0
		for _, node := range graph.Nodes {
			if node.Root == "workspace" && node.Kind == "file" && strings.HasPrefix(node.Path, "flat/") {
				count++
			}
		}
		if count < lastCount {
			t.Fatalf("progressive view dropped files between cycles: %d -> %d", lastCount, count)
		}
		lastCount, previous = count, candidate
		if count == want {
			break
		}
	}
	source := coverageSource(t, graph, "workspace")
	if lastCount != want || source.State != "ready" || source.Coverage == nil || source.Coverage.Files < want || source.Coverage.Pending != 0 || len(source.Coverage.Reasons) != 0 {
		t.Fatalf("large progressive catalogue did not reach complete coverage: files=%d state=%s coverage=%+v", lastCount, source.State, source.Coverage)
	}
	t.Logf("progressively indexed %d flat files in %s", lastCount, time.Since(started).Round(time.Millisecond))
}

func TestKnowledgeProgressiveCancelledCandidateDoesNotAdvance(t *testing.T) {
	a := testApp(t)
	previous := newKnowledgeScanCache(nil)
	a.knowledgeSnapshotModeCached(context.Background(), false, previous)
	if previous.err != nil {
		t.Fatal(previous.err)
	}
	candidate := newKnowledgeScanCache(previous)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.knowledgeSnapshotModeCached(ctx, false, candidate)
	for key, v := range previous.discovery {
		if !v.Cursor.Complete || v.Cycle != 1 {
			t.Fatal(key, v)
		}
	}
}

func TestKnowledgeDiscoveryBudgetIncreaseKeepsCursor(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 80; i++ {
		if err := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("budget-%03d.txt", i)), []byte("entry"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(a.workPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	area := knowledgeArea{scope: knowledgeID("scope", "", "", ""), root: "workspace", source: "workspace", kind: "local", location: a.workPath, enabled: true}
	policy := defaultKnowledgeIndexPolicy()
	cache := newKnowledgeScanCache(nil)
	first, _, err := cache.discover(context.Background(), root, area, policy, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cursor.MaxEntries != 12 || first.Cursor.Complete {
		t.Fatalf("expected a partial small catalog, got max=%d complete=%v", first.Cursor.MaxEntries, first.Cursor.Complete)
	}
	cycle := first.Cycle
	visited := len(first.Cursor.Visited)
	second, _, err := cache.discover(context.Background(), root, area, policy, 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	if second.Cycle != cycle || second.Cursor.MaxEntries != 100 || len(second.Cursor.Visited) < visited {
		t.Fatalf("budget increase reset or shrank cursor: cycle %d->%d max=%d visited %d->%d", cycle, second.Cycle, second.Cursor.MaxEntries, visited, len(second.Cursor.Visited))
	}
}

func TestKnowledgeLocalDiscoveryRevisionIgnoresOnlyBudgets(t *testing.T) {
	base := defaultKnowledgeIndexPolicy()
	changedBudget := base
	changedBudget.FileBudget += 100
	changedBudget.DirectoryBudget += 20
	if knowledgeLocalDiscoveryRevision(base) != knowledgeLocalDiscoveryRevision(changedBudget) {
		t.Fatal("budget-only change invalidated local discovery")
	}
	changedScope := base
	changedScope.ScopePaths = map[string][]string{"workspace": {"docs"}}
	if knowledgeLocalDiscoveryRevision(base) == knowledgeLocalDiscoveryRevision(changedScope) {
		t.Fatal("scope change reused local discovery")
	}
}

func TestKnowledgeProgressiveUpdateAPI(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 850; i++ {
		if e := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("entry%04d.txt", i)), []byte("entry"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	requireStatus(t, request(a, "GET", "/api/knowledge-map/updates", nil), 200)
	for i := 0; i < 10; i++ {
		a.knowledgeUpdates.views[0].checked = a.knowledgeUpdates.views[0].checked.Add(-knowledgeUpdateInterval)
		requireStatus(t, request(a, "GET", "/api/knowledge-map/updates", nil), 200)
	}
	if progressiveFileCount(a.knowledgeUpdates.views[0].graph) < 850 {
		t.Fatal("API kept first batch only")
	}
}

func TestKnowledgeUpdatesServePublishedGraphWhileScanIsBusy(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 8; i++ {
		if e := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("cached-%d.txt", i)), []byte("cached"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	first := request(a, "GET", "/api/knowledge-map/updates", nil)
	requireStatus(t, first, 200)
	var baseline knowledgeUpdateResponse
	if err := json.Unmarshal(first.Body.Bytes(), &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Revision == "" || !baseline.Reset || len(baseline.Nodes) == 0 {
		t.Fatalf("initial snapshot was incomplete: %+v", baseline)
	}

	state := &a.knowledgeUpdates
	state.gate <- struct{}{} // Simulate another request owning a long scan.
	busy := request(a, "GET", "/api/knowledge-map/updates?cursor="+baseline.Revision, nil)
	<-state.gate
	requireStatus(t, busy, 200)
	var stale knowledgeUpdateResponse
	if err := json.Unmarshal(busy.Body.Bytes(), &stale); err != nil {
		t.Fatal(err)
	}
	if !stale.Stale || stale.Revision != baseline.Revision || len(stale.Nodes) != 0 || len(stale.Warnings) == 0 {
		t.Fatalf("busy scan did not return a clearly marked cached delta: %+v", stale)
	}

	// A workspace epoch change must invalidate the fallback immediately.
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	state.gate <- struct{}{}
	changed := request(a, "GET", "/api/knowledge-map/updates?cursor="+baseline.Revision, nil)
	<-state.gate
	requireStatus(t, changed, 503)
}

func TestKnowledgeProgressiveRejectsReplacedAncestor(t *testing.T) {
	a := testApp(t)
	visible := filepath.Join(a.workPath, "visible")
	if e := os.Mkdir(visible, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(visible, "note.txt"), []byte("public"), 0600); e != nil {
		t.Fatal(e)
	}
	previous := newKnowledgeScanCache(nil)
	g := a.knowledgeSnapshotModeCached(context.Background(), false, previous)
	if previous.err != nil {
		t.Fatal(previous.err)
	}
	seen := false
	for _, n := range g.Nodes {
		seen = seen || n.Root == "workspace" && n.Path == "visible/note.txt"
	}
	if !seen {
		t.Fatal("missing initial file")
	}
	hidden := filepath.Join(a.workPath, ".hidden")
	if e := os.Mkdir(hidden, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(hidden, "note.txt"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(visible, filepath.Join(a.workPath, "moved")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(".hidden", visible); e != nil {
		t.Fatal(e)
	}
	next := newKnowledgeScanCache(previous)
	g = a.knowledgeSnapshotModeCached(context.Background(), false, next)
	if next.err != nil {
		t.Fatal(next.err)
	}
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Path == "visible/note.txt" || n.Text == "secret" {
			t.Fatal("followed replaced ancestor", n)
		}
	}
}
