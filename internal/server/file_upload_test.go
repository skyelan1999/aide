package server

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uploadRequest(a *App, target string, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestFileUploadAndSafeHiddenListing(t *testing.T) {
	a := testApp(t)
	// 工作目录上传用与普通写入相同的相对路径约束，且不会静默覆盖。
	w := uploadRequest(a, "/api/file/upload?path=drop/note.bin", []byte{0, 1, 2})
	requireStatus(t, w, 201)
	if b, err := os.ReadFile(filepath.Join(a.workPath, "drop", "note.bin")); err != nil || !bytes.Equal(b, []byte{0, 1, 2}) {
		t.Fatalf("workspace upload: %q %v", b, err)
	}
	requireStatus(t, uploadRequest(a, "/api/file/upload?path=drop/note.bin", []byte("replacement")), 409)
	requireStatus(t, uploadRequest(a, "/api/file/upload?path=.git/config", []byte("blocked")), 400)

	// 可写来源可接收上传，只读来源仍由服务端拒绝，不能仅依赖前端禁用按钮。
	refs := filepath.Join(a.workPath, "refs")
	if err := os.MkdirAll(refs, 0755); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("rw", "RW", "local", map[string]any{"path": "refs"}, true)},
	}), 200)
	requireStatus(t, uploadRequest(a, "/api/file/upload?source=rw&path=from-drop.txt", []byte("source-data")), 201)
	if b, err := os.ReadFile(filepath.Join(refs, "from-drop.txt")); err != nil || string(b) != "source-data" {
		t.Fatalf("source upload: %q %v", b, err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("rw", "RW", "local", map[string]any{"path": "refs"}, false)},
	}), 200)
	requireStatus(t, uploadRequest(a, "/api/file/upload?source=rw&path=blocked.txt", []byte("blocked")), 403)

	// 安全的隐藏目录默认显示，但受保护路径不会出现在列表中。
	if err := os.MkdirAll(filepath.Join(a.workPath, ".cache", "visible"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.workPath, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	w = request(a, "GET", "/api/files?root=workspace&path=.", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":".cache"`) || strings.Contains(w.Body.String(), `"name":".git"`) {
		t.Fatalf("hidden listing safety: %s", w.Body.String())
	}
}

func TestFileSearchSupportsRecursiveFuzzyAndExactMatching(t *testing.T) {
	a := testApp(t)
	for rel, content := range map[string]string{
		"notes/target-REQ-001.md":         "root",
		"notes/archive/target-REQ-001.md": "nested",
		"notes/archive/target-REQ-002.md": "other",
	} {
		full := filepath.Join(a.workPath, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 当前目录的模糊查询不进入 archive；递归查询则返回两个同名结果。
	w := request(a, "GET", "/api/files?root=workspace&path=notes&search=req&scope=folder&match=fuzzy", nil)
	requireStatus(t, w, 200)
	if strings.Count(w.Body.String(), `"name":"target-REQ-001.md"`) != 1 || strings.Contains(w.Body.String(), "REQ-002") {
		t.Fatalf("folder fuzzy search: %s", w.Body.String())
	}
	w = request(a, "GET", "/api/files?root=workspace&path=notes&search=target-REQ-001.md&scope=recursive&match=exact", nil)
	requireStatus(t, w, 200)
	if strings.Count(w.Body.String(), `"name":"target-REQ-001.md"`) != 2 || strings.Contains(w.Body.String(), "REQ-002") {
		t.Fatalf("recursive exact search: %s", w.Body.String())
	}
	requireStatus(t, request(a, "GET", "/api/files?root=workspace&path=notes&search=x&scope=unknown", nil), 400)
}
