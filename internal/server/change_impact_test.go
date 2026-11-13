package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func impactFixtureGraph() knowledgeGraph {
	return knowledgeGraph{Workspace: "fixture", Nodes: []knowledgeNode{
		{ID: "spec", Kind: "file", Root: "workspace", Path: "spec.md", Text: "pressure v1"},
		{ID: "calc", Kind: "file", Root: "workspace", Path: "calc.go"},
		{ID: "symbol", Kind: "function", ParentFile: "calc"},
		{ID: "test", Kind: "file", Root: "workspace", Path: "calc_test.go"},
		{ID: "drawing", Kind: "file", Root: "workspace", Path: "drawing.step"},
		{ID: "other", Kind: "file", Root: "workspace", Path: "other.md"},
	}, Edges: []knowledgeEdge{{From: "calc", To: "spec", Kind: "mention", Evidence: "spec.md", Line: 3}, {From: "test", To: "symbol", Kind: "call", Evidence: "Calculate()", Line: 8}, {From: "drawing", To: "test", Kind: "mention"}, {From: "spec", To: "other", Kind: "contains"}}}
}
func TestChangeImpactRelationsBoundsAndUncertainty(t *testing.T) {
	g := impactFixtureGraph()
	v, err := analyzeChangeImpact(context.Background(), g, "ws", "Pressure changed", []string{"spec.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 4 || len(v.Warnings) == 0 {
		t.Fatalf("candidates: %+v", v)
	}
	for _, row := range v.Items {
		if row.Path == "other.md" {
			t.Fatal("containment treated as impact")
		}
		if row.Path == "spec.md" && row.Classification != "must_change" {
			t.Fatal("seed")
		}
		if row.Path == "calc_test.go" && (row.Classification != "review" || len(row.Evidence) != 2 || row.Evidence[1].From != "test") {
			t.Fatal("lost direction/trail", row)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := analyzeChangeImpact(ctx, g, "ws", "Changed", []string{"spec.md"}); err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, p := range []string{"../secret", "/absolute", "missing.md"} {
		if _, err := analyzeChangeImpact(context.Background(), g, "ws", "Changed", []string{p}); err == nil {
			t.Fatal("invalid seed accepted", p)
		}
	}
	for n := 0; n < 100; n++ {
		id := fmt.Sprint(n)
		g.Nodes = append(g.Nodes, knowledgeNode{ID: id, Kind: "file", Root: "workspace", Path: id + ".md"})
		g.Edges = append(g.Edges, knowledgeEdge{From: id, To: "spec", Kind: "mention"})
	}
	v, err = analyzeChangeImpact(context.Background(), g, "ws", "Changed", []string{"spec.md"})
	if err != nil || len(v.Items) != 80 || !v.Limited {
		t.Fatal("unbounded analysis", len(v.Items), err)
	}
}
func TestChangeImpactRevisionPreservesGraphAndIgnoresChatter(t *testing.T) {
	g := impactFixtureGraph()
	before, _ := json.Marshal(g)
	rev := impactGraphRevision(g)
	after, _ := json.Marshal(g)
	if string(before) != string(after) {
		t.Fatal("mutated shared graph")
	}
	g.Nodes = append(g.Nodes, knowledgeNode{ID: "session", Kind: "session", Text: "new conversation"})
	if impactGraphRevision(g) != rev {
		t.Fatal("session chatter invalidates file analysis")
	}
	g.Nodes[0].Text = "pressure v2"
	if impactGraphRevision(g) == rev {
		t.Fatal("file revision ignored")
	}
}
func impactHTTPView(t *testing.T, a *App) map[string]any {
	t.Helper()
	w := request(a, "GET", "/api/sessions/impact-session/impacts", nil)
	requireStatus(t, w, 200)
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestChangeImpactAPIPersistenceCASAndFreshness(t *testing.T) {
	a := testApp(t)
	s := planSession(a, "impact-session", "Impact fixture")
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), "spec.md"), []byte("# Specification v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := impactHTTPView(t, a)
	payload := map[string]any{"action": "analyze", "workspaceId": v["workspaceId"], "snapshot": v["snapshot"], "change": "Pressure changed", "paths": []string{"spec.md"}}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(a, "POST", "/api/sessions/impact-session/impacts", payload).Code
		}()
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for c := range codes {
		if c == 200 {
			ok++
		}
		if c == 409 {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("CAS", ok, conflict)
	}
	v = impactHTTPView(t, a)
	analysis := s.Impacts[0]
	item := analysis.Items[0]
	review := map[string]any{"action": "review", "workspaceId": v["workspaceId"], "snapshot": v["snapshot"], "id": analysis.ID, "itemId": item.ID, "classification": "unaffected", "state": "done", "note": "Manually inspected source; unrelated to pressure"}
	requireStatus(t, request(a, "POST", "/api/sessions/impact-session/impacts", review), 200)
	v = impactHTTPView(t, a)
	review["snapshot"] = v["snapshot"]
	review["note"] = "Rechecked actual source"
	requireStatus(t, request(a, "POST", "/api/sessions/impact-session/impacts", review), 200)
	if len(s.Impacts[0].Items[0].History) != 1 {
		t.Fatal("prior review lost")
	}
	r := &Task{ID: "impact-read", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision}
	s.Runs = append(s.Runs, r)
	var summary map[string]any
	if err := json.Unmarshal([]byte(a.taskChangeImpact(r, map[string]any{})), &summary); err != nil {
		t.Fatal(err)
	}
	rows := summary["analyses"].([]any)
	record := rows[0].(map[string]any)
	text := a.taskChangeImpact(r, map[string]any{"analysisId": record["id"], "snapshot": record["snapshot"]})
	if !strings.Contains(text, "Rechecked actual source") {
		t.Fatal("model could not read review", text)
	}
	r.WorkspaceID = "foreign"
	if strings.Contains(a.taskChangeImpact(r, map[string]any{}), "Pressure changed") {
		t.Fatal("foreign impact leaked")
	}
	b, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.sessions[s.ID].Impacts[0].Items[0].Note != review["note"] {
		t.Fatal("review not durable")
	}
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), "spec.md"), []byte("# Specification v2 - changed content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v = impactHTTPView(t, a)
	review["snapshot"] = v["snapshot"]
	requireStatus(t, request(a, "POST", "/api/sessions/impact-session/impacts", review), 409)
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/sessions/impact-session/impacts", nil), 423)
}
