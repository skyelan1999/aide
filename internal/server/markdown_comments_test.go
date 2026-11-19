package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMarkdownCommentsBindFileVersionAndSource(t *testing.T) {
	a := testApp(t)
	source := []byte("# Review\r\n\r\nSame phrase and a second Same phrase.\r\n")
	for _, file := range []string{"review.md", "other.md"} {
		if err := a.workspace.WriteFile(file, source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.reference.WriteFile("review.md", source, 0600); err != nil {
		t.Fatal(err)
	}
	route := "/api/file/markdown-comments"
	target := "?root=workspace&path=review.md"
	data := map[string]any{"hash": strings.Repeat("0", 64), "anchorQuote": "Same phrase", "anchorIndex": 1, "text": "Review the second match"}
	requireStatus(t, request(a, "POST", route+target, data), http.StatusConflict)
	data["hash"] = hash(source)
	created := request(a, "POST", route+target, data)
	requireStatus(t, created, http.StatusCreated)
	var first Comment
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil || first.DocHash != hash(source) || first.AnchorIndex != 1 {
		t.Fatalf("unexpected comment %+v, %v", first, err)
	}
	for _, target := range []string{
		route + "?root=context&path=review.md",
		route + "?root=workspace&path=other.md",
		"/api/comments?path=review.md",
	} {
		got := request(a, "GET", target, nil)
		requireStatus(t, got, http.StatusOK)
		if strings.Contains(got.Body.String(), first.ID) {
			t.Fatal("comment leaked across documents or sources")
		}
	}
	requireStatus(t, request(a, "PUT", route+"/"+first.ID+"?path=other.md", map[string]any{"status": "resolved"}), http.StatusNotFound)
	resolved := request(a, "PUT", route+"/"+first.ID+target, map[string]any{"status": "resolved"})
	requireStatus(t, resolved, http.StatusOK)
	if !strings.Contains(resolved.Body.String(), `"status":"resolved"`) {
		t.Fatal(resolved.Body.String())
	}
	if err := a.workspace.WriteFile("review.md", []byte("# Revised"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := request(a, "GET", route+target, nil)
	requireStatus(t, stale, http.StatusOK)
	if !strings.Contains(stale.Body.String(), `"stale":true`) || !strings.Contains(stale.Body.String(), first.ID) {
		t.Fatal(stale.Body.String())
	}
	requireStatus(t, request(a, "DELETE", route+"/"+first.ID+target, nil), http.StatusOK)
	var list struct{ Comments []Comment }
	w := request(a, "GET", route+target, nil)
	requireStatus(t, w, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Comments) != 0 {
		t.Fatalf("comment not removed: %+v %v", list, err)
	}
	saved, err := a.workspace.ReadFile("review.md")
	if err != nil || string(saved) != "# Revised" {
		t.Fatal("sidecar changes must preserve source Markdown", err)
	}
}
