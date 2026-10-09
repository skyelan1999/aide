package server

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceActionFixture(t *testing.T) *App {
	a := testApp(t)
	a.sourceRegistry = sourcesRegistry{Version: 1, Sources: []Source{{ID: "writable-actions", Name: "Writable", Type: "local", Enabled: true, RW: true}, {ID: "readonly-actions", Name: "Read only", Type: "local", Enabled: true}}}
	if err := os.MkdirAll(filepath.Join(a.reference.Name(), "nested", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "nested", "file name.md"), []byte("reference bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "sentinel.md"), []byte("workspace unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	return a
}

// Opt-in real SFTP server with disposable fixture credentials only.
func TestSourceFileActionsLiveSFTP(t *testing.T) {
	host := os.Getenv("AIDE_SOURCE_ACTION_FIXTURE_HOST")
	if host == "" {
		t.Skip("source action fixture not started")
	}
	a := testApp(t)
	id := "action-sftp"
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody(id, "SFTP actions", "sftp", map[string]any{"host": host, "port": 22, "username": "fixture", "auth": "password", "path": "/srv/refs"}, true)}, "secrets": map[string]any{id: map[string]any{"password": "fixture-pass"}}}), 200)
	dir := "actions-" + newID()
	requireStatus(t, request(a, "POST", "/api/directory", map[string]string{"root": "context", "source": id, "parent": ".", "name": dir}), 200)
	body := func(p string) map[string]string { return map[string]string{"root": "context", "source": id, "path": p} }
	defer func() { _ = request(a, "POST", "/api/file/delete", body(dir)) }()
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": id, "path": dir + "/notes.txt", "content": "sftp roundtrip"}), 200)
	w := request(a, "GET", "/api/file/properties?root=context&source="+id+"&path="+dir+"/notes.txt", nil)
	requireStatus(t, w, 200)
	var info map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info["size"] != float64(14) || info["modified"] == nil {
		t.Fatal(info)
	}
	rename := body(dir + "/notes.txt")
	rename["newName"] = "renamed.txt"
	requireStatus(t, request(a, "POST", "/api/file/rename", rename), 200)
	requireStatus(t, request(a, "POST", "/api/file/archive", body(dir+"/renamed.txt")), 200)
	w = request(a, "POST", "/api/file/extract", body(dir+"/renamed.txt.zip"))
	requireStatus(t, w, 200)
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	w = request(a, "GET", "/api/file?source="+id+"&path="+out["path"]+"/renamed.txt", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "sftp roundtrip") {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, request(a, "POST", "/api/file/delete", body(out["path"])), 200)
	requireStatus(t, request(a, "POST", "/api/file/delete", body(dir)), 200)
}
func TestWritableSourceFileActions(t *testing.T) {
	a := sourceActionFixture(t)
	body := func(p string) map[string]string {
		return map[string]string{"root": "context", "source": "writable-actions", "path": p}
	}
	w := request(a, "GET", "/api/file/properties?root=context&source=writable-actions&path="+url.QueryEscape("nested/file name.md"), nil)
	requireStatus(t, w, 200)
	var info map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info["name"] != "file name.md" || info["size"] != float64(15) {
		t.Fatal(info)
	}
	rename := body("nested/file name.md")
	rename["newName"] = "renamed.md"
	requireStatus(t, request(a, "POST", "/api/file/rename", rename), 200)
	requireStatus(t, request(a, "POST", "/api/file/archive", body("nested")), 200)
	requireStatus(t, request(a, "POST", "/api/file/archive", body("nested")), 409)
	w = request(a, "POST", "/api/file/extract", body("nested.zip"))
	requireStatus(t, w, 200)
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	b, e := os.ReadFile(filepath.Join(a.reference.Name(), out["path"], "nested", "renamed.md"))
	if e != nil || string(b) != "reference bytes" {
		t.Fatalf("extracted bytes: %q %v", b, e)
	}
	if _, e = os.Stat(filepath.Join(a.reference.Name(), out["path"], "nested", "empty")); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "POST", "/api/file/delete", body(out["path"])), 200)
	if _, e = os.Stat(filepath.Join(a.reference.Name(), out["path"])); !os.IsNotExist(e) {
		t.Fatal("delete failed", e)
	}
	b, e = os.ReadFile(filepath.Join(a.workPath, "sentinel.md"))
	if e != nil || string(b) != "workspace unchanged" {
		t.Fatal("workspace changed", e)
	}
}
func TestSourceFileActionsRejectReadonlyAndInvalidTargets(t *testing.T) {
	a := sourceActionFixture(t)
	requireStatus(t, request(a, "GET", "/api/file/properties?root=context&source=readonly-actions&path=nested", nil), 200)
	for _, op := range []string{"rename", "delete", "archive", "extract"} {
		requireStatus(t, request(a, "POST", "/api/file/"+op, map[string]string{"root": "context", "source": "readonly-actions", "path": "nested", "newName": "new"}), 403)
		requireStatus(t, request(a, "POST", "/api/file/"+op, map[string]string{"root": "workspace", "source": "writable-actions", "path": "nested", "newName": "new"}), 400)
		for _, p := range []string{".", "../sentinel.md", "nested\nrm"} {
			requireStatus(t, request(a, "POST", "/api/file/"+op, map[string]string{"root": "context", "source": "writable-actions", "path": p, "newName": "new"}), 400)
		}
	}
	requireStatus(t, request(a, "POST", "/api/file/rename", map[string]string{"root": "context", "source": "writable-actions", "path": "nested", "newName": "../escape"}), 400)
	a.sourceRegistry.Sources[0].Enabled = false
	requireStatus(t, request(a, "POST", "/api/file/delete", map[string]string{"root": "context", "source": "writable-actions", "path": "nested"}), 403)
	if _, e := os.Stat(filepath.Join(a.reference.Name(), "nested", "file name.md")); e != nil {
		t.Fatal("rejected request mutated files", e)
	}
}
