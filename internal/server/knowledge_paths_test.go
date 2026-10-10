package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKnowledgePathAPIRevisionIsolation(t *testing.T) {
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	s := &a.knowledgeUpdates
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	s.workspace, s.epoch = workspace, epoch
	s.views[0] = &knowledgeUpdateView{revision: "r", graph: knowledgeGraph{Workspace: workspace, Nodes: []knowledgeNode{{ID: "a"}, {ID: "b"}}, Edges: []knowledgeEdge{{From: "a", To: "b", Kind: "mention"}}}}
	q := knowledgePathRequest{Workspace: workspace, Revision: "r", From: "a", To: "b"}
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 200)
	q.Revision = "stale"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Revision = "r"
	q.Code = true
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Code = false
	q.Workspace = "other"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Workspace = workspace
	s.epoch++
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	s.epoch = epoch
	s.views[0].failed = true
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	s.views[0].failed = false
	a.mu.Lock()
	a.settings.UserPasswordHash = "configured"
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 423)
	a.mu.Lock()
	a.settings.UserPasswordHash = ""
	a.mu.Unlock()
	body, _ := json.Marshal(q)
	w := httptest.NewRecorder()
	a.knowledgePaths(w, httptest.NewRequest("POST", "/api/knowledge-map/paths", strings.NewReader(string(body)+"{}")))
	requireStatus(t, w, 400)
}

func TestKnowledgePathEvidence(t *testing.T) {
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "folder"}}, Edges: []knowledgeEdge{
		{From: "folder", To: "a", Kind: "contains"}, {From: "folder", To: "c", Kind: "contains"},
		{From: "a", To: "b", Kind: "call_typed", Line: 7, Column: 4, Evidence: "go/types", Confidence: "type_binding"},
		{From: "b", To: "c", Kind: "call_candidate", Evidence: "AST name", Confidence: "name_candidate"},
		{From: "c", To: "a", Kind: "mention"}, {From: "a", To: "missing", Kind: "mention"},
	}}
	q := knowledgePathRequest{From: "a", To: "c", Revision: "r"}
	r, err := findKnowledgePath(context.Background(), g, q)
	if err != nil || !r.Found || len(r.Steps) != 2 || r.Steps[0].Edge.Line != 7 || r.Steps[1].Edge.Confidence != "name_candidate" {
		t.Fatalf("%+v %v", r, err)
	}
	q.Undirected = true
	r, err = findKnowledgePath(context.Background(), g, q)
	if err != nil || len(r.Steps) != 1 || !r.Steps[0].Reversed || r.Steps[0].Edge.From != "c" {
		t.Fatalf("%+v %v", r, err)
	}
	q.From = "folder"
	r, err = findKnowledgePath(context.Background(), g, q)
	if err != nil || r.Found {
		t.Fatalf("containment became evidence: %+v %v", r, err)
	}
	q.IncludeStructure = true
	r, err = findKnowledgePath(context.Background(), g, q)
	if err != nil || !r.Found || len(r.Steps) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	q.To = "missing"
	if _, err = findKnowledgePath(context.Background(), g, q); err == nil {
		t.Fatal("unknown node accepted")
	}
}

func TestKnowledgePathLimitsAndCancellation(t *testing.T) {
	g := knowledgeGraph{Nodes: []knowledgeNode{{ID: "a"}, {ID: "b"}}, Edges: []knowledgeEdge{{From: "a", To: "b", Kind: "mention"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := findKnowledgePath(ctx, g, knowledgePathRequest{From: "a", To: "b"}); err == nil {
		t.Fatal("cancel ignored")
	}
	g.Truncated = true
	g.Warnings = []string{"partial index"}
	r, err := findKnowledgePath(context.Background(), g, knowledgePathRequest{From: "a", To: "a"})
	if err != nil || !r.Found || len(r.Nodes) != 1 || len(r.Steps) != 0 || !r.Truncated || len(r.Warnings) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	chain := knowledgeGraph{}
	for i := 0; i < 35; i++ {
		id := fmt.Sprint(i)
		chain.Nodes = append(chain.Nodes, knowledgeNode{ID: id})
		if i > 0 {
			chain.Edges = append(chain.Edges, knowledgeEdge{From: fmt.Sprint(i - 1), To: id, Kind: "mention"})
		}
	}
	r, err = findKnowledgePath(context.Background(), chain, knowledgePathRequest{From: "0", To: "34"})
	if err != nil || r.Found || !r.Limited {
		t.Fatalf("unbounded trail: %+v %v", r, err)
	}
}
