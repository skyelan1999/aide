package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func coverageSource(t *testing.T, graph knowledgeGraph, region string) knowledgeSource {
	t.Helper()
	for _, source := range graph.Sources {
		if source.Region == region {
			return source
		}
	}
	t.Fatal("missing source", region)
	return knowledgeSource{}
}
func TestKnowledgeCoverageSmallLocal(t *testing.T) {
	a := testApp(t)
	before := coverageSource(t, a.knowledgeSnapshot(context.Background()), "workspace").Coverage
	if err := os.WriteFile(filepath.Join(a.workPath, "readme.md"), []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, ".secret"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	g := a.knowledgeSnapshot(context.Background())
	source := coverageSource(t, g, "workspace")
	if source.State != "ready" || source.Coverage == nil {
		t.Fatal(source)
	}
	c := source.Coverage
	if c.Files != before.Files+1 || c.Directories != before.Directories || c.TotalKnown || c.TextBytesPerFile != 8192 || len(c.Reasons) != 0 {
		t.Fatal(c)
	}
}
func TestKnowledgeCoverageFileBudget(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 801; i++ {
		if err := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("f%04d.txt", i)), []byte("text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := a.knowledgeSnapshot(context.Background())
	s := coverageSource(t, g, "workspace")
	c := s.Coverage
	if s.State != "partial" || c == nil || c.Files != c.FileBudget || c.TotalKnown {
		t.Fatal(s)
	}
	found := false
	for _, r := range c.Reasons {
		found = found || r == "files"
	}
	if !found && (!c.Progressive || c.Pending == 0) {
		t.Fatal("missing explicit budget or pending coverage", c)
	}
	// Coverage is part of revision identity and survives a delta with no node changes.
	rev := knowledgeGraphRevision(&g)
	view := &knowledgeUpdateView{graph: g, revision: rev.id, history: []knowledgeRevision{rev}}
	delta := knowledgeDelta(view, rev.id)
	if len(delta.Nodes) != 0 || coverageSource(t, knowledgeGraph{Sources: delta.Sources}, "workspace").Coverage.Files != c.Files {
		t.Fatal(delta)
	}
	old := rev.id
	g.Sources[1].Coverage.Reasons = append(c.Reasons, "time")
	if knowledgeGraphRevision(&g).id == old {
		t.Fatal("coverage change did not revise graph")
	}
}

func TestKnowledgeCoverageDirectoryBeyondLegacyThousandEntryLimit(t *testing.T) {
	a := testApp(t)
	// Keep this fixture to one active area so its configured budget can exceed
	// 1,000 files; production divides the global budget among active areas.
	a.mu.Lock()
	reference, sources := a.reference, a.sourceRegistry.Sources
	a.reference, a.sourceRegistry.Sources = nil, nil
	a.mu.Unlock()
	t.Cleanup(func() {
		a.mu.Lock()
		a.reference, a.sourceRegistry.Sources = reference, sources
		a.mu.Unlock()
	})
	const count = 1105
	flat := filepath.Join(a.workPath, "flat")
	if err := os.MkdirAll(flat, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		name := filepath.Join(flat, fmt.Sprintf("f%04d.txt", i))
		if err := os.WriteFile(name, []byte("text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 3000
	p.DirectoryBudget = 500
	v := indexPolicyResponse(t, a)
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{
		"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"],
	}), 200)

	// A nil scan cache deliberately exercises the bounded direct snapshot path.
	source := coverageSource(t, a.knowledgeSnapshotModeCached(context.Background(), false, nil), "workspace")
	if source.State != "ready" || source.Coverage == nil || source.Coverage.Files != count {
		t.Fatalf("large but in-budget directory was truncated: state=%s coverage=%+v", source.State, source.Coverage)
	}
	if len(source.Coverage.Reasons) != 0 {
		t.Fatalf("complete directory reported truncation: %+v", source.Coverage)
	}
}

func TestKnowledgeCoverageDirectoryBeyondDynamicEntryBudgetIsExplicit(t *testing.T) {
	a := testApp(t)
	// Isolate the workspace source so this fixture exercises its full configured
	// budget instead of the production per-area split.
	a.mu.Lock()
	reference, sources := a.reference, a.sourceRegistry.Sources
	a.reference, a.sourceRegistry.Sources = nil, nil
	a.mu.Unlock()
	t.Cleanup(func() {
		a.mu.Lock()
		a.reference, a.sourceRegistry.Sources = reference, sources
		a.mu.Unlock()
	})
	const count = 1105
	flat := filepath.Join(a.workPath, "flat")
	if err := os.MkdirAll(flat, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		name := filepath.Join(flat, fmt.Sprintf("f%04d.txt", i))
		if err := os.WriteFile(name, []byte("text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 800
	p.DirectoryBudget = 200
	v := indexPolicyResponse(t, a)
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{
		"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"],
	}), 200)

	// A nil scan cache deliberately exercises the bounded direct snapshot path.
	source := coverageSource(t, a.knowledgeSnapshotModeCached(context.Background(), false, nil), "workspace")
	if source.State != "partial" || source.Coverage == nil || source.Coverage.Files != p.FileBudget {
		t.Fatalf("directory over budget was not explicitly bounded: state=%s coverage=%+v", source.State, source.Coverage)
	}
	found := false
	for _, reason := range source.Coverage.Reasons {
		found = found || reason == "directory_entries"
	}
	if !found {
		t.Fatalf("directory boundary was not reported: %+v", source.Coverage)
	}
}

func TestKnowledgeCoverageDepthBudget(t *testing.T) {
	a := testApp(t)
	before := coverageSource(t, a.knowledgeSnapshot(context.Background()), "workspace").Coverage
	p := a.workPath
	for i := 0; i < 9; i++ {
		p = filepath.Join(p, "nested")
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s := coverageSource(t, a.knowledgeSnapshot(context.Background()), "workspace")
	found := false
	for _, reason := range s.Coverage.Reasons {
		found = found || reason == "depth"
	}
	if s.State != "partial" || !found || s.Coverage.Directories != before.Directories+7 {
		t.Fatal(s)
	}
}

func TestKnowledgeCoverageSessionLimit(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	for i := 1; i <= 201; i++ {
		id := fmt.Sprintf("session%04d", i)
		a.sessions[id] = &Session{ID: id, Number: i, Title: id}
	}
	a.mu.Unlock()
	source := coverageSource(t, a.knowledgeSnapshot(context.Background()), "sessions")
	if source.State != "partial" || source.NodeCount != 200 || source.Message == "" {
		t.Fatal(source)
	}
}
