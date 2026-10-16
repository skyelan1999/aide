package server

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"
)

func appendKnowledgeMentionsLegacy(g *knowledgeGraph) {
	pathCounts := map[string]int{}
	for _, n := range g.Nodes {
		if n.Kind == "file" {
			pathCounts[n.Path]++
		}
	}
	for _, from := range g.Nodes {
		if from.Text == "" || from.Kind == "symbol" {
			continue
		}
		refs := map[string]knowledgeTextReference{}
		for _, ref := range knowledgeTextReferences(from.Text) {
			rel := knowledgeLocalReference(ref.target)
			if rel == "" {
				continue
			}
			if from.Kind == "file" {
				rel = path.Clean(path.Join(path.Dir(from.Path), rel))
			} else {
				rel = path.Clean(rel)
			}
			if _, exists := refs[rel]; !exists {
				refs[rel] = ref
			}
		}
		for _, to := range g.Nodes {
			if to.Kind != "file" || from.ID == to.ID {
				continue
			}
			stable := "[" + to.ID + "]"
			index := strings.Index(from.Text, stable)
			ref, explicit := knowledgeTextReference{stable, index}, index >= 0
			if !explicit && to.Root == "context" {
				ref, explicit = knowledgeNamespacedReference(from.Text, "/context/"+to.Path)
			}
			if !explicit && to.Root == "source" {
				ref, explicit = knowledgeNamespacedReference(from.Text, to.Source+":"+to.Path)
			}
			localRef, hasLocal := refs[to.Path]
			sameArea := from.Region == to.Region && from.Root == to.Root && from.Source == to.Source
			local := hasLocal && (sameArea || from.Kind == "session" && pathCounts[to.Path] == 1)
			if !explicit && local {
				ref = localRef
			}
			if explicit || local {
				line, column, evidence := knowledgeReferenceLocation(from.Text, ref)
				g.Edges = append(g.Edges, knowledgeEdge{From: from.ID, To: to.ID, Kind: "mention", Line: line, Column: column, Evidence: evidence, Confidence: "explicit_reference"})
				if len(g.Edges) >= 4000 {
					g.Truncated = true
					return
				}
			}
		}
	}
}

func TestKnowledgeReferenceIndexedEquivalence(t *testing.T) {
	paths := []string{"file.md", "file.md.bak", "a/file.md", "space file.md", "a/b.go", "unicode-\u6587\u4ef6.md", "a", "a b"}
	texts := []string{"[F1] [F3]", "See [next](file.md)", "`space file.md`", "reference /context/file.md#L2", "source-a:a/b.go source-a:a/b.go.bak", "https://example.test/source-a:a/b.go", "source-a:a source-a:a b", "[F2] and [local](a/b.go)", "x[F1]x [[F3]]", "[encoded](space%20file.md)", "[unicode](unicode-\u6587\u4ef6.md)", "source-a:space file.md?line=1", "/context/a /context/a b"}
	for seed := 0; seed < 24; seed++ {
		g := knowledgeGraph{}
		for i := 0; i < 80; i++ {
			n := knowledgeNode{ID: fmt.Sprintf("F%d", i), Kind: "file", Path: paths[(i+seed)%len(paths)], Root: "workspace", Region: "workspace", Text: texts[(i*7+seed)%len(texts)]}
			if i%3 == 0 {
				n.Root = "context"
				n.Region = "context"
			} else if i%3 == 1 {
				n.Root = "source"
				n.Source = "source-a"
				n.Region = "source:source-a"
			}
			if i%11 == 0 {
				n.Kind = "session"
			}
			if i%17 == 0 {
				n.Kind = "symbol"
			}
			g.Nodes = append(g.Nodes, n)
		}
		legacy := g
		appendKnowledgeMentionsLegacy(&legacy)
		appendKnowledgeMentions(&g)
		if !reflect.DeepEqual(g.Edges, legacy.Edges) || g.Truncated != legacy.Truncated {
			t.Fatalf("seed %d differs: new=%+v old=%+v", seed, g.Edges, legacy.Edges)
		}
	}
}
func TestKnowledgeReferenceLargeRepositoryWork(t *testing.T) {
	g := knowledgeGraph{}
	for i := 0; i < 20000; i++ {
		g.Nodes = append(g.Nodes, knowledgeNode{ID: fmt.Sprintf("F%d", i), Kind: "file", Root: "workspace", Region: "workspace", Path: fmt.Sprintf("src/module%d/file%d.md", i/40, i), Text: "Ordinary repository documentation without a path reference."})
	}
	legacy := g
	before := time.Now()
	appendKnowledgeMentionsLegacy(&legacy)
	oldTime := time.Since(before)
	before = time.Now()
	appendKnowledgeMentions(&g)
	newTime := time.Since(before)
	if !reflect.DeepEqual(g.Edges, legacy.Edges) {
		t.Fatal("large graph changed relations")
	}
	t.Logf("20000 text/file nodes: legacy=%s indexed=%s", oldTime, newTime)
}
