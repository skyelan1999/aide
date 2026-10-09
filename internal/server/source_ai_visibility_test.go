package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSourceAIVisibilityWorkspaceScope(t *testing.T) {
	a := sourceActionFixture(t)
	scope := a.wsID()
	set := func(visible bool) {
		requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": "readonly-actions", "workspaceId": scope, "visible": visible}), 200)
	}
	set(false)
	src, _ := a.findSource("readonly-actions")
	if sourceAIVisible(src, scope) || !sourceAIVisible(src, "other-workspace") {
		t.Fatal("visibility scope")
	}
	b, err := os.ReadFile(a.sourcesPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved sourcesRegistry
	if json.Unmarshal(b, &saved) != nil {
		t.Fatal("invalid registry")
	}
	if err := a.loadSources(); err != nil {
		t.Fatal(err)
	}
	if a.sourceAIAllowed(src.ID) {
		t.Fatal("visibility lost after reload")
	}
	if !strings.Contains(string(b), "aiHiddenWorkspaces") {
		t.Fatal("setting not persisted")
	}
	// Manual viewing still works; AI reads, directory attachments and searches do not.
	requireStatus(t, request(a, "GET", "/api/file?source=readonly-actions&path=nested/file%20name.md", nil), 200)
	for _, dir := range []bool{false, true} {
		p := "nested/file name.md"
		if dir {
			p = "nested"
		}
		if _, _, _, err := a.attachmentContext([]Attachment{{Root: "source", Source: src.ID, Path: p, Directory: dir}}); err == nil {
			t.Fatal("hidden attachment accepted")
		}
	}
	for _, name := range []string{"read_file", "list_files", "semantic_search", "mcp_call"} {
		got := a.executeToolCall(context.Background(), readCall(name, `{"source":"readonly-actions","path":"nested/file name.md","query":"reference"}`), &Task{}, nil)
		if !strings.Contains(got, "disabled") {
			t.Fatalf("%s bypass: %s", name, got)
		}
	}
	got := a.executeToolCall(context.Background(), readCall("list_sources", `{}`), &Task{}, nil)
	if strings.Contains(got, src.ID) {
		t.Fatal("hidden source advertised")
	}
	g := knowledgeGraph{Nodes: []knowledgeNode{{ID: "hidden", Source: src.ID}, {ID: "visible", Root: "workspace"}}, Edges: []knowledgeEdge{{From: "hidden", To: "visible"}}}
	filtered := a.knowledgeForAI(g)
	if len(filtered.Nodes) != 1 || len(filtered.Edges) != 0 || len(g.Nodes) != 2 {
		t.Fatal("AI graph filtering")
	}
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": src.ID, "workspaceId": "stale", "visible": true}), 409)
	set(true)
	if _, _, _, err := a.attachmentContext([]Attachment{{Root: "source", Source: src.ID, Path: "nested/file name.md"}}); err != nil {
		t.Fatal(err)
	}
}
func TestBuiltinSourceAIVisibilitySurvivesSourceEdit(t *testing.T) {
	a := testApp(t)
	scope := a.wsID()
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": systemDocsSource, "workspaceId": scope, "visible": false}), 200)
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{}}), 200)
	if a.sourceAIAllowed(systemDocsSource) {
		t.Fatal("built-in setting lost")
	}
	// Existing sources.json has no hidden-workspace field and remains AI-readable.
	if !a.sourceAIAllowed(contextSource) {
		t.Fatal("legacy default changed")
	}
}
