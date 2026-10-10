package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
