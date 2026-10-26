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

func TestLocalMountRejectsOutsideSymlink(t *testing.T) {
	a := testApp(t)
	mount, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(mount, "escape")); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(mount)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a.localMountConfig = localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "extra", Name: "Extra", Path: "E:/Extra"}}}
	a.localMountRoots = map[string]*os.Root{"extra": r}
	for _, p := range []string{"E:/Extra/escape", "/mounts/extra/escape", "E:/Extra/escape/missing-cache"} {
		if resolved, _, err := a.resolveHostPath(p); err == nil {
			t.Fatalf("resolved outside mount: %s -> %s", p, resolved)
		}
	}
}

func TestLocalMountAnchoredOpeningAndMissingCache(t *testing.T) {
	a := testApp(t)
	mount, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(mount, "inside"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mount, "inside/marker"), []byte("inside"), 0600)
	os.WriteFile(filepath.Join(outside, "marker"), []byte("outside"), 0600)
	if err := os.Symlink("inside", filepath.Join(mount, "link")); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(mount)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a.localMountConfig = localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "extra", Name: "Extra", Path: "E:/Extra"}}}
	a.localMountRoots = map[string]*os.Root{"extra": r}
	cp, _, err := a.resolveHostPath("E:/Extra/link")
	if err != nil {
		t.Fatal(err)
	}
	anchored, err := a.openHostRoot(cp)
	if err != nil {
		t.Fatal(err)
	}
	defer anchored.Close()
	src := Source{}
	src.Config.Path = "E:/Extra/link"
	source, err := a.localSourceRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cache, _, err := a.resolveHostPath("E:/Extra/new/cache")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.mkdirHostPath(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Stat("new/cache"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(mount, "inside"), filepath.Join(mount, "kept")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(mount, "inside")); err != nil {
		t.Fatal(err)
	}
	for _, opened := range []*os.Root{anchored, source} {
		b, e := opened.ReadFile("marker")
		if e != nil || string(b) != "inside" {
			t.Fatalf("root escaped after swap: %s %v", b, e)
		}
	}
	if opened, err := a.openHostRoot(cp); err == nil {
		opened.Close()
		t.Fatal("reopened swapped outside link")
	}
	if err = a.mkdirHostPath(filepath.Join(mount, "inside/missing-cache"), 0700); err == nil {
		t.Fatal("created cache outside root")
	}
	if _, err = os.Stat(filepath.Join(outside, "missing-cache")); !os.IsNotExist(err) {
		t.Fatal("outside cache created")
	}
}

func TestLocalMountWorkspaceKeepsCacheInternal(t *testing.T) {
	a := testApp(t)
	mount := t.TempDir()
	r, err := os.OpenRoot(mount)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a.localMountConfig = localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "extra", Name: "Extra", Path: "E:/Extra"}}}
	a.localMountRoots = map[string]*os.Root{"extra": r}
	a.wsConfig.Workspace.Mode = "local"
	a.wsConfig.Workspace.Path = "E:/Extra"
	if err = a.applyWorkspaceConfig(); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(a.dataPath, "cache", "projects") + string(filepath.Separator)
	if !strings.HasPrefix(a.cacheContainer, expected) {
		t.Fatalf("mount cache is not internally isolated: %s", a.cacheContainer)
	}
	if _, err = os.Stat(filepath.Join(mount, ".cache")); !os.IsNotExist(err) {
		t.Fatal("cache created in mounted directory")
	}
	prior := a.wsRevision
	a.wsConfig.Cache.Path = "E:/Extra/missing-cache"
	if err = a.applyWorkspaceConfig(); err == nil {
		t.Fatal("accepted custom mount cache")
	}
	if a.wsRevision != prior {
		t.Fatal("invalid cache changed workspace before validation")
	}
	for _, p := range []string{"E:/Extra/cache", "/mounts/extra/cache", filepath.Join(mount, "cache")} {
		out := request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "E:/Extra"}, "cache": map[string]any{"path": p}})
		requireStatus(t, out, 400)
		if !strings.Contains(out.Body.String(), "缓存") {
			t.Fatal(out.Body.String())
		}
	}
}

// Release directory selection must feed the same external root into browsing,
// system-document proposals and the final approved write, including Windows
// spelling/Unicode paths while the server itself runs in a Linux container.
func TestLocalMountExternalSystemDocumentsRoundTrip(t *testing.T) {
	a := testApp(t)
	outside := t.TempDir()
	docs := filepath.Join(outside, "资料目录")
	if err := os.Mkdir(docs, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(outside)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a.localMountConfig = localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "external", Name: "External documents", Path: "E:/Projects"}}}
	a.localMountRoots = map[string]*os.Root{"external": root}
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"docs": map[string]any{"location": "local", "path": `e:\Projects\资料目录`},
	}), 200)
	s, task := documentProposalTask(a)
	if _, err := proposeDocument(a, task, nil, "report.md", "external document receipt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(docs, "report.md")); !os.IsNotExist(err) {
		t.Fatal("wrote before approval")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	body, err := os.ReadFile(filepath.Join(docs, "report.md"))
	if err != nil || string(body) != "external document receipt" {
		t.Fatalf("external write: %s %v", body, err)
	}
	if _, err := a.workspace.Stat("report.md"); !os.IsNotExist(err) {
		t.Fatal("document incorrectly written to workspace")
	}
	listed := request(a, "GET", "/api/files?root=context&source=system-docs&path=.", nil)
	requireStatus(t, listed, 200)
	if !strings.Contains(listed.Body.String(), "report.md") {
		t.Fatal(listed.Body.String())
	}
	read := request(a, "GET", "/api/file?root=context&source=system-docs&path=report.md", nil)
	requireStatus(t, read, 200)
	if !strings.Contains(read.Body.String(), "external document receipt") {
		t.Fatal(read.Body.String())
	}
}
