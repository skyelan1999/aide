package server

// #63 侧车批注 + Office 工具测试。
// 运行环境：aide:local 容器（含 python3 + python-docx 1.2.0），仓库挂在 /src。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorkspaceFile 在测试工作区写一个文件（宿主视角路径）。
func writeWorkspaceFile(t *testing.T, a *App, name, content string) {
	t.Helper()
	full := filepath.Join(a.workPath, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCommentsCRUD 新建/查询/更新状态/添加回复/删除全流程。
func TestCommentsCRUD(t *testing.T) {
	a := testApp(t)
	writeWorkspaceFile(t, a, "notes/doc.docx", "v1 bytes")
	h := hash([]byte("v1 bytes"))

	// 新建
	create := map[string]any{
		"path": "notes/doc.docx", "hash": h,
		"anchorQuote": "v1", "anchorIndex": 0, "text": "第一条批注", "author": "me",
	}
	rec := request(a, "POST", "/api/comments", create)
	requireStatus(t, rec, 201)
	var c Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Status != "open" || c.Text != "第一条批注" || c.DocPath != "notes/doc.docx" {
		t.Fatalf("新建返回异常: %+v", c)
	}
	if c.Stale {
		t.Fatal("hash 一致时不应 stale")
	}

	// 查询
	rec = request(a, "GET", "/api/comments?path=notes/doc.docx", nil)
	var list struct {
		Comments []Comment `json:"comments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Comments) != 1 {
		t.Fatalf("查询失败: %v %s", err, rec.Body.String())
	}

	// 标记解决
	rec = request(a, "PUT", "/api/comments/"+c.ID, map[string]any{"status": "resolved"})
	requireStatus(t, rec, 200)
	if strings.Contains(rec.Body.String(), `"status":"resolved"`) == false {
		t.Fatalf("状态更新失败: %s", rec.Body.String())
	}

	// 添加回复
	rec = request(a, "PUT", "/api/comments/"+c.ID,
		map[string]any{"reply": map[string]string{"text": "回复内容", "author": "me"}})
	requireStatus(t, rec, 200)
	var c2 Comment
	_ = json.Unmarshal(rec.Body.Bytes(), &c2)
	if len(c2.Replies) != 1 || c2.Replies[0].Text != "回复内容" {
		t.Fatalf("回复失败: %+v", c2)
	}

	// 删除
	rec = request(a, "DELETE", "/api/comments/"+c.ID, nil)
	requireStatus(t, rec, 200)
	rec = request(a, "GET", "/api/comments?path=notes/doc.docx", nil)
	list.Comments = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Comments) != 0 {
		t.Fatalf("删除后应无批注: %+v", list.Comments)
	}
	// 删除审计
	if b, err := os.ReadFile(filepath.Join(AuditDir(a.dataPath), "comments-audit.jsonl")); err != nil || !strings.Contains(string(b), "comment-delete") {
		t.Fatalf("审计日志缺失: %v %s", err, b)
	}
}

// TestCommentsStale 文档内容变化 → stale=true；文件删除 → stale。
func TestCommentsStale(t *testing.T) {
	a := testApp(t)
	writeWorkspaceFile(t, a, "d.docx", "版本一")
	h1 := hash([]byte("版本一"))
	rec := request(a, "POST", "/api/comments", map[string]any{
		"path": "d.docx", "hash": h1, "text": "批注",
	})
	var c Comment
	_ = json.Unmarshal(rec.Body.Bytes(), &c)

	get := func() Comment {
		rec := request(a, "GET", "/api/comments?path=d.docx", nil)
		var list struct {
			Comments []Comment `json:"comments"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
		if len(list.Comments) != 1 {
			t.Fatalf("列表异常: %s", rec.Body.String())
		}
		return list.Comments[0]
	}
	if got := get(); got.Stale {
		t.Fatal("内容未变不应 stale")
	}

	writeWorkspaceFile(t, a, "d.docx", "版本二（已改）")
	if got := get(); !got.Stale {
		t.Fatal("内容变化后应 stale")
	}

	if err := os.Remove(filepath.Join(a.workPath, "d.docx")); err != nil {
		t.Fatal(err)
	}
	if got := get(); !got.Stale {
		t.Fatal("文件删除后应 stale")
	}
}

// TestCommentsPathSafety ../、绝对路径、反斜杠一律拒绝。
func TestCommentsPathSafety(t *testing.T) {
	a := testApp(t)
	for _, p := range []string{"../escape.docx", "/etc/passwd", `a\b.docx`} {
		rec := request(a, "POST", "/api/comments", map[string]any{"path": p, "text": "x"})
		if rec.Code != 400 {
			t.Fatalf("路径 %q 应被拒绝，实际 %d %s", p, rec.Code, rec.Body.String())
		}
	}
}

// TestCommentsFilePerms 目录 0700、批注文件 0600。
func TestCommentsFilePerms(t *testing.T) {
	a := testApp(t)
	writeWorkspaceFile(t, a, "p.docx", "x")
	rec := request(a, "POST", "/api/comments", map[string]any{"path": "p.docx", "text": "t"})
	var c Comment
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	dir := CommentsDir(a.dataPath)
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0700 {
		t.Fatalf("comments 目录权限: %v %v", st, err)
	}
	full := filepath.Join(dir, commentPathKey("p.docx"), c.ID+".json")
	st, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("批注文件权限 %o，要 0600", st.Mode().Perm())
	}
}

// makeFixtureDocx 在测试工作区生成含标题/段落/表格的 .docx。
func makeFixtureDocx(t *testing.T, a *App, name string) {
	t.Helper()
	gen := `
import sys
from docx import Document
d = Document()
d.add_heading("设计说明", level=1)
d.add_paragraph("这是背景段落。")
d.add_heading("方案", level=2)
d.add_paragraph("这里写方案细节。")
t = d.add_table(rows=2, cols=3)
d.save(sys.argv[1])
`
	full := filepath.Join(a.workPath, name)
	cmd := exec.Command("python3", "-c", gen, full)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("跳过 Office 工具测试（容器无 python-docx）: %v %s", err, out)
	}
}

