package server

import (
	"aide/internal/localmounts"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalMountSettingsRevisionAndActions(t *testing.T) {
	a := testApp(t)
	get := request(a, "GET", "/api/local-mounts", nil)
	requireStatus(t, get, 200)
	var snapshot struct{ Revision string }
	json.Unmarshal(get.Body.Bytes(), &snapshot)
	config := localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "archive", Name: "Archive", Path: "E:/Projects", ReadOnly: true}}}
	body := map[string]any{"config": config, "revision": snapshot.Revision}
	requireStatus(t, request(a, "PUT", "/api/local-mounts", body), 200)
	requireStatus(t, request(a, "PUT", "/api/local-mounts", body), 409)
	requireStatus(t, request(a, "POST", "/api/local-mounts/action", map[string]any{"action": "choose"}), 409)
	localmounts.Write(filepath.Join(a.localMountDir(), "host.json"), map[string]any{"updated": time.Now().Unix(), "os": "windows"})
	requireStatus(t, request(a, "POST", "/api/local-mounts/action", map[string]any{"action": "apply", "revision": "stale"}), 409)
	r := request(a, "POST", "/api/local-mounts/action", map[string]any{"action": "choose"})
	requireStatus(t, r, 202)
	requireStatus(t, request(a, "POST", "/api/local-mounts/action", map[string]any{"action": "choose"}), 409)
	req := httptest.NewRequest(http.MethodGet, "/api/local-mounts", nil)
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, req)
	if out.Code == 200 {
		t.Fatal("unauthenticated directory exposure")
	}
}
func TestLocalMountRootsBoundaryAndWorkspaceRights(t *testing.T) {
	a := testApp(t)
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.localMountConfig = localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "archive", Name: "Archive", Path: "E:/Projects", ReadOnly: true}}}
	a.localMountRoots = map[string]*os.Root{"archive": root}
	for _, candidate := range []string{`e:\Projects\子目录`, "/mounts/archive/子目录"} {
		p, _, err := a.resolveHostPath(candidate)
		if err != nil || p != filepath.Join(root.Name(), "子目录") {
			t.Fatalf("%s -> %s %v", candidate, p, err)
		}
	}
	for _, candidate := range []string{"E:/Projects/../Other", "/mounts/archive/../other", "/mounts/unknown"} {
		if _, _, err := a.resolveHostPath(candidate); err == nil {
			t.Fatal("accepted escaped mount", candidate)
		}
	}
	if _, err = a.root("mount:unknown"); err == nil {
		t.Fatal("unknown root accepted")
	}
	r := request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "E:/Projects"}})
	requireStatus(t, r, 400)
	if !strings.Contains(r.Body.String(), "只读") {
		t.Fatal(r.Body.String())
	}
	requireStatus(t, request(a, "GET", "/api/files?root=mount:archive&path=.", nil), 200)
}
