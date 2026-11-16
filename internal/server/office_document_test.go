package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeDocumentReadEditAndIdlePreview(t *testing.T) {
	scripts, _ := filepath.Abs("../../scripts/office")
	t.Setenv("AIDE_OFFICE_SCRIPTS", scripts)
	for _, kind := range []string{"docx", "xlsx", "pptx"} {
		t.Run(kind, func(t *testing.T) {
			a, s := collaboraFixture(t)
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
			// Point a synthetic human lease at this target. Viewing is not a lock.
			ar, _ := a.officeDocumentArea(in)
			a.collabora.mu.Lock()
			lease := a.collabora.sessions[s["id"].(string)]
			lease.Doc.Area = ar
			lease.Doc.Path = p
			a.collabora.mu.Unlock()
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e != nil {
				t.Fatal("idle editor blocked", e)
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
			requireStatus(t, request(a, "PUT", "/api/office/collabora/sessions/"+s["id"].(string), map[string]any{"active": true}), 200)
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e == nil || !strings.Contains(e.Error(), "人工") {
				t.Fatal("active human not protected", e)
			}
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, "old-workspace"); e == nil {
				t.Fatal("stale workspace accepted")
			}
		})
	}
}
func TestCollaboraRealAcceptanceServer(t *testing.T) {
	if os.Getenv("AIDE_COLLABORA_REAL_SERVER") != "1" {
		t.Skip("explicit isolated browser acceptance harness")
	}
	work := os.Getenv("AIDE_COLLABORA_FIXTURES")
	if work == "" {
		t.Fatal("fixture directory required")
	}
	a, e := New(filepath.Join(work, "workspace"), filepath.Join(work, "context"), filepath.Join(work, "data"))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, _ := json.Marshal(map[string]string{"token": a.token})
	if e = os.WriteFile(filepath.Join(work, "browser-token.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Addr: ":19979", Handler: a.Handler()}
	t.Cleanup(func() { server.Close() })
	t.Log("Isolated Collabora acceptance server, HTTP only; synthetic fixture data")
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		t.Fatal(e)
	}
}

func TestOfficeDocumentNativeToolDispatchAndSandbox(t *testing.T) {
	useSourceOfficeScripts(t)
	a := testApp(t)
	a.pluginSurface = []byte(`{"plugins":[{"id":"office","tools":[{"name":"office_document_read","executable":true},{"name":"office_document_edit","executable":true}]}]}`)
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
