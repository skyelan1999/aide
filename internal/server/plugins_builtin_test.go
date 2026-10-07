package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledControlPluginsUpgradeExistingWorkspace(t *testing.T) {
	bundle, err := filepath.Abs("../../plugins")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_BUILTIN_PLUGINS", bundle)
	root := t.TempDir()
	work := filepath.Join(root, "work")
	pluginDir := filepath.Join(work, "plugins")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	old := pluginRegistry{Version: 1, Plugins: []PluginManifest{{ID: "user-tool", Name: "User tool", Main: "index.js", Enabled: false, Settings: map[string]any{"mode": "custom"}}}}
	if err := atomicJSON(filepath.Join(pluginDir, "registry.json"), old); err != nil {
		t.Fatal(err)
	}
	a, err := New(work, filepath.Join(root, "ref"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	w := request(a, "GET", "/api/plugins", nil)
	requireStatus(t, w, 200)
	for _, id := range []string{"user-tool", "browser-control", "computer-control"} {
		if !strings.Contains(w.Body.String(), `"id":"`+id+`"`) {
			t.Fatalf("missing plugin %s: %s", id, w.Body.String())
		}
	}
	if len(a.pluginRegistry.Plugins) != 3 {
		t.Fatalf("registry size: %d", len(a.pluginRegistry.Plugins))
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled {
			t.Fatalf("upgrade unexpectedly enabled %s", p.ID)
		}
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/browser-control/settings", map[string]any{"settings": map[string]any{"allowedHosts": []string{"example.com"}}}), 200)
	requireStatus(t, request(a, "PUT", "/api/plugins/browser-control", map[string]any{"enabled": true}), 200)
	w = request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"browser_navigate"`) || !strings.Contains(w.Body.String(), `"name":"browser_snapshot"`) {
		t.Fatalf("browser tools did not reach model surface: %s", w.Body.String())
	}
	if len(a.pluginToolSchemasForOwner("browser-control")) != 7 {
		t.Fatal("browser tool schemas did not reach provider tool list")
	}
	codePath := filepath.Join(pluginDir, "browser-control", "index.js")
	if err := os.WriteFile(codePath, []byte("// user customization"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	if len(a.pluginRegistry.Plugins) != 3 || !a.pluginRegistry.Plugins[1].Enabled || a.pluginRegistry.Plugins[1].Settings["allowedHosts"] == nil {
		t.Fatalf("reload replaced registry state: %+v", a.pluginRegistry)
	}
	code, _ := os.ReadFile(codePath)
	if string(code) != "// user customization" {
		t.Fatal("upgrade overwrote workspace plugin code")
	}
	var persisted pluginRegistry
	registryBytes, err := os.ReadFile(filepath.Join(pluginDir, "registry.json"))
	if err != nil || json.Unmarshal(registryBytes, &persisted) != nil || len(persisted.Plugins) != 3 {
		t.Fatal("installed plugin registry was not persisted")
	}
	requireStatus(t, request(a, "DELETE", "/api/plugins/computer-control", nil), 200)
	if err := a.loadPlugins(); err != nil || len(a.pluginRegistry.Plugins) != 2 {
		t.Fatalf("startup resurrected an explicitly removed builtin: %v", err)
	}
}

func TestBundledControlPluginsRegisterInEmptyWorkspace(t *testing.T) {
	bundle, _ := filepath.Abs("../../plugins")
	t.Setenv("AIDE_BUILTIN_PLUGINS", bundle)
	a := &App{workPath: t.TempDir()}
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	if len(a.pluginRegistry.Plugins) != 2 {
		t.Fatalf("expected two bundled control plugins, got %d", len(a.pluginRegistry.Plugins))
	}
	if err := a.loadPlugins(); err != nil || len(a.pluginRegistry.Plugins) != 2 {
		t.Fatalf("repeat startup duplicated registration: %v", err)
	}
}

func TestBundledControlPluginsRegisterExistingFilesWithoutReplacement(t *testing.T) {
	bundle, _ := filepath.Abs("../../plugins")
	t.Setenv("AIDE_BUILTIN_PLUGINS", bundle)
	work := t.TempDir()
	dir := filepath.Join(work, "plugins", "browser-control")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{ID: "browser-control", Name: "Customized browser", Main: "index.js", Enabled: true, Settings: map[string]any{"custom": "preserved"}}
	if err := atomicJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("// preserve existing"), 0644); err != nil {
		t.Fatal(err)
	}
	a := &App{workPath: work}
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	code, _ := os.ReadFile(filepath.Join(dir, "index.js"))
	if string(code) != "// preserve existing" || a.pluginRegistry.Plugins[0].Name != "Customized browser" || a.pluginRegistry.Plugins[0].Enabled || a.pluginRegistry.Plugins[0].Settings["custom"] != "preserved" {
		t.Fatalf("registration replaced existing contents or auto-enabled control: %+v", a.pluginRegistry)
	}
}
