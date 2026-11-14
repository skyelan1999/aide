package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pluginBundleRequest(t *testing.T, a *App, entries map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("bundle", "toolkit.zip")
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	for name, contents := range entries {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/plugins/bundle", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestPluginBundleInstallsAndRunsPythonTool(t *testing.T) {
	a := testApp(t)
	manifest, _ := json.Marshal(PluginManifest{ID: "python-smoke", Name: "Python smoke", Version: "1.0.0", Main: "index.js"})
	entries := map[string]string{
		"python-toolkit/manifest.json": string(manifest),
		"python-toolkit/index.js":      `module.exports = { apply(ctx) { ctx.tool({ name: 'python-echo', description: 'Python echo', parameters: {type:'object',properties:{value:{type:'string'}}}, handler: async (args, api) => ({text: (await api.runPython('worker.py', args)).trim()}) }); } };`,
		"python-toolkit/worker.py":     "import json, sys\nvalue = json.load(sys.stdin)\nprint(json.dumps({'echo': value.get('value', '')}))\n",
		"python-toolkit/README.md":     "example package",
	}
	w := pluginBundleRequest(t, a, entries)
	requireStatus(t, w, 201)
	if !strings.Contains(w.Body.String(), `"id":"python-smoke"`) {
		t.Fatalf("upload result: %s", w.Body.String())
	}
	got, err := a.callPluginTool("python-smoke", "python-echo", map[string]any{"value": "ready"})
	if err != nil {
		t.Fatalf("Python tool failed: %v", err)
	}
	if !strings.Contains(gotText(got), `\"echo\": \"ready\"`) && !strings.Contains(gotText(got), `\"echo\":\"ready\"`) {
		t.Fatalf("unexpected Python output: %s", gotText(got))
	}
}

func TestPluginBundleValidatesCompletePackageAtDeclaredEntry(t *testing.T) {
	a := testApp(t)
	w := pluginBundleRequest(t, a, map[string]string{
		"pkg/manifest.json": `{"id":"bundle-relative","name":"Relative imports","version":"1.0.0","main":"src/index.js"}`,
		"pkg/src/index.js":  `const tool = require('./tool.js'); const schema = require('../schema.json'); const fs = require('fs'); const path = require('path'); const manifest = JSON.parse(fs.readFileSync(path.join(__dirname,'../manifest.json'),'utf8')); module.exports={name:manifest.name,apply(ctx){ctx.tool({name:'relative-echo',description:schema.description,parameters:{type:'object'},handler:()=>tool.value});}};`,
		"pkg/src/tool.js":   `module.exports={value:'relative dependency result'};`,
		"pkg/schema.json":   `{"description":"Synthetic dependency"}`,
	})
	requireStatus(t, w, 201)
	got, err := a.callPluginTool("bundle-relative", "relative-echo", map[string]any{})
	if err != nil || !strings.Contains(gotText(got), "relative dependency result") {
		t.Fatalf("installed package tool: %s, %v", gotText(got), err)
	}
}

func TestPluginBundleMissingDependencyLeavesNoPartialInstall(t *testing.T) {
	a := testApp(t)
	before := append([]PluginManifest{}, a.pluginRegistry.Plugins...)
	w := pluginBundleRequest(t, a, map[string]string{
		"manifest.json": `{"id":"missing-dependency","name":"Missing dependency","main":"src/index.js"}`,
		"src/index.js":  `require('./missing.json'); module.exports={apply(ctx){}};`,
	})
	requireStatus(t, w, 400)
	if !reflect.DeepEqual(before, append([]PluginManifest{}, a.pluginRegistry.Plugins...)) {
		t.Fatal("invalid package changed registry")
	}
	entries, err := os.ReadDir(a.pluginsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "missing-dependency" || strings.HasPrefix(entry.Name(), ".install-") {
			t.Fatalf("invalid package left partial install: %s", filepath.Join(a.pluginsPath, entry.Name()))
		}
	}
}

func TestPluginBundleRejectsTraversalWithoutPartialInstall(t *testing.T) {
	a := testApp(t)
	registeredBefore := append([]PluginManifest(nil), a.pluginRegistry.Plugins...)
	manifest, _ := json.Marshal(PluginManifest{ID: "bad-bundle", Name: "Bad bundle", Main: "index.js"})
	w := pluginBundleRequest(t, a, map[string]string{
		"pkg/manifest.json": string(manifest),
		"pkg/index.js":      validPlugin,
		"pkg/../escape.py":  "print('no')",
	})
	requireStatus(t, w, 400)
	registeredAfter := append([]PluginManifest(nil), a.pluginRegistry.Plugins...)
	if !reflect.DeepEqual(registeredAfter, registeredBefore) {
		t.Fatalf("rejected bundle was registered: %+v", a.pluginRegistry.Plugins)
	}
	if strings.Contains(w.Body.String(), "escape.py") {
		t.Fatalf("unexpected path detail: %s", w.Body.String())
	}
}

func TestPluginSettingsCanBeSavedForBundle(t *testing.T) {
	a := testApp(t)
	manifest, _ := json.Marshal(PluginManifest{ID: "settings-smoke", Name: "Settings smoke", Main: "index.js"})
	w := pluginBundleRequest(t, a, map[string]string{
		"pkg/manifest.json": string(manifest),
		"pkg/index.js":      validPlugin,
	})
	requireStatus(t, w, 201)

	w = request(a, "PUT", "/api/plugins/settings-smoke/settings", map[string]any{"settings": map[string]any{"mode": "quiet"}})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"mode":"quiet"`) {
		t.Fatalf("settings were not saved: %s", w.Body.String())
	}
}

func gotText(v any) string { b, _ := json.Marshal(v); return string(b) }
