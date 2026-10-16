package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeBuiltinContextIndexedOnceWithReadCompatibility(t *testing.T) {
	a := testApp(t)
	const body = "# reference evidence\n"
	if err := os.WriteFile(filepath.Join(a.refPath, "reference.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	areas := a.knowledgeAreasLocked(a.wsID(), a.workspace.Name(), a.reference.Name())
	var alias knowledgeArea
	for _, ar := range areas {
		if ar.source == contextSource {
			alias = ar
		}
	}
	a.mu.Unlock()
	if alias.origin == "" {
		t.Fatal("missing existing built-in read identity")
	}
	g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
	counts := map[string]int{}
	var canonical knowledgeNode
	for _, n := range g.Nodes {
		if n.Kind == "file" && n.Path == "reference.md" {
			counts[n.Region]++
			if n.Root == "context" {
				canonical = n
			}
		}
	}
	if counts["context"] != 1 || counts["source:context"] != 0 {
		t.Fatalf("built-in reference indexed twice or canonical identity lost: %v", counts)
	}
	if canonical.ID != knowledgeID(a.wsID(), "context", "", "reference.md") {
		t.Fatal("canonical node identity changed", canonical.ID)
	}
	for _, source := range g.Sources {
		if source.Region == "source:context" {
			t.Fatal("redundant source still consumes index budget")
		}
	}
	check := func(b []byte, err error) {
		t.Helper()
		if err != nil || string(b) != body {
			t.Fatalf("existing reference read broken: %q %v", b, err)
		}
	}
	check(a.knowledgeReadNode(context.Background(), canonical, g.Workspace, 1024))
	check(a.knowledgeReadViewer(context.Background(), "context", "", canonical.Path, canonical.Origin, g.Workspace, 1024))
	check(a.knowledgeReadViewer(context.Background(), "source", contextSource, canonical.Path, alias.origin, g.Workspace, 1024))
	check(a.knowledgeReadSource(context.Background(), alias.src, canonical.Path, 1024))
	if _, err := a.knowledgeReadViewer(context.Background(), "source", contextSource, canonical.Path, alias.origin+"stale", g.Workspace, 1024); err == nil {
		t.Fatal("stale viewer origin accepted")
	}
	// An explicit custom alias and the independently writable docs source retain
	// their own identity even when they happen to refer to the same directory.
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "custom-ref", Name: "custom", Type: "local", Enabled: true, Config: SourceConfig{Path: "/context"}})
	a.mu.Unlock()
	g = a.knowledgeSnapshotModeCached(context.Background(), false, nil)
	custom, docs := false, false
	for _, source := range g.Sources {
		custom = custom || source.Region == "source:custom-ref"
		docs = docs || source.Region == "source:system-docs"
	}
	if !custom || !docs {
		t.Fatal("explicit custom or distinct docs source was hidden", custom, docs)
	}
	customFiles := 0
	for _, n := range g.Nodes {
		if n.Kind == "file" && n.Path == "reference.md" && n.Source == "custom-ref" {
			customFiles++
		}
	}
	if customFiles != 1 {
		t.Fatal("custom alias lost its file", customFiles)
	}
}
