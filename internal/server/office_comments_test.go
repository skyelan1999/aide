package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func officeCommentFixture(t *testing.T, a *App) []byte {
	t.Helper()
	b, err := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "开头甲乙丙结尾"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "批注文档.docx"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return b
}

func officePart(t *testing.T, b []byte, part string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == part {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			out, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
	}
	t.Fatalf("DOCX 缺少 %s", part)
	return ""
}

func TestOfficeNativeCommentsRoundTrip(t *testing.T) {
	a := testApp(t)
	b := officeCommentFixture(t, a)
	base := map[string]any{"path": "批注文档.docx", "hash": hash(b), "workspaceId": a.wsID()}
	get := request(a, "GET", "/api/office/docx/comments?path=%E6%89%B9%E6%B3%A8%E6%96%87%E6%A1%A3.docx", nil)
	requireStatus(t, get, 200)
	if !strings.Contains(get.Body.String(), `"comments":[]`) {
		t.Fatalf("initial list: %s", get.Body.String())
	}
	base["quote"], base["text"], base["author"] = "甲乙丙", "删掉乙", "测试者"
	add := request(a, "POST", "/api/office/docx/comments", base)
	requireStatus(t, add, 200)
	var added struct {
		ID   int    `json:"id"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(add.Body.Bytes(), &added); err != nil || added.Hash == "" {
		t.Fatalf("add: %v %s", err, add.Body.String())
	}
	stored, err := os.ReadFile(filepath.Join(a.workPath, "批注文档.docx"))
	if err != nil {
		t.Fatal(err)
	}
	download := request(a, "GET", "/api/file/raw?root=workspace&path=%E6%89%B9%E6%B3%A8%E6%96%87%E6%A1%A3.docx", nil)
	requireStatus(t, download, 200)
	if !bytes.Equal(download.Body.Bytes(), stored) {
		t.Fatal("downloaded DOCX differs from saved native-comment file")
	}
	for _, expected := range []string{"删掉乙", "测试者"} {
		if !strings.Contains(officePart(t, stored, "word/comments.xml"), expected) {
			t.Fatalf("portable comment missing %q", expected)
		}
	}
	document := officePart(t, stored, "word/document.xml")
	for _, marker := range []string{"commentRangeStart", "commentRangeEnd", "commentReference"} {
		if !strings.Contains(document, marker) {
			t.Fatalf("portable anchor missing %s", marker)
		}
	}
	get = request(a, "GET", "/api/office/docx/comments?path=%E6%89%B9%E6%B3%A8%E6%96%87%E6%A1%A3.docx", nil)
	requireStatus(t, get, 200)
	if !strings.Contains(get.Body.String(), `"anchorQuote":"甲乙丙"`) {
		t.Fatalf("list anchor: %s", get.Body.String())
	}
	stale := request(a, "POST", "/api/office/docx/comments", base)
	requireStatus(t, stale, 409)
	bad := request(a, "PUT", "/api/office/docx/comments", map[string]any{"path": "批注文档.docx", "hash": added.Hash, "workspaceId": a.wsID(), "id": added.ID, "expectedText": "wrong", "newText": "甲丙"})
	requireStatus(t, bad, 400)
	edit := request(a, "PUT", "/api/office/docx/comments", map[string]any{"path": "批注文档.docx", "hash": added.Hash, "workspaceId": a.wsID(), "id": added.ID, "expectedText": "甲乙丙", "newText": "甲丙"})
	requireStatus(t, edit, 200)
	updated, err := os.ReadFile(filepath.Join(a.workPath, "批注文档.docx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(officePart(t, updated, "word/document.xml"), "甲丙") || !strings.Contains(officePart(t, updated, "word/comments.xml"), "删掉乙") {
		t.Fatal("editing lost text or comment")
	}
	if out := a.officeCommentTool(a.workspace, "local", "", "批注文档.docx", "list", map[string]any{}); !strings.Contains(out, `"anchorQuote":"甲丙"`) {
		t.Fatalf("plugin reader: %s", out)
	}
}

func TestOfficeNativeCommentsReferenceReadOnly(t *testing.T) {
	a := testApp(t)
	b, err := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "引用原文"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "ref.docx"), b, 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "comment-ref", Name: "引用", Type: "local", Enabled: true, RW: false})
	get := request(a, "GET", "/api/office/docx/comments?source=comment-ref&path=ref.docx", nil)
	requireStatus(t, get, 200)
	if !strings.Contains(get.Body.String(), `"readOnly":true`) {
		t.Fatalf("reference permission: %s", get.Body.String())
	}
	post := request(a, "POST", "/api/office/docx/comments", map[string]any{"path": "ref.docx", "source": "comment-ref", "hash": hash(b), "workspaceId": "source:comment-ref", "quote": "引用原文", "text": "提醒"})
	requireStatus(t, post, 403)
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "comment-ref" {
			a.sourceRegistry.Sources[i].RW = true
		}
	}
	post = request(a, "POST", "/api/office/docx/comments", map[string]any{"path": "ref.docx", "source": "comment-ref", "hash": hash(b), "workspaceId": "source:comment-ref", "quote": "引用原文", "text": "提醒"})
	requireStatus(t, post, 200)
	refBytes, err := os.ReadFile(filepath.Join(a.reference.Name(), "ref.docx"))
	if err != nil || !strings.Contains(officePart(t, refBytes, "word/comments.xml"), "提醒") {
		t.Fatalf("read/write reference did not persist native comment: %v", err)
	}
}
