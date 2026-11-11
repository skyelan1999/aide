package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mapWorkbenchFixture(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	entries := map[string]string{"manifest.json": `{"id":"mapcanvas","name":"MapCanvas fixture","version":"0.1.0","main":"index.js","workbench":{"protocol":1,"entry":"viewer.html","title":"MapCanvas fixture","tools":["mapcanvas_browse","mapcanvas_read"],"fileHandler":{"extensions":[".pgm",".png"],"editable":true}}}`, "index.js": `module.exports={apply(ctx){for(const name of ['mapcanvas_browse','mapcanvas_read'])ctx.tool({name,description:'fixture',parameters:{type:'object',properties:{}},handler:()=>({})})}};`, "viewer.html": "<!doctype html><title>MapCanvas fixture</title>"}
	requireStatus(t, pluginBundleRequest(t, a, entries), 201)
	out := request(a, "POST", "/api/plugins/mapcanvas/workbench/open", map[string]any{})
	requireStatus(t, out, 200)
	var b struct {
		URL    string `json:"url"`
		Bridge struct {
			Key string `json:"instanceId"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, workbenchRequest(a, "GET", b.URL, false), 200)
	if err := os.WriteFile(filepath.Join(a.workPath, "map.pgm"), []byte("P5\n2 1\n255\n\x00\xff"), 0600); err != nil {
		t.Fatal(err)
	}
	return a, b.Bridge.Key
}
func mapCall(a *App, key, id, method string, args map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/plugins/mapcanvas/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": method, "arguments": args})
}
func TestMapWorkbenchSourceArgumentScope(t *testing.T) {
	for _, tc := range []struct {
		id, tool string
		args     map[string]any
		want     bool
	}{
		{"mapcanvas", "mapcanvas_read", map[string]any{"source": "source:fixture", "path": "map.pgm", "offset": 0}, true},
		{"mapcanvas", "mapcanvas_browse", map[string]any{"source": "context", "path": "."}, true},
		{"other", "mapcanvas_read", map[string]any{"source": "context", "path": "map.pgm"}, false},
		{"mapcanvas", "other_read", map[string]any{"source": "context", "path": "map.pgm"}, false},
		{"mapcanvas", "mapcanvas_read", map[string]any{"source": "context", "path": "map.pgm", "command": "x"}, false},
		{"mapcanvas", "mapcanvas_read", map[string]any{"source": "/private", "path": "map.pgm"}, false},
	} {
		if got := workbenchToolArgumentsAllowed(tc.id, tc.tool, tc.args); got != tc.want {
			t.Fatalf("scope %s %s: %v", tc.id, tc.tool, got)
		}
	}
}
func TestMapDocumentBindSaveVersionAndPendingDraft(t *testing.T) {
	a, key := mapWorkbenchFixture(t)
	original, _ := os.ReadFile(filepath.Join(a.workPath, "map.pgm"))
	args := map[string]any{"source": "workspace", "path": "map.pgm", "version": hash(original)}
	bad := map[string]any{"source": "workspace", "path": "map.pgm", "version": strings.Repeat("0", 64)}
	requireStatus(t, mapCall(a, key, "stale", "map.document.bind", bad), 409)
	out := mapCall(a, key, "bind", "map.document.bind", args)
	requireStatus(t, out, 200)
	if !strings.Contains(out.Body.String(), `"editable":true`) {
		t.Fatal(out.Body.String())
	}
	changed := []byte("P5\n2 1\n255\n\x7f\xff")
	requireStatus(t, mapCall(a, key, "begin", "document.save.begin", map[string]any{"size": len(changed), "version": hash(original)}), 200)
	requireStatus(t, mapCall(a, key, "pending_bind", "map.document.bind", args), 403)
	requireStatus(t, mapCall(a, key, "chunk", "document.save.chunk", map[string]any{"offset": 0, "data": base64.StdEncoding.EncodeToString(changed)}), 200)
	requireStatus(t, mapCall(a, key, "prepare", "document.save.request", nil), 200)
	before, _ := os.ReadFile(filepath.Join(a.workPath, "map.pgm"))
	if string(before) != string(original) {
		t.Fatal("saved before parent confirmation")
	}
	requireStatus(t, mapCall(a, key, "commit", "document.save.commit", nil), 200)
	after, _ := os.ReadFile(filepath.Join(a.workPath, "map.pgm"))
	if string(after) != string(changed) {
		t.Fatal("image save failed")
	}
	requireStatus(t, mapCall(a, key, "bind", "map.document.bind", args), 403)
}
func TestMapDocumentReadonlyAndInvalidSelection(t *testing.T) {
	a, key := mapWorkbenchFixture(t)
	p := a.reference.Name()
	data := []byte("P5\n1 1\n255\n\xff")
	os.WriteFile(filepath.Join(p, "map.pgm"), data, 0600)
	src := Source{ID: "map-readonly", Name: "Map readonly", Type: "local", Enabled: true}
	src.Config.Path = "/context"
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src)
	out := mapCall(a, key, "readonly", "map.document.bind", map[string]any{"source": "source:map-readonly", "path": "map.pgm", "version": hash(data)})
	requireStatus(t, out, 200)
	if !strings.Contains(out.Body.String(), `"editable":false`) {
		t.Fatal(out.Body.String())
	}
	requireStatus(t, mapCall(a, key, "deny_write", "document.save.begin", map[string]any{"size": len(data), "version": hash(data)}), 403)
	for i, p := range []string{"../map.pgm", "/tmp/map.pgm", "map.yaml"} {
		requireStatus(t, mapCall(a, key, fmt.Sprintf("invalid_%d", i), "map.document.bind", map[string]any{"source": "workspace", "path": p, "version": hash(data)}), 400)
	}
	requireStatus(t, mapCall(a, key, "hidden", "map.document.bind", map[string]any{"source": "source:missing", "path": "map.pgm", "version": hash(data)}), 403)
	if ok, _ := marketCompatible(marketPlugin{Features: []string{"map-image-document-v1"}, MinAideVersion: "0.1.18.0 RC3"}, "0.1.18.0 RC3"); !ok {
		t.Fatal("new host feature not advertised")
	}
}
