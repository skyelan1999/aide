package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Isolated manager fixture; no installed user plugin is upgraded by this test.
func TestPluginUpgradeButtonsBrowser(t *testing.T) {
	if os.Getenv("AIDE_PLUGIN_BUTTON_BROWSER") != "1" {
		t.Skip("opt-in browser regression")
	}
	out := os.Getenv("AIDE_EVIDENCE_DIR")
	if out == "" {
		t.Fatal("set AIDE_EVIDENCE_DIR")
	}
	a := testApp(t)
	for _, id := range []string{"upgrade-a", "upgrade-b"} {
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": "Upgrade fixture " + id, "version": "1.0.0", "code": "module.exports={apply(){}}"}), 201)
	}
	a.mu.Lock()
	for i := range a.pluginRegistry.Plugins {
		a.pluginRegistry.Plugins[i].UpdateURL = "https://example.com/fixture.zip"
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
	script, err := filepath.Abs("../../scripts/browser-plugin-upgrade-state.cjs")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin upgrade browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
