package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func referenceNode(id, path, text string) knowledgeNode {
	return knowledgeNode{ID: id, Kind: "file", Root: "workspace", Region: "workspace", Path: path, Text: text}
}

func TestKnowledgeReferenceSpacedReportSourcePath(t *testing.T) {
	g := knowledgeGraph{Nodes: []knowledgeNode{
		referenceNode("report", "reports/summary.md", "依据 [证据](<../evidence/分析 记录.md#原文>)"),
		referenceNode("evidence", "evidence/分析 记录.md", "引用 [代码](../src/source.go)"),
		referenceNode("source", "src/source.go", "package main"),
	}}
	appendKnowledgeMentions(&g)
	result, err := findKnowledgePath(context.Background(), g, knowledgePathRequest{From: "report", To: "source"})
	if err != nil || !result.Found || len(result.Steps) != 2 {
		t.Fatalf("explicit report/source path missing: %+v %v", result, err)
	}
	for _, step := range result.Steps {
		if step.Edge.Confidence != "explicit_reference" || step.Edge.Line != 1 || step.Edge.Column == 0 {
			t.Fatalf("lost source evidence: %+v", step)
		}
	}
}

func TestKnowledgeReferenceLocatorUsesMatchedOccurrence(t *testing.T) {
	text := "旧记录 ../src/source.go.bak\n\n引用 [源文件](../src/source.go)\n"
	g := knowledgeGraph{Nodes: []knowledgeNode{referenceNode("report", "reports/r.md", text), referenceNode("source", "src/source.go", "")}}
	appendKnowledgeMentions(&g)
	if len(g.Edges) != 1 || g.Edges[0].Line != 3 {
		t.Fatalf("locator points to an unrelated earlier substring: %+v", g.Edges)
	}
	prefix := "引用 [源文件]("
	if g.Edges[0].Column != len([]rune(prefix))+1 || !strings.Contains(g.Edges[0].Evidence, "../src/source.go") {
		t.Fatal(g.Edges[0])
	}
}

func TestKnowledgeReferenceNamespacedSuffixIsNotReference(t *testing.T) {
	g := knowledgeGraph{Nodes: []knowledgeNode{referenceNode("report", "report.md", "source-a:lib/file.md.bak https://example.test/source-a:lib/file.md"), {ID: "foreign", Kind: "file", Root: "source", Source: "source-a", Region: "source:source-a", Path: "lib/file.md"}}}
	appendKnowledgeMentions(&g)
	if len(g.Edges) != 0 {
		t.Fatalf("filename suffix or external URL invented a reference: %+v", g.Edges)
	}
}

func TestKnowledgeReferenceEncodingsAndAreaBoundaries(t *testing.T) {
	for _, token := range []string{"[正文](分析%20记录.md#L7)", "[正文](<分析 记录.md> \"标题\")", "`分析 记录.md`", "\"分析 记录.md\""} {
		g := knowledgeGraph{Nodes: []knowledgeNode{referenceNode("r", "r.md", token), referenceNode("local", "分析 记录.md", ""), {ID: "foreign", Kind: "file", Root: "source", Source: "other", Region: "source:other", Path: "分析 记录.md"}}}
		appendKnowledgeMentions(&g)
		if len(g.Edges) != 1 || g.Edges[0].To != "local" {
			t.Fatalf("token %q crossed identity or lost target: %+v", token, g.Edges)
		}
	}
	for _, text := range []string{"正文[foreign]结尾", "`other:分析 记录.md`", "参考 “other:分析 记录.md”"} {
		g := knowledgeGraph{Nodes: []knowledgeNode{referenceNode("r", "r.md", text), {ID: "foreign", Kind: "file", Root: "source", Source: "other", Region: "source:other", Path: "分析 记录.md"}}}
		appendKnowledgeMentions(&g)
		if len(g.Edges) != 1 || g.Edges[0].To != "foreign" {
			t.Fatalf("explicit identity lost: %q %+v", text, g.Edges)
		}
	}
	for _, token := range []string{"[正文](https://example.test/分析%20记录.md)", "[正文](file:///分析%20记录.md)", "`/分析 记录.md`", "[正文](分析%0020记录.md)"} {
		g := knowledgeGraph{Nodes: []knowledgeNode{referenceNode("r", "r.md", token), referenceNode("local", "分析 记录.md", "")}}
		appendKnowledgeMentions(&g)
		if len(g.Edges) != 0 {
			t.Fatalf("invalid/foreign URL linked locally: %q %+v", token, g.Edges)
		}
	}
}

func TestKnowledgeReferenceExtractionBounds(t *testing.T) {
	refs := knowledgeTextReferences(strings.Repeat("`file.md` ", 220))
	if len(refs) != 200 {
		t.Fatal(len(refs))
	}
	if len(knowledgeTextReferences("`"+strings.Repeat("x", 1025)+"`")) != 0 {
		t.Fatal("oversized target retained")
	}
}

func TestKnowledgeReferenceIndexedReportPathAPI(t *testing.T) {
	a := testApp(t)
	files := map[string]string{
		"reports/summary.md": "# 报告\n旧记录 ../src/source.go.bak\n依据 [证据](<../evidence/分析 记录.md#原文>)\n",
		"evidence/分析 记录.md":  "# 证据\n引用 [代码](../src/source.go)\n",
		"src/source.go":      "package source\n",
	}
	for name, text := range files {
		filename := filepath.Join(a.workPath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
	ids := map[string]string{}
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Kind == "file" {
			ids[n.Path] = n.ID
		}
	}
	for name := range files {
		if ids[name] == "" {
			t.Fatalf("index omitted %q", name)
		}
	}
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	s := &a.knowledgeUpdates
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	s.workspace, s.epoch = workspace, epoch
	s.views[0] = &knowledgeUpdateView{revision: "explicit-fixture", graph: g}
	q := knowledgePathRequest{Workspace: workspace, Revision: "explicit-fixture", From: ids["reports/summary.md"], To: ids["src/source.go"]}
	w := request(a, "POST", "/api/knowledge-map/paths", q)
	requireStatus(t, w, 200)
	var result knowledgePathResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Found || len(result.Steps) != 2 || len(result.Nodes) != 3 || result.Workspace != workspace || result.Revision != q.Revision {
		t.Fatalf("indexed report path/identity lost: %+v", result)
	}
	for i, line := range []int{3, 2} {
		edge := result.Steps[i].Edge
		if edge.Kind != "mention" || edge.Confidence != "explicit_reference" || edge.Line != line || edge.Column < 1 {
			t.Fatalf("invalid evidence at step %d: %+v", i, edge)
		}
	}
	if result.Nodes[1].ID != ids["evidence/分析 记录.md"] {
		t.Fatal("path bypassed actual evidence file")
	}
	q.Revision = "stale"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Revision = "explicit-fixture"
	q.Workspace = "other-workspace"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
}
