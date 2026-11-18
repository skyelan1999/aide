package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredOfficePluginLeavesInstalledDataAndNativeTools(t *testing.T) {
	a := testApp(t)
	legacy := PluginManifest{ID: "office", Name: "Office", Version: "1.3.0", Main: "index.js", Enabled: true, Settings: map[string]any{"saved": "preserved"}}
	a.pluginRegistry.Plugins = []PluginManifest{legacy}
	if err := a.savePluginRegistry(); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(a.pluginsPath, "registry.json")
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.pluginsPath, "office", "user-data")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	a.runPluginHost(context.Background())
	if a.pluginRegistry.Plugins[0].Enabled || a.pluginOwnerOf("office_document_read") != "" {
		t.Fatal("legacy plugin still active")
	}
	for _, name := range []string{"office_create", "office_comments", "office_comment_edit", "office_document_search", "office_document_read", "office_document_edit"} {
		count := 0
		for _, candidate := range builtinToolNames() {
			if candidate == name {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("want exactly one native tool %s, got %d", name, count)
		}
	}
	for _, endpoint := range []struct{ method, route string }{
		{"PUT", "/api/plugins/office"},
		{"PUT", "/api/plugins/office/settings"},
		{"POST", "/api/plugins/office/upgrade"},
		{"GET", "/api/plugin-market/office/download"},
		{"POST", "/api/plugin-market/office/install"},
	} {
		requireStatus(t, request(a, endpoint.method, endpoint.route, map[string]any{"enabled": true}), http.StatusGone)
	}
	bundleManifest, _ := json.Marshal(legacy)
	upload := pluginBundleRequest(t, a, map[string]string{"manifest.json": string(bundleManifest), "index.js": "module.exports = {apply(){}};"})
	requireStatus(t, upload, http.StatusGone)
	requireStatus(t, request(a, "POST", "/api/office/collabora/sessions", nil), http.StatusNotFound)
	requireStatus(t, request(a, "GET", "/office-engine/browser/example", nil), http.StatusNotFound)
	requireStatus(t, request(a, "GET", "/wopi/files/example", nil), http.StatusNotFound)
	after, err := os.ReadFile(registry)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retirement rewrote persisted registry", err)
	}
	if kept, err := os.ReadFile(file); err != nil || string(kept) != "retained" {
		t.Fatal("retirement removed user plugin data", err)
	}
}
