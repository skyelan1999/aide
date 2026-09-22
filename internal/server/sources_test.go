package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceBody(id, name, typ string, cfg map[string]any, rw bool) map[string]any {
	return map[string]any{"id": id, "name": name, "type": typ, "enabled": true, "rw": rw, "config": cfg}
}

func TestSourcesRegistryAndBrowsing(t *testing.T) {
	a := testApp(t)
	dir := filepath.Join(a.workPath, "refs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("reference-content"), 0644); err != nil {
		t.Fatal(err)
	}
	// 添加本地来源（相对路径 → workPath）
	w := request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("ref1", "参考资料一", "local", map[string]any{"path": "refs"}, true)},
	})
	requireStatus(t, w, 200)
	// 列表
	w = request(a, "GET", "/api/sources", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"ref1"`) {
		t.Fatalf("sources list: %s", w.Body.String())
	}
	// 浏览来源目录
	w = request(a, "GET", "/api/files?source=ref1&path=.", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"note.md"`) {
		t.Fatalf("source listing: %s", w.Body.String())
	}
	// 读取
	w = request(a, "GET", "/api/file?source=ref1&path=note.md", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "reference-content") {
		t.Fatalf("source read: %s", w.Body.String())
	}
	// RW 来源编辑器保存
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": "ref1", "path": "new.md", "content": "written"}), 200)
	if b, err := os.ReadFile(filepath.Join(dir, "new.md")); err != nil || string(b) != "written" {
		t.Fatalf("source write: %s %v", b, err)
	}
	// 只读来源拒绝写入
	w = request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("ref1", "参考资料一", "local", map[string]any{"path": "refs"}, false)},
	})
	requireStatus(t, w, 200)
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": "ref1", "path": "x.md", "content": "no"}), 403)
	// 停用来源不可浏览
	w = request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("ref1", "参考资料一", "local", map[string]any{"path": "refs"}, false), map[string]any{"id": "off", "name": "停用", "type": "local", "enabled": false, "config": map[string]any{"path": "refs"}}},
	})
	requireStatus(t, w, 200)
	requireStatus(t, request(a, "GET", "/api/files?source=off&path=.", nil), 400)
}

func TestSystemDocsAutoMount(t *testing.T) {
	a := testApp(t)
	// 设置工作空间配置的文档路径 → 自动挂载为读写来源
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"docs": map[string]any{"path": "docs-dir"}}), 200)
	w := request(a, "GET", "/api/sources", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"system-docs"`) || !strings.Contains(w.Body.String(), `"rw":true`) || !strings.Contains(w.Body.String(), `"builtin":true`) {
		t.Fatalf("system docs source: %s", w.Body.String())
	}
	// 客户端提交不包含系统来源时自动补回
	w = request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("ref2", "其他", "local", map[string]any{"path": "refs"}, false)}})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"system-docs"`) {
		t.Fatalf("system-docs must be re-added: %s", w.Body.String())
	}
}

func TestCurlSourceWithMock(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	stub := filepath.Join(dir, "curl")
	os.WriteFile(stub, []byte(`#!/bin/sh
url=""
for arg in "$@"; do
  case "$arg" in
    http*|ftp*|smb*) url="$arg" ;;
  esac
done
case "$url" in
  */)
    echo 'sub/'
    echo 'file.txt'
    ;;
  *)
    echo 'curl-source-content'
    ;;
esac
exit 0
`), 0700)
	a.curlBin = stub
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("ftp1", "FTP 站点", "ftp", map[string]any{"url": "ftp://example.com/pub", "username": "u"}, false)},
		"secrets": map[string]any{"ftp1": map[string]any{"password": "pw"}},
	}), 200)
	w := request(a, "GET", "/api/files?source=ftp1&path=.", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"sub"`) || !strings.Contains(w.Body.String(), `"name":"file.txt"`) {
		t.Fatalf("ftp listing: %s", w.Body.String())
	}
	w = request(a, "GET", "/api/file?source=ftp1&path=file.txt", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "curl-source-content") {
		t.Fatalf("ftp read: %s", w.Body.String())
	}
	// MCP：诚实错误
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("mcp1", "MCP 服务器", "mcp", map[string]any{"command": "npx -y @modelcontextprotocol/server-filesystem"}, false)},
	}), 200)
	w = request(a, "GET", "/api/files?source=mcp1&path=.", nil)
	requireStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "待实现") {
		t.Fatalf("mcp honest error: %s", w.Body.String())
	}
}
