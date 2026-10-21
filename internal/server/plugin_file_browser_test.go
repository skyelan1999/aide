package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPluginFilePreviewBrowser(t *testing.T) {
	if os.Getenv("AIDE_PLUGIN_FILE_BROWSER") != "1" {
		t.Skip("isolated opt-in browser")
	}
	a := testApp(t)
	root, err := filepath.Abs("../../examples/file-preview-plugin")
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, name := range []string{"manifest.json", "index.js", "viewer.html"} {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil {
			t.Fatal(e)
		}
		entries[name] = string(b)
	}
	entries["assets/empty.wasm"] = string([]byte{0, 97, 115, 109, 1, 0, 0, 0})
	var manifest map[string]any
	json.Unmarshal([]byte(entries["manifest.json"]), &manifest)
	manifest["workbench"].(map[string]any)["fileHandler"].(map[string]any)["extensions"] = []string{".obj", ".stl"}
	encoded, _ := json.Marshal(manifest)
	entries["manifest.json"] = string(encoded)
	requireStatus(t, pluginBundleRequest(t, a, entries), 201)
	cube, err := os.ReadFile(filepath.Join(root, "cube.obj"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(a.workPath, "cube.obj"), cube, 0600)
	os.WriteFile(filepath.Join(a.reference.Name(), "cube.obj"), cube, 0600)
	stl := []byte("solid demo\n facet normal 0 0 1\n outer loop\n vertex 0 0 0\n vertex 1 0 0\n vertex 0 1 0\n endloop\n endfacet\nendsolid demo\n")
	os.WriteFile(filepath.Join(a.workPath, "triangle.stl"), stl, 0600)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "preview-ro", Name: "只读预览", Type: "local", Enabled: true, RW: false, Config: SourceConfig{Path: "/context"}}, Source{ID: "preview-rw", Name: "可写预览", Type: "local", Enabled: true, RW: true, Config: SourceConfig{Path: "/context"}})
	if e := a.saveSources(); e != nil {
		t.Fatal(e)
	}
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-plugin-file-preview.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath, "AIDE_TEST_WORK="+a.workPath, "AIDE_TEST_REFERENCE="+a.reference.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}
