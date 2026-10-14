package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKnowledgeLocalReadFailurePublishesPrefixAndOtherSources(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires normal runtime user permissions")
	}
	a := testApp(t)
	for name, body := range map[string]string{"a-readable.md": "readable evidence", "z-unreadable.md": "not readable"} {
		if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.refPath, "reference.md"), []byte("independent evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(a.workPath, "z-unreadable.md")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0600)
	g, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal("one unreadable file blocked publication", err)
	}
	readable, reference, partial := false, false, false
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Path == "a-readable.md" && n.Text == "readable evidence" {
			readable = true
		}
		if n.Root == "context" && n.Path == "reference.md" {
			reference = true
		}
		if n.Path == "z-unreadable.md" && n.Text != "" {
			t.Fatal("unreadable text exposed")
		}
	}
	for _, s := range g.Sources {
		if s.Region == "workspace" {
			partial = s.State == "partial" && strings.Contains(s.Message, "权限")
		}
	}
	if !readable || !reference || !partial || !g.Truncated {
		t.Fatalf("partial publication lost evidence/boundary: %v %v %v", readable, reference, partial)
	}
	if err := os.Chmod(blocked, 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range recovered.Nodes {
		if n.Path == "z-unreadable.md" && n.Text == "not readable" {
			found = true
		}
	}
	if !found {
		t.Fatal("later permission repair did not recover source")
	}
}

func TestKnowledgeLocalUnavailableRootRetainsSameOriginObservation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires normal runtime user permissions")
	}
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "before.md"), []byte("prior observation"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	originalNodes := append([]knowledgeNode(nil), before.Nodes...)
	originalCoverage := map[string]knowledgeCoverage{}
	for _, s := range before.Sources {
		if s.Coverage != nil {
			v := *s.Coverage
			v.Reasons = append([]string(nil), v.Reasons...)
			originalCoverage[s.Region] = v
		}
	}
	if err := os.Chmod(a.workPath, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(a.workPath, 0700)
	after, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	found, marked := false, false
	for _, n := range after.Nodes {
		if n.Root == "workspace" && n.Path == "before.md" && n.Text == "prior observation" {
			found = true
		}
	}
	for _, s := range after.Sources {
		if s.Region == "workspace" {
			marked = s.State == "partial" && strings.Contains(s.Message, "上次可用观察") && s.Coverage != nil && !s.Coverage.TotalKnown
		}
	}
	if !found || !marked {
		t.Fatal("prior evidence lost or presented as current complete", found, marked)
	}
	if !reflect.DeepEqual(before.Nodes, originalNodes) {
		t.Fatal("previous published node slice mutated")
	}
	for _, s := range before.Sources {
		if s.Coverage != nil {
			copy := *s.Coverage
			copy.Reasons = append([]string(nil), copy.Reasons...)
			if !reflect.DeepEqual(copy, originalCoverage[s.Region]) {
				t.Fatal("previous published coverage mutated")
			}
		}
	}
}

func TestKnowledgeLocalFallbackRejectsChangedSourceIdentity(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires normal runtime user permissions")
	}
	a := testApp(t)
	for _, dir := range []string{"old-source", "new-source"} {
		if err := os.MkdirAll(filepath.Join(a.workPath, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "old-source", "old.md"), []byte("old source only"), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "moving", Name: "moving", Type: "local", Enabled: true, Config: SourceConfig{Path: "old-source"}})
	if _, err := a.knowledgeCurrentGraph(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "moving" {
			a.sourceRegistry.Sources[i].Config.Path = "new-source"
		}
	}
	a.mu.Unlock()
	inaccessible := filepath.Join(a.workPath, "new-source")
	if err := os.Chmod(inaccessible, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(inaccessible, 0700)
	current, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range current.Nodes {
		if n.Source == "moving" {
			t.Fatal("changed source inherited old observations")
		}
	}
}

func TestKnowledgeLocalFallbackDoesNotRestoreEvidenceForChangedText(t *testing.T) {
	ar := knowledgeArea{root: "source", source: "s", origin: "o"}
	old := knowledgeNode{ID: "a", Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Origin: ar.origin, Path: "a.md", Text: "before"}
	prior := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{old, {ID: "b", Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Origin: ar.origin, Path: "b.md", Text: "target"}}, Edges: []knowledgeEdge{{From: "a", To: "b", Kind: "mention", Evidence: "old text"}}}
	fresh := old
	fresh.Text = "after"
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{fresh}, Sources: []knowledgeSource{{Region: ar.region()}}}
	cache := newKnowledgeScanCache(nil)
	cache.priorGraph = &prior
	cache.localSourceFailure(&g, ar, "read failed", nil)
	if len(g.Nodes) != 2 || g.Nodes[0].Text != "after" {
		t.Fatal("prefix or prior node lost")
	}
	if len(g.Edges) != 0 {
		t.Fatal("fresh text inherited stale relation evidence")
	}
}