// TestDocxStructureTool docx_structure 解析标题/段落/表格。
func TestDocxStructureTool(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	makeFixtureDocx(t, a, "fixture.docx")

	out := a.docxTool(a.workspace, "", "fixture.docx", "docx_structure.py")
	if strings.HasPrefix(out, "错误") || strings.Contains(out, "失败") {
		t.Fatalf("docx_structure 失败: %s", out)
	}
	var st struct {
		Headings   []map[string]any `json:"headings"`
		Tables     []map[string]any `json:"tables"`
		Paragraphs []map[string]any `json:"paragraphs"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("输出不是 JSON: %s (%v)", out, err)
	}
	if len(st.Headings) != 2 {
		t.Fatalf("标题数量异常: %s", out)
	}
	if st.Headings[0]["text"] != "设计说明" || st.Headings[0]["level"].(float64) != 1 {
		t.Fatalf("一级标题异常: %+v", st.Headings[0])
	}
	if len(st.Tables) != 1 || st.Tables[0]["rows"].(float64) != 2 || st.Tables[0]["cols"].(float64) != 3 {
		t.Fatalf("表格解析异常: %s", out)
	}
	if len(st.Paragraphs) != 2 {
		t.Fatalf("段落数量异常: %s", out)
	}
}

// TestDocxCommentRoundTrip 加批注→列出→标记解决→重开校验。
func TestDocxCommentRoundTrip(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	makeFixtureDocx(t, a, "c.docx")

	if out := a.docxTool(a.workspace, "", "c.docx", "docx_add_comment.py",
		"方案细节", "这里要复核", "tester", "0"); !strings.Contains(out, `"ok": true`) {
		t.Fatalf("add 失败: %s", out)
	}
	listOut := a.docxTool(a.workspace, "", "c.docx", "docx_list_comments.py")
	if !strings.Contains(listOut, "这里要复核") {
		t.Fatalf("list 失败: %s", listOut)
	}
	if out := a.docxTool(a.workspace, "", "c.docx", "docx_resolve_comment.py", "0"); !strings.Contains(out, `"resolved": true`) {
		t.Fatalf("resolve 失败: %s", out)
	}
	// 文件仍可被 python-docx 正常打开
	full := filepath.Join(a.workPath, "c.docx")
	cmd := exec.Command("python3", "-c", "import sys; from docx import Document; d=Document(sys.argv[1]); print(len(d.comments))", full)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "1") {
		t.Fatalf("解决后文件损坏: %s %v", out, err)
	}
}

// TestDocxToolPathSafety 路径/扩展名/存在性校验。
func TestDocxToolPathSafety(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	if out := a.docxTool(a.workspace, "", "../x.docx", "docx_structure.py"); !strings.Contains(out, "路径无效") {
		t.Fatalf(".. 应被拒绝: %s", out)
	}
	if out := a.docxTool(a.workspace, "", "note.txt", "docx_structure.py"); !strings.Contains(out, "仅支持 .docx") {
		t.Fatalf("非 .docx 应被拒绝: %s", out)
	}
	if out := a.docxTool(a.workspace, "", "missing.docx", "docx_structure.py"); !strings.Contains(out, "文件不存在") {
		t.Fatalf("不存在文件应报错: %s", out)
	}
}

// TestLegacyDocRejected .doc 在查看器端点得到明确提示。
func TestLegacyDocRejected(t *testing.T) {
	a := testApp(t)
	rec := request(a, "GET", "/api/file?path=old.doc", nil)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), ".doc") {
		t.Fatalf(".doc 应被明确拒绝: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(a, "GET", "/api/file/raw?path=old.doc", nil)
	if rec.Code != 400 {
		t.Fatalf("raw 端点也应拒绝 .doc: %d", rec.Code)
	}
}
