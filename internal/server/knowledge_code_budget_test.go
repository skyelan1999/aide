package server

import (
	"context"
	"fmt"
	"testing"
)

func TestKnowledgeCodeBudgetExcludesCatalogueEdges(t *testing.T) {
	files := []knowledgeCodeFile{
		{ID: "H", Text: "<button id='save'>Save</button>\n<script src='./app.js'></script>", Node: knowledgeNode{ID: "H", Kind: "file", Root: "workspace", Path: "index.html"}},
		{ID: "J", Text: "async function save(){return fetch('/api/save',{method:'POST'});}\ndocument.getElementById('save').addEventListener('click',save);", Node: knowledgeNode{ID: "J", Kind: "file", Root: "workspace", Path: "app.js"}},
		{ID: "G", Text: knowledgeWebServerFixture, Node: knowledgeNode{ID: "G", Kind: "file", Root: "workspace", Path: "server.go"}},
	}
	g := knowledgeGraph{Nodes: []knowledgeNode{files[0].Node, files[1].Node, files[2].Node}}
	for i := 0; i < 20000; i++ {
		id := fmt.Sprintf("D%d", i)
		g.Nodes = append(g.Nodes, knowledgeNode{ID: id, Kind: "directory"})
		g.Edges = append(g.Edges, knowledgeEdge{From: "root", To: id, Kind: "contains"})
	}
	expandKnowledgeCode(context.Background(), &g, files)
	from, to := "", ""
	for _, n := range g.Nodes {
		if n.Name == "button#save" {
			from = n.ID
		}
		if n.Name == "service" {
			to = n.ID
		}
	}
	result, err := findKnowledgePath(context.Background(), g, knowledgePathRequest{From: from, To: to})
	if err != nil || !result.Found || len(result.Steps) != 6 {
		t.Fatalf("catalogue edges consumed code relation budget: found=%v steps=%d err=%v", result.Found, len(result.Steps), err)
	}
	if g.Truncated {
		t.Fatal("unrelated catalogue falsely exhausted code budget")
	}
	// The code budget remains finite even with unrelated catalogue edges.
	for i := knowledgeCodeEdgeCount(g.Edges); i < knowledgeCodeEdgeLimit; i++ {
		g.Edges = append(g.Edges, knowledgeEdge{From: "H", To: fmt.Sprintf("reserved%d", i), Kind: "defines"})
	}
	before := len(g.Edges)
	expandKnowledgeCode(context.Background(), &g, files)
	if len(g.Edges) != before || !g.Truncated {
		t.Fatal("exhausted code budget admitted more relations")
	}
}
