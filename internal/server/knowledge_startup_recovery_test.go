package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoveredKnowledgeApp(t *testing.T) (*App, string, []byte) {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := defaultWorkspaceConfig()
	cfg.Workspace.Path = "missing"
	cfg.Docs.Path = "missing/docs"
	if err := atomicJSON(filepath.Join(work, wsConfigFile), cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(work, wsConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(work, filepath.Join(base, "reference"), filepath.Join(base, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if err := os.WriteFile(filepath.Join(work, "readme.md"), []byte("available workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "missing-other"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "missing-other", "guide.md"), []byte("independent source"), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "independent", Name: "independent", Type: "local", Enabled: true, Config: SourceConfig{Path: "missing-other"}})
	return a, work, before
}

func TestKnowledgeStartupUnavailableSourceDoesNotRetarget(t *testing.T) {
	a, work, before := recoveredKnowledgeApp(t)
	// Even if a descendant becomes readable, repair must be explicit. A failed
	// workspace must not silently mix its descendants into the temporary workspace.
	if err := os.MkdirAll(filepath.Join(work, "missing", "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "missing", "docs", "old.md"), []byte("old workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	docs, ok := a.findSource(systemDocsSource)
	if !ok {
		t.Fatal("system docs missing")
	}
	if docs.Config.Path != "missing/docs" || !docs.Enabled {
		t.Fatal("saved source binding changed")
	}
	if root, err := a.localSourceRoot(docs); err == nil {
		root.Close()
		t.Fatal("unavailable source was opened instead of requiring repair")
	}
	g, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	workspaceSeen, independentSeen, unavailableSeen := false, false, false
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Path == "readme.md" {
			workspaceSeen = true
		}
		if n.Source == "independent" {
			independentSeen = true
		}
		if n.Source == systemDocsSource {
			t.Fatal("unavailable source acquired nodes")
		}
	}
	for _, src := range g.Sources {
		if src.ID == systemDocsSource {
			unavailableSeen = src.State == "unavailable" && strings.Contains(src.Message, "修复工作空间")
		}
	}
	if !workspaceSeen || !independentSeen || !unavailableSeen {
		t.Fatalf("availability boundaries lost: workspace=%v independent=%v unavailable=%v", workspaceSeen, independentSeen, unavailableSeen)
	}
	after, _ := os.ReadFile(filepath.Join(work, wsConfigFile))
	if !bytes.Equal(before, after) {
		t.Fatal("startup scan rewrote saved workspace config")
	}
	sibling, _ := a.findSource("independent")
	root, err := a.localSourceRoot(sibling)
	if err != nil {
		t.Fatal("path prefix sibling incorrectly blocked", err)
	}
	root.Close()
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": ""}, "docs": map[string]any{"path": "missing-other", "location": "local"}}), 200)
	if a.wsStartupUnavailableRoot.Load() != nil {
		t.Fatal("explicit repair did not clear source recovery boundary")
	}
	docs, _ = a.findSource(systemDocsSource)
	root, err = a.localSourceRoot(docs)
	if err != nil {
		t.Fatal("repaired source remains blocked", err)
	}
	defer root.Close()
	body, err := root.ReadFile("guide.md")
	if err != nil || string(body) != "independent source" {
		t.Fatal("repaired source not readable", err)
	}
}
