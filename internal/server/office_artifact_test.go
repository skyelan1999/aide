package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeCreateAndXlsxEdit(t *testing.T) {
	if err := checkOfficeArchive([]byte("not a workbook")); err == nil {
		t.Fatal("malformed Office archive accepted")
	}
	a := testApp(t)
	formats := []struct {
		kind    string
		content map[string]any
	}{
		{"docx", map[string]any{"title": "验收", "blocks": []any{map[string]any{"type": "paragraph", "text": "你好 Office"}}}},
		{"xlsx", map[string]any{"sheets": []any{map[string]any{"name": "数据", "rows": []any{[]any{"名称", "数值"}, []any{"甲", 12}}}}}},
		{"pptx", map[string]any{"slides": []any{map[string]any{"title": "标题", "body": "正文"}}}},
	}
	for _, tc := range formats {
		name := "成果." + tc.kind
		msg := a.officeCreateTool(a.workspace, "local", "", name, tc.kind, tc.content)
		if !strings.Contains(msg, "已生成") {
			t.Fatalf("create %s: %s", tc.kind, msg)
		}
		b, err := os.ReadFile(filepath.Join(a.workPath, name))
		if err != nil || len(b) < 4 || string(b[:2]) != "PK" {
			t.Fatalf("invalid %s: %v", tc.kind, err)
		}
		if again := a.officeCreateTool(a.workspace, "local", "", name, tc.kind, tc.content); !strings.Contains(again, "不会覆盖") {
			t.Fatalf("overwrite not blocked: %s", again)
		}
	}
	view := request(a, "GET", "/api/office/xlsx?path=%E6%88%90%E6%9E%9C.xlsx", nil)
	requireStatus(t, view, 200)
	var grid struct {
		Hash        string   `json:"hash"`
		WorkspaceID string   `json:"workspaceId"`
		Sheets      []string `json:"sheets"`
	}
	if err := json.Unmarshal(view.Body.Bytes(), &grid); err != nil || grid.Hash == "" || len(grid.Sheets) != 1 || grid.Sheets[0] != "数据" {
		t.Fatalf("view: %v %s", err, view.Body.String())
	}
	body := map[string]any{"path": "成果.xlsx", "hash": grid.Hash, "workspaceId": grid.WorkspaceID,
		"changes": []map[string]any{{"sheet": "数据", "ref": "B2", "value": 27}}}
	edit := request(a, "PUT", "/api/office/xlsx", body)
	requireStatus(t, edit, 200)
	stale := request(a, "PUT", "/api/office/xlsx", body)
	requireStatus(t, stale, 409)
	view = request(a, "GET", "/api/office/xlsx?path=%E6%88%90%E6%9E%9C.xlsx", nil)
	if !strings.Contains(view.Body.String(), `"value":27`) {
		t.Fatalf("edit not persisted: %s", view.Body.String())
	}
}

func TestOfficeXlsxReferencePermissions(t *testing.T) {
	a := testApp(t)
	b, err := officeCreateBytes("xlsx", map[string]any{"sheets": []any{map[string]any{"name": "引用", "rows": []any{[]any{"原值"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	refDir := a.reference.Name()
	if err := os.WriteFile(filepath.Join(refDir, "ref.xlsx"), b, 0644); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "office-ref", Name: "Office", Type: "local", Enabled: true, RW: false})
	view := request(a, "GET", "/api/office/xlsx?source=office-ref&path=ref.xlsx", nil)
	requireStatus(t, view, 200)
	if !strings.Contains(view.Body.String(), `"readOnly":true`) {
		t.Fatalf("readonly missing: %s", view.Body.String())
	}
	edit := request(a, "PUT", "/api/office/xlsx", map[string]any{"path": "ref.xlsx", "source": "office-ref", "workspaceId": "source:office-ref", "hash": hash(b), "changes": []map[string]any{{"sheet": "引用", "ref": "A1", "value": "新值"}}})
	requireStatus(t, edit, 403)
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "office-ref" {
			a.sourceRegistry.Sources[i].RW = true
		}
	}
	edit = request(a, "PUT", "/api/office/xlsx", map[string]any{"path": "ref.xlsx", "source": "office-ref", "workspaceId": "source:office-ref", "hash": hash(b), "changes": []map[string]any{{"sheet": "引用", "ref": "A1", "value": "新值"}}})
	requireStatus(t, edit, 200)
	view = request(a, "GET", "/api/office/xlsx?source=office-ref&path=ref.xlsx", nil)
	if !strings.Contains(view.Body.String(), "新值") {
		t.Fatalf("reference edit not persisted: %s", view.Body.String())
	}
}
