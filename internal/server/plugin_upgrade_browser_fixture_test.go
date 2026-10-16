package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in isolated full-workbench fixture. Public GitHub is the real download
// source; only synthetic plugin data is writable. Publish 18619 to loopback.
func TestPluginUpgradeBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_PLUGIN_UPGRADE_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in isolated browser fixture")
	}
	a := testApp(t)
	const id = "current-time"
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": "升级验收 · 当前时间", "version": "0.9.0", "code": upgradeOldCode}), 201)
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id+"/settings", map[string]any{"settings": map[string]any{"timezone": "Asia/Shanghai", "fixture": "preserve"}}), 200)
	dir := filepath.Join(a.pluginsPath, id, ".data")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"synthetic":"preserve during upgrade"}`)
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "custom-upgrade-fixture", "name": "自定义插件升级验收", "version": "1.0.0", "code": `module.exports={apply(){}}`}), 201)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		_, _ = w.Write([]byte(`<!doctype html><title>插件升级验收</title><script src="/fixture-bootstrap.js" defer></script>`))
	})
	mux.HandleFunc("GET /fixture-bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		token, _ := json.Marshal(a.token)
		_, _ = w.Write(append(append([]byte("localStorage.setItem('aide-token',"), token...), []byte(");location.replace('/');")...))
	})
	mux.HandleFunc("GET /fixture-proof", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		var installed PluginManifest
		for _, p := range a.pluginRegistry.Plugins {
			if p.ID == id {
				installed = p
			}
		}
		a.mu.Unlock()
		data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
		backup, backupErr := os.ReadFile(filepath.Join(a.pluginsPath, installed.UpgradeBackup, "index.js"))
		_, registryErr := os.Stat(filepath.Join(a.pluginsPath, installed.UpgradeBackup, ".aide-upgrade-registry.json"))
		result := map[string]any{"version": installed.Version, "settingsPreserved": installed.Settings["fixture"] == "preserve", "enabled": installed.Enabled, "localDataPreserved": err == nil && bytes.Equal(data, record), "oldCodeBackedUp": backupErr == nil && string(backup) == upgradeOldCode, "registryBackedUp": registryErr == nil, "rememberedSource": installed.UpdateURL, "packageSHA256": installed.PackageSHA256}
		if installed.Version != "0.9.0" {
			tool, err := a.callPluginTool(id, "get_current_datetime", map[string]any{"timezone": "Asia/Shanghai"})
			result["newToolCallable"] = err == nil && tool != nil
		}
		jsonOut(w, 200, result)
	})
	mux.Handle("/", a.Handler())
	t.Log("Isolated plugin upgrade workbench :18619; synthetic files, public GitHub source")
	server := &http.Server{Addr: ":18619", Handler: mux}
	t.Cleanup(func() { _ = server.Close() })
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
