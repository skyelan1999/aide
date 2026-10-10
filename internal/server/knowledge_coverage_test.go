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
