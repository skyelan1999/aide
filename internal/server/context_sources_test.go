package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #53 根因回归：内置 /context（AIDE_CONTEXT）常驻，不被自定义工作区的空 doc/ 替换或 Close。
// 复现场景：用户切到含空 doc/ 的自定义工程目录，Docs.Path 指向该空目录。
func TestContextSourcePersistentAcrossWorkspaceSwitch(t *testing.T) {
	a := testApp(t)

	// 1) 在真实 /context 放 13 项
	const wantCtx = 13
	for i := 0; i < wantCtx; i++ {
		if err := os.WriteFile(filepath.Join(a.refPath, string(rune('a'+i))+".md"), []byte("ctx"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 2) 自定义工作区 custom-ws，其下含一个空 doc/
	customWS := filepath.Join(a.workPath, "custom-ws")
	if err := os.MkdirAll(filepath.Join(customWS, "doc"), 0755); err != nil {
		t.Fatal(err)
	}

	beforeRef := a.reference.Name()

	// 3) 切到自定义工作区，Docs.Path 叠加指向空 doc/
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "local", "path": "custom-ws"},
		"docs":      map[string]any{"path": "custom-ws/doc"},
	}), 200)

	// a.reference 未被替换（仍指向原 /context）
	if a.reference.Name() != beforeRef {
		t.Fatalf("a.reference 被替换: before=%s after=%s", beforeRef, a.reference.Name())
	}

	// 4) 来源列表：内置 /context 与 system-docs 叠加共存
	body := request(a, "GET", "/api/sources", nil).Body.String()
	if !strings.Contains(body, `"id":"context"`) || !strings.Contains(body, `"builtin":true`) {
		t.Fatalf("内置 /context 来源缺失: %s", body)
	}
	if !strings.Contains(body, `"id":"system-docs"`) {
		t.Fatalf("system-docs 叠加来源缺失: %s", body)
	}

	// 5) source=context 仍列出 13 项（未被空 doc 顶替）
	a.mu.Lock()
	ctxSrc, ok := a.findSource(contextSource)
	a.mu.Unlock()
	if !ok || !ctxSrc.Enabled {
		t.Fatal("context 内置来源不可用")
	}
	entries, err := a.listSourceDir(ctxSrc, ".")
	if err != nil {
		t.Fatalf("浏览 /context 失败（疑似被 Close）: %v", err)
	}
	if len(entries) != wantCtx {
		t.Fatalf("/context 应仍有 %d 项，实得 %d: %v", wantCtx, len(entries), entries)
	}

	// 6) Docs.Path 空目录作为来源正常显示为空（不报错、不顶替 /context）
	a.mu.Lock()
	sysSrc, _ := a.findSource(systemDocsSource)
	a.mu.Unlock()
	sysEntries, err := a.listSourceDir(sysSrc, ".")
	if err != nil {
		t.Fatalf("空 doc 来源不应报错: %v", err)
	}
	if len(sysEntries) != 0 {
		t.Fatalf("空 doc 来源应显示为空，实得 %d 项", len(sysEntries))
	}
}

// #53：list_sources 工具稳定返回内置 /context（标记 builtin）+ 自定义来源；内置不可删除。
func TestListSourcesToolReturnsBuiltinPlusCustom(t *testing.T) {
	a := testApp(t)
	if err := os.MkdirAll(filepath.Join(a.workPath, "refs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "refs", "note.md"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("myref", "自定义", "local", map[string]any{"path": "refs"}, false)},
	}), 200)

	task := &Task{}
	out := a.executeToolCall(context.Background(), readCall("list_sources", `{}`), task, nil)
	if !strings.Contains(out, `"id":"context"`) || !strings.Contains(out, `"builtin":true`) {
		t.Fatalf("list_sources 缺内置 /context: %s", out)
	}
	if !strings.Contains(out, `"id":"myref"`) {
		t.Fatalf("list_sources 缺自定义来源: %s", out)
	}

	// 内置 /context 不可被删除：客户端提交漏掉它时必须自动补回
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("only", "仅剩", "local", map[string]any{"path": "refs"}, false)},
	}), 200)
	body := request(a, "GET", "/api/sources", nil).Body.String()
	if !strings.Contains(body, `"id":"context"`) {
		t.Fatalf("内置 /context 被删除: %s", body)
	}
}

// #53：默认工作区（Docs.Path 空）不回归——system-docs 回落 /context，来源列表正常。
func TestDefaultWorkspaceContextNoRegression(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.refPath, "keep.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	body := request(a, "GET", "/api/sources", nil).Body.String()
	if !strings.Contains(body, `"id":"context"`) || !strings.Contains(body, `"id":"system-docs"`) {
		t.Fatalf("默认工作区应含内置来源: %s", body)
	}
	// 默认空 Docs.Path → system-docs 回落 /context，能读到 keep.md
	task := &Task{}
	out := a.executeToolCall(context.Background(), readCall("read_file", `{"source":"system-docs","path":"keep.md"}`), task, nil)
	if out != "keep" {
		t.Fatalf("默认 system-docs 应回落 /context 读到 keep.md: %q", out)
	}
}
