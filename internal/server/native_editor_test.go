package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editorAppFixture(t *testing.T) (*App, string) {
	t.Helper()
	a := workbenchFixture(t)
	root, cfg, _ := editorTargetFixture(t)
	a.workspace = root
	a.workPath = root.Name()
	a.pluginRegistry.Plugins[0].Enabled = true
	dir := filepath.Join(a.pluginsPath, "brdplan")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "ui.html"), []byte("<p>native editor</p>"), 0600)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "brdplan", Name: "BrdPlan", Enabled: true, Main: "index.js", Workbench: &PluginWorkbench{Protocol: 1, Entry: "ui.html", Title: "BrdPlan"}})
	token := filepath.Join(root.Name(), "token")
	os.WriteFile(token, []byte(strings.Repeat("s", 64)), 0600)
	t.Setenv("AIDE_BRDPLAN_EDITOR_WORKSPACE", cfg.WorkspaceRoot)
	t.Setenv("AIDE_BRDPLAN_EDITOR_SESSION", cfg.SessionPath)
	t.Setenv("AIDE_BRDPLAN_EDITOR_TOKEN_FILE", token)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 64) {
			t.Error("missing backend auth")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/lease/open":
			json.NewEncoder(w).Encode(map[string]any{"leaseId": strings.Repeat("b", 64), "project": "STM32", "view": "pcb", "generation": "generation_1"})
		case "/lease/close":
			json.NewEncoder(w).Encode(map[string]any{"state": "unknown", "saved": "unknown", "editorActive": true})
		case "/lease/heartbeat":
			json.NewEncoder(w).Encode(map[string]any{"expiresIn": 60})
		case "/core/rfb.js":
			w.Header().Set("Content-Type", "text/javascript")
			w.Write([]byte("/* fixed mock asset */"))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(backend.Close)
	t.Setenv("AIDE_BRDPLAN_EDITOR_URL", backend.URL)
	return a, root.Name()
}
func editorOpen(t *testing.T, a *App) string {
	t.Helper()
	w := request(a, "POST", "/api/plugins/brdplan/editor/open", map[string]any{"protocol": 1, "requestId": "editor_1", "sessionPath": "session", "view": "pcb"})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var out struct{ URL string }
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.URL
}
func TestNativeEditorOpaqueViewerAndScopeRevocation(t *testing.T) {
	a, _ := editorAppFixture(t)
	url := editorOpen(t, a)
	w := workbenchRequest(a, "GET", url, false)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;") || strings.Contains(w.Header().Get("Content-Security-Policy"), "allow-same-origin") || strings.Contains(w.Body.String(), strings.Repeat("s", 64)) {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	w = workbenchRequest(a, "GET", url+"core/rfb.js", false)
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "null" || len(w.Header().Values("Access-Control-Allow-Origin")) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = workbenchRequest(a, "GET", url+"arbitrary.html", false); w.Code != 404 {
		t.Fatal(w.Code)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if w = workbenchRequest(a, "GET", url, false); w.Code != 403 {
		t.Fatal("stale scope accepted", w.Code)
	}
}
func TestNativeEditorAdmissionRejectsInFlightAndReadOnly(t *testing.T) {
	a, _ := editorAppFixture(t)
	_, release := a.nativeEditorEnter(context.Background())
	w := request(a, "POST", "/api/plugins/brdplan/editor/open", map[string]any{"protocol": 1, "requestId": "editor_1", "sessionPath": "session", "view": "pcb"})
	release()
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.settings.SandboxMode = "read-only"
	w = request(a, "POST", "/api/plugins/brdplan/editor/open", map[string]any{"protocol": 1, "requestId": "editor_1", "sessionPath": "session", "view": "pcb"})
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestNativeEditorPersistentUnknownBlocksHostWrites(t *testing.T) {
	a, root := editorAppFixture(t)
	marker := filepath.Join(root, "session/bridge/editor-active.json")
	os.WriteFile(marker, []byte(`{"active":true}`), 0600)
	if a.nativeEditorWriteError() == nil {
		t.Fatal("active editor not guarded")
	}
	for _, route := range []string{"/api/file", "/api/file/upload", "/api/directory", "/api/command", "/api/sessions/id/runs/run/apply"} {
		method := "POST"
		if route == "/api/file" {
			method = "PUT"
		}
		w := request(a, method, route, map[string]any{"path": "probe", "content": "x"})
		if w.Code != 409 {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
	if _, _, e := a.execShellCommand(context.Background(), nil, "echo blocked"); e == nil {
		t.Fatal("shell ran during editing")
	}
	if _, e := a.callPluginTool("unknown", "unknown", nil); e == nil {
		t.Fatal("unknown plugin admitted")
	}
	os.WriteFile(marker, []byte("corrupt"), 0600)
	if a.nativeEditorWriteError() == nil {
		t.Fatal("uncertain marker unlocked")
	}
	os.WriteFile(marker, []byte(`{"active":false}`), 0600)
	if e := a.nativeEditorWriteError(); e != nil {
		t.Fatal(e)
	}
}