func TestKnowledgeClosedAppCannotPublishRetainedObservation(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "note.md"), []byte("prior"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.knowledgeCurrentGraph(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	revision := a.knowledgeUpdates.views[0].revision
	a.Close()
	if _, err := a.knowledgeCurrentGraph(context.Background(), false); err == nil {
		t.Fatal("closed app published candidate")
	}
	if a.knowledgeUpdates.views[0].revision != revision {
		t.Fatal("shutdown replaced published revision")
	}
	if view, release, _, err := a.knowledgeAcquireUpdatesView(context.Background(), false); err == nil {
		if release != nil {
			release()
		}
		t.Fatal("closed app replayed cached view", view != nil)
	}
}

func TestKnowledgeLocalFallbackBoundsNodesEdgesAndRetryMaterial(t *testing.T) {
	ar := knowledgeArea{root: "source", source: "s", origin: "o", location: "/fixture"}
	prior := knowledgeGraph{Workspace: "w", Sources: []knowledgeSource{{Region: ar.region(), Coverage: &knowledgeCoverage{TotalKnown: true}}}}
	cache := newKnowledgeScanCache(nil)
	cache.previous = map[string]knowledgeFileMemo{}
	for i := 0; i < knowledgeLocalFallbackMaxNodes+10; i++ {
		name := fmt.Sprintf("f%05d", i)
		prior.Nodes = append(prior.Nodes, knowledgeNode{ID: name, Path: name + ".md", Text: "cached", Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Origin: ar.origin})
		cache.previous[ar.location+"\x00"+name+".md"] = knowledgeFileMemo{text: "cached"}
	}
	for i := 0; i < knowledgeLocalFallbackMaxEdges+10; i++ {
		prior.Edges = append(prior.Edges, knowledgeEdge{From: "f00000", To: fmt.Sprintf("f%05d", i+1), Kind: "mention"})
	}
	cache.previous[ar.location+"\x00deleted.md"] = knowledgeFileMemo{text: "obsolete"}
	cache.priorGraph = &prior
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "fresh", Kind: "file", Path: "fresh.md", Region: ar.region(), Root: ar.root, Source: ar.source, Origin: ar.origin, Text: "fresh"}}, Sources: []knowledgeSource{{Region: ar.region()}}}
	cache.localSourceFailure(&g, ar, "unavailable", nil)
	if len(g.Nodes) > knowledgeLocalFallbackMaxNodes || len(g.Edges) > knowledgeLocalFallbackMaxEdges || len(cache.files) > knowledgeLocalFallbackMaxNodes {
		t.Fatal("unbounded local fallback", len(g.Nodes), len(g.Edges), len(cache.files))
	}
	if g.Nodes[0].ID != "fresh" || !g.Truncated {
		t.Fatal("fresh prefix displaced or incomplete graph marked complete")
	}
	if _, exists := cache.files[ar.location+"\x00deleted.md"]; exists {
		t.Fatal("discarded observations retained retry material")
	}
	if len(prior.Nodes) != knowledgeLocalFallbackMaxNodes+10 || !prior.Sources[0].Coverage.TotalKnown {
		t.Fatal("prior view mutated")
	}
}
