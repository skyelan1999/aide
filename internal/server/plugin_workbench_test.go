package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workbenchFixture(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "workbench-fixture")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "ui.html"), []byte("<!doctype html><title>fixture</title><p>local computation</p>"), 0644)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "workbench-fixture", Name: "Workbench", Enabled: true, Main: "index.js", Workbench: &PluginWorkbench{Protocol: 1, Entry: "ui.html", Title: "Fixture"}})
	return a
}
func workbenchRequest(a *App, method, url string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	if auth {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func workbenchURL(t *testing.T, a *App) string {
	t.Helper()
	w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if !strings.HasPrefix(out.URL, "/plugin-workbench/") {
		t.Fatal(out)
	}
	return out.URL
}
func TestPluginWorkbenchAuthSandboxAndOneUse(t *testing.T) {
	a := workbenchFixture(t)
	if w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/api/plugins/workbench-fixture/workbench/open", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	url := workbenchURL(t, a)
	w = workbenchRequest(a, "GET", url, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "local computation") {
		t.Fatal(w.Code, w.Body.String())
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, part := range []string{"connect-src 'none'", "sandbox allow-scripts allow-downloads", "frame-ancestors 'self'", "worker-src blob:", "media-src blob:"} {
		if !strings.Contains(csp, part) {
			t.Fatal(csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || strings.Contains(w.Body.String(), a.token) {
		t.Fatal("credential or origin leak")
	}
	if w := workbenchRequest(a, "GET", url, false); w.Code != 403 {
		t.Fatal("ticket reusable", w.Code)
	}
}
func TestPluginWorkbenchRevocationChangeAndBound(t *testing.T) {
	for _, kind := range []string{"expiry", "disable", "file", "workspace", "lock"} {
		t.Run(kind, func(t *testing.T) {
			a := workbenchFixture(t)
			url := workbenchURL(t, a)
			key := strings.TrimPrefix(url, "/plugin-workbench/")
			switch kind {
			case "expiry":
				entry := a.pluginWorkbenchTickets[key]
				entry.Expires = time.Now().Add(-time.Minute)
				a.pluginWorkbenchTickets[key] = entry
			case "disable":
				a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
			case "file":
				os.WriteFile(filepath.Join(a.pluginsPath, "workbench-fixture/ui.html"), []byte("changed"), 0644)
			case "workspace":
				entry := a.pluginWorkbenchTickets[key]
				entry.Base = "stale-workspace"
				a.pluginWorkbenchTickets[key] = entry
			case "lock":
				os.MkdirAll(filepath.Dir(lockStatePath(a.dataPath)), 0700)
				os.WriteFile(lockStatePath(a.dataPath), []byte(`{"locked":true,"gen":4}`), 0600)
			}
			if w := workbenchRequest(a, "GET", url, false); w.Code == 200 {
				t.Fatal("revoked content served", kind)
			}
		})
	}
	a := workbenchFixture(t)
	for i := 0; i < 8; i++ {
		workbenchURL(t, a)
	}
	if w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true); w.Code != 429 {
		t.Fatal("ticket budget", w.Code)
	}
}
func TestPluginWorkbenchPackageAndPathValidation(t *testing.T) {
	for _, entry := range []string{"../outside.html", "/etc/file.html", "a/../x.html", "a\\x.html", ".hidden.html", "a.js"} {
		if validatePluginWorkbench(&PluginWorkbench{Protocol: 1, Entry: entry, Title: "Test"}) == nil {
			t.Fatal(entry)
		}
	}
	valid := `{"id":"wb-package","name":"Workbench","main":"index.js","workbench":{"protocol":1,"entry":"ui.html","title":"Tools"}}`
	raw := upgradePackage(t, "wb-package", upgradeOldCode, map[string]string{"manifest.json": valid, "ui.html": "<!doctype html>hello"})
	m, _, e := parsePluginBundle(raw)
	if e != nil || m.Workbench == nil {
		t.Fatal(m, e)
	}
	missing := upgradePackage(t, "wb-package", upgradeOldCode, map[string]string{"manifest.json": valid})
	if _, _, e := parsePluginBundle(missing); e == nil {
		t.Fatal("missing page accepted")
	}
	a := workbenchFixture(t)
	file := filepath.Join(a.pluginsPath, "workbench-fixture/ui.html")
	os.Remove(file)
	os.Symlink(filepath.Join(a.workPath, "outside.html"), file)
	os.WriteFile(filepath.Join(a.workPath, "outside.html"), []byte("secret"), 0644)
	if w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true); w.Code == 200 {
		t.Fatal("symlink accepted")
	}
}

func TestPluginWorkbenchFileExtensions(t *testing.T) {
	base := PluginWorkbench{Protocol: 1, Entry: "workbench.html", Title: "Project", FileExtensions: []string{".pp"}}
	if err := validatePluginWorkbench(&base); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var round PluginWorkbench
	if err = json.Unmarshal(raw, &round); err != nil || len(round.FileExtensions) != 1 || round.FileExtensions[0] != ".pp" {
		t.Fatal("extension lost", err)
	}
	for _, bad := range [][]string{{"pp"}, {".PP"}, {"../pp"}, {".pp", ".pp"}, {".a", ".b", ".c", ".d", ".e", ".f", ".g", ".h", ".i"}} {
		value := base
		value.FileExtensions = bad
		if validatePluginWorkbench(&value) == nil {
			t.Fatal("accepted unsafe extensions", bad)
		}
	}
}
