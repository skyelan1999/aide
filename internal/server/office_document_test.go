package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeDocumentReadEditChecksVersion(t *testing.T) {
	scripts, _ := filepath.Abs("../../scripts/office")
	t.Setenv("AIDE_OFFICE_SCRIPTS", scripts)
	for _, kind := range []string{"docx", "xlsx", "pptx"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			spec := map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Original"}}}
			if kind == "xlsx" {
				spec = map[string]any{"sheets": []any{map[string]any{"name": "Data", "rows": []any{[]any{"Original", 42}}}}}
			}
			if kind == "pptx" {
				spec = map[string]any{"slides": []any{map[string]any{"title": "Original", "body": "Body"}}}
			}
			b, e := officeCreateBytes(kind, spec)
			if e != nil {
				t.Fatal(e)
			}
			p := "test." + kind
			dwgWrite(t, a, p, b)
			in := officeDocumentRequest{Path: p}
			out, e := a.officeDocumentOperation(context.Background(), in, false, true, a.wsID())
			if e != nil {
				t.Fatal(e)
			}
			blocks := out["blocks"].([]any)
			block := blocks[0].(map[string]any)
			in.Hash = out["hash"].(string)
			in.Changes = []map[string]any{{"id": block["id"], "expectedText": block["text"], "newText": "Updated by AI"}}
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e != nil {
				t.Fatal(e)
			}
			after, _ := a.workspace.ReadFile(p)
			text, e := officeExtractText(after, "."+kind)
			if e != nil || !strings.Contains(text, "Updated by AI") {
				t.Fatal(text, e)
			}
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e == nil {
				t.Fatal("stale hash accepted")
			}
			out, e = a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: p}, false, true, a.wsID())
			if e != nil {
				t.Fatal(e)
			}
			in.Hash = out["hash"].(string)
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, "old-workspace"); e == nil {
				t.Fatal("stale workspace accepted")
			}
		})
	}
}
func TestOfficeDocumentNativeToolDispatchAndSandbox(t *testing.T) {
	useSourceOfficeScripts(t)
	a := testApp(t)
	b, e := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Native dispatch"}}})
	if e != nil {
		t.Fatal(e)
	}
	dwgWrite(t, a, "dispatch.docx", b)
	out := releasePublicNative(t, a, "office_document_read", map[string]any{"path": "dispatch.docx"})
	var result map[string]any
	if json.Unmarshal([]byte(out), &result) != nil {
		t.Fatal(out)
	}
	block := result["blocks"].([]any)[0].(map[string]any)
	out = releasePublicNative(t, a, "office_document_edit", map[string]any{"path": "dispatch.docx", "hash": result["hash"], "changes": []any{map[string]any{"id": block["id"], "expectedText": block["text"], "newText": "Native AI edited"}}})
	if !strings.Contains(out, `"saved":true`) {
		t.Fatal(out)
	}
	a.mu.Lock()
	a.settings.SandboxMode = "read-only"
	a.mu.Unlock()
	if a.sandboxToolError("office_document_read") != nil || a.sandboxToolError("office_document_edit") == nil {
		t.Fatal("read-only sandbox boundary wrong")
	}
}
