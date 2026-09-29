package server

import (
	"encoding/json"
	"fmt"
	"testing"
)

// 递归搜索命中结果上限时，必须通过响应头 X-Search-Truncated 上报，而不是静默截断。
// body 仍为裸 JSON 数组（不破坏现有消费方）。
func TestFileSearchTruncationHeaderOnResultCap(t *testing.T) {
	a := testApp(t)
	// 工作区根目录直接放 600 个匹配文件，远超 maxFileSearchResults=500。
	const n = maxFileSearchResults + 100
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("match_%03d.txt", i)
		if err := a.workspace.WriteFile(name, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a, "GET", "/api/files?root=workspace&path=.&search=match&scope=recursive", nil)
	requireStatus(t, w, 200)

	if got := w.Result().Header.Get("X-Search-Truncated"); got != "1" {
		t.Fatalf("X-Search-Truncated = %q want \"1\"", got)
	}
	if got := w.Result().Header.Get("X-Search-Dir-Limit"); got != "200" {
		t.Fatalf("X-Search-Dir-Limit = %q want 200", got)
	}
	if got := w.Result().Header.Get("X-Search-Result-Limit"); got != "500" {
		t.Fatalf("X-Search-Result-Limit = %q want 500", got)
	}

	var items []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("body must remain a bare JSON array: %v", err)
	}
	if len(items) != maxFileSearchResults {
		t.Fatalf("returned rows = %d want exactly %d (cap, not %d)", len(items), maxFileSearchResults, n)
	}
}

// 未命中任何上限的普通搜索不应携带截断头。
func TestFileSearchNoHeaderWhenUnderCap(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("match_%d.txt", i)
		if err := a.workspace.WriteFile(name, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a, "GET", "/api/files?root=workspace&path=.&search=match", nil)
	requireStatus(t, w, 200)

	if got := w.Result().Header.Get("X-Search-Truncated"); got != "" {
		t.Fatalf("unexpected X-Search-Truncated = %q for an under-cap search", got)
	}
	var items []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("rows = %d want 3", len(items))
	}
}
