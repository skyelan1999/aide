package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckoutPluginSeedPreservesInstalledState(t *testing.T) {
	work := t.TempDir()
	bundle := filepath.Join(work, "bundled-plugins")
	for _, id := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(bundle, id), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bundle, id, "index.js"), []byte("bundled"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	raw := []byte(`{"version":1,"plugins":[{"id":"one"},{"id":"two"}]}`)
	if err := os.WriteFile(filepath.Join(bundle, "registry.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	a := &App{workPath: work, pluginsPath: filepath.Join(work, "plugins")}
	if err := os.MkdirAll(filepath.Join(a.pluginsPath, "one"), 0755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(a.pluginsPath, "one", "index.js")
	if err := os.WriteFile(installed, []byte("upgraded user plugin"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.seedCheckoutPlugins(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(installed)
	if string(b) != "upgraded user plugin" {
		t.Fatal("overwrote installed plugin")
	}
	b, _ = os.ReadFile(filepath.Join(a.pluginsPath, "two", "index.js"))
	if string(b) != "bundled" {
		t.Fatal("missing fresh seed")
	}
	registry := filepath.Join(a.pluginsPath, "registry.json")
	if err := os.WriteFile(registry, []byte(`{"version":1,"plugins":[],"removedBuiltins":["two"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(a.pluginsPath, "two")); err != nil {
		t.Fatal(err)
	}
	if err := a.seedCheckoutPlugins(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.pluginsPath, "two")); !os.IsNotExist(err) {
		t.Fatal("deleted plugin resurrected")
	}
	b, _ = os.ReadFile(registry)
	if string(b) == string(raw) {
		t.Fatal("overwrote user registry")
	}
}
