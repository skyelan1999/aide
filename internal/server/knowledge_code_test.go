package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestKnowledgeCodeConfidenceAndUnresolved(t *testing.T) {
	files := []knowledgeCodeFile{
		{ID: "Fa", Module: "example.test/project", Node: knowledgeNode{ID: "Fa", Path: "main.go", Root: "workspace"}, Text: "package main\nimport (\"example.test/project/lib\"; \"fmt\")\nfunc known(){}\nfunc entry(){known();lib.Target();fmt.Println(\"x\");missing();var f func();f()}\n"},
		{ID: "Fb", Module: "example.test/project", Node: knowledgeNode{ID: "Fb", Path: "lib/lib.go", Root: "workspace"}, Text: "package lib\nfunc Target(){}\n"},
	}
	g := knowledgeGraph{Nodes: []knowledgeNode{files[0].Node, files[1].Node}}
	expandKnowledgeCode(context.Background(), &g, files)
	if g.Code == nil || g.Code.Engine != "ast-name-resolution" || g.Code.Calls != 2 || g.Code.Unresolved != 3 || g.Code.UnresolvedImports != 1 {
		t.Fatalf("summary %#v", g.Code)
	}
	counts := map[string]int{}
	for _, e := range g.Edges {
		counts[e.Confidence]++
		if e.Kind == "call_candidate" && e.Confidence != "name_candidate" {
			t.Fatal("candidate promoted", e)
		}
	}
	if counts["source_declaration"] != 3 || counts["source_path"] != 1 || counts["name_candidate"] != 2 {
		t.Fatal("categories", counts)
	}
	if len(g.Code.UnresolvedRelations) != 4 {
		t.Fatal("missing unresolved records", g.Code.UnresolvedRelations)
	}
	for _, r := range g.Code.UnresolvedRelations {
		if r.File != "Fa" || r.From == "" || r.Reason == "" || r.Kind == "call" && r.Line != 4 {
			t.Fatal("missing provenance", r)
		}
	}
	raw, err := json.Marshal(g)
	if err != nil || !strings.Contains(string(raw), "outside_index_or_unknown_binding") {
		t.Fatal("wire evidence", err)
	}
	if len(g.Nodes) != 5 {
		t.Fatal("unresolved calls created fake nodes", len(g.Nodes))
	}
}

func TestKnowledgeCodeUnresolvedEvidenceBound(t *testing.T) {
	text := "package main\nfunc entry(){\n" + strings.Repeat("unknown()\n", 1100) + "}\n"
	f := knowledgeCodeFile{ID: "F", Node: knowledgeNode{ID: "F", Path: "main.go", Root: "workspace"}, Text: text}
	g := knowledgeGraph{Nodes: []knowledgeNode{f.Node}}
	expandKnowledgeCode(context.Background(), &g, []knowledgeCodeFile{f})
	if g.Code.Unresolved != 1100 || len(g.Code.UnresolvedRelations) != 1000 || !g.Code.RelationsTruncated {
		t.Fatalf("bound %#v", g.Code)
	}
}
