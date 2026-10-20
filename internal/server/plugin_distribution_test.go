package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentPluginsDoNotInstallFromCheckoutOrLegacyImage(t *testing.T) {
	work := t.TempDir()
	legacy := filepath.Join(work, "bundled-plugins")
	if err := os.MkdirAll(filepath.Join(legacy, "example"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "registry.json"), []byte(`{"version":1,"plugins":[{"id":"example","main":"index.js"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_BUILTIN_PLUGINS", legacy)
	a := &App{workPath: work}
	for i := 0; i < 2; i++ {
		if err := a.loadPlugins(); err != nil {
			t.Fatal(err)
		}
		if len(a.pluginRegistry.Plugins) != 0 {
			t.Fatal("host silently installed a plugin")
		}
		if _, err := os.Stat(filepath.Join(work, "plugins", "registry.json")); !os.IsNotExist(err) {
			t.Fatalf("fresh host wrote installed state: %v", err)
		}
	}
}

func TestIndependentPluginsPreserveInstalledStateAcrossReload(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "plugins", "custom")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	code := filepath.Join(dir, "index.js")
	if err := os.WriteFile(code, []byte("// user-installed revision"), 0644); err != nil {
		t.Fatal(err)
	}
	registry := pluginRegistry{Version: 1, Plugins: []PluginManifest{{ID: "custom", Main: "index.js", Version: "9.0.0", Enabled: false, Settings: map[string]any{"user": "keep"}}}, RemovedBuiltins: []string{"browser-control"}}
	registryPath := filepath.Join(work, "plugins", "registry.json")
	if err := atomicJSON(registryPath, registry); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(registryPath)
	a := &App{workPath: work}
	for i := 0; i < 2; i++ {
		if err := a.loadPlugins(); err != nil {
			t.Fatal(err)
		}
		if len(a.pluginRegistry.Plugins) != 1 || a.pluginRegistry.Plugins[0].Version != "9.0.0" || a.pluginRegistry.Plugins[0].Settings["user"] != "keep" || a.pluginRegistry.Plugins[0].Enabled {
			t.Fatal("installed state changed")
		}
	}
	after, _ := os.ReadFile(registryPath)
	if string(before) != string(after) {
		t.Fatal("host rewrote registry")
	}
	data, _ := os.ReadFile(code)
	if string(data) != "// user-installed revision" {
		t.Fatal("host replaced installed code")
	}
}
