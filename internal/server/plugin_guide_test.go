package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginGuideInstalledDocumentationAndCapabilities(t *testing.T) {
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "guide-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Installed guide\n<script>unsafe()</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "guide-fixture", Name: "Fixture", Version: "1.0.0", Enabled: false, Settings: map[string]any{"secret": "must-not-return"}, ProviderSupport: &PluginProviderSupport{Protocol: 1, Catalog: "private.json"}})
	a.pluginSurface = []byte(`{"plugins":[{"id":"guide-fixture","tools":[{"name":"declaration","executable":false},{"name":"actual","description":"Useful tool","executable":true,"schema":{"secret":"must-not-return"}}]}]}`)
	a.mu.Unlock()
	w := request(a, "GET", "/api/plugins/guide-fixture/guide", nil)
	requireStatus(t, w, 200)
	var got struct {
		Enabled         bool   `json:"enabled"`
		ProviderSupport bool   `json:"providerSupport"`
		Readme          string `json:"readme"`
		Tools           []struct {
			Executable bool `json:"executable"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.ProviderSupport || !strings.Contains(got.Readme, "Installed guide") || len(got.Tools) != 2 || got.Tools[0].Executable || !got.Tools[1].Executable {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "must-not-return") || strings.Contains(w.Body.String(), "private.json") {
		t.Fatal("settings/schema/catalog leaked")
	}
	if strings.Contains(w.Body.String(), "<script>") {
		t.Fatal("JSON must escape HTML")
	}
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/plugins/guide-fixture/guide", nil), 423)
}

func TestPluginGuideBoundsAndConfinedReadme(t *testing.T) {
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "guide-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "guide-fixture", Name: "Fixture"})
	secret := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(secret, []byte("outside-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/plugins/guide-fixture/guide", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "outside-secret") {
		t.Fatal("escaped plugin directory")
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(strings.Repeat("文", 50000)), 0600); err != nil {
		t.Fatal(err)
	}
	w = request(a, "GET", "/api/plugins/guide-fixture/guide", nil)
	requireStatus(t, w, 200)
	var got struct {
		Readme    string `json:"readme"`
		Truncated bool   `json:"readmeTruncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Readme) > 128<<10+3 {
		t.Fatal("unbounded README")
	}
	requireStatus(t, request(a, "GET", "/api/plugins/not-installed/guide", nil), 404)
}
