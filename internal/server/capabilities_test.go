package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func capabilitySnapshot(t *testing.T, a *App) (string, []CapabilityItem) {
	t.Helper()
	w := request(a, "GET", "/api/capabilities", nil)
	requireStatus(t, w, 200)
	var data struct {
		WorkspaceID string           `json:"workspaceId"`
		Items       []CapabilityItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data.WorkspaceID, data.Items
}
func capabilityFind(t *testing.T, items []CapabilityItem, key string) CapabilityItem {
	t.Helper()
	for _, item := range items {
		if item.Key == key {
			return item
		}
	}
	t.Fatal("missing " + key)
	return CapabilityItem{}
}
func capabilityBody(workspace string, item CapabilityItem) map[string]any {
	return map[string]any{"key": item.Key, "fingerprint": item.Fingerprint, "workspaceId": workspace}
}
func TestCapabilityLocalChecksAndStaleness(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "keep.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, items := capabilitySnapshot(t, a)
	item := capabilityFind(t, items, "workspace")
	if item.Check.State != "not_run" {
		t.Fatal(item)
	}
	w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, item))
	requireStatus(t, w, 200)
	_, items = capabilitySnapshot(t, a)
	checked := capabilityFind(t, items, "workspace")
	if checked.Check.State != "pass" || !strings.Contains(checked.Check.Message, "未检查写入") {
		t.Fatal(checked)
	}
	if strings.Contains(w.Body.String(), "binding") || strings.Contains(w.Body.String(), "Fingerprint") {
		t.Fatal("private binding leaked")
	}
	a.mu.Lock()
	saved := a.capabilityChecks["workspace"]
	saved.At = time.Now().Add(-6 * time.Minute).Format(time.RFC3339Nano)
	a.capabilityChecks["workspace"] = saved
	a.mu.Unlock()
	_, items = capabilitySnapshot(t, a)
	if capabilityFind(t, items, "workspace").Check.State != "stale" {
		t.Fatal("old observation not stale")
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	w = request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, item))
	requireStatus(t, w, 409)
	if b, err := os.ReadFile(filepath.Join(a.workPath, "keep.txt")); err != nil || string(b) != "preserve" {
		t.Fatal("read probe changed file")
	}
	w = request(a, "POST", "/api/capabilities/check", map[string]any{"key": "parser:evil", "workspaceId": workspace})
	requireStatus(t, w, 404)
	recorder := httptest.NewRecorder()
	a.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/capabilities", nil))
	requireStatus(t, recorder, 401)
}
func TestCapabilityPluginAndSourceBoundaries(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "probe", Name: "Probe", Type: "local", Enabled: true, RW: true})
	a.sourceSecrets.Secrets["probe"] = sourceSecretEntry{Password: "do-not-expose"}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "probe", Name: "Probe", Main: "index.js", Enabled: true})
	a.mu.Unlock()
	dir := filepath.Join(a.pluginsPath, "probe")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("throw new Error('must not execute');"), 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.pluginSurface = []byte(`{"plugins":[{"id":"probe","error":"masked-upstream-secret","tools":[]}]}`)
	a.mu.Unlock()
	workspace, items := capabilitySnapshot(t, a)
	if capabilityFind(t, items, "plugin:probe").HostState != "error" {
		t.Fatal("host error metadata absent")
	}
	if strings.Contains(request(a, "GET", "/api/capabilities", nil).Body.String(), "masked-upstream-secret") {
		t.Fatal("raw host error leaked")
	}
	src := capabilityFind(t, items, "source:probe")
	w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, src))
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "未验证写权限") {
		t.Fatal(w.Body.String())
	}
	w = request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, capabilityFind(t, items, "plugin:probe")))
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "未执行插件") {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	a.sourceSecrets.Secrets["probe"] = sourceSecretEntry{Password: "changed"}
	a.mu.Unlock()
	_, items = capabilitySnapshot(t, a)
	if capabilityFind(t, items, "source:probe").Check.State != "stale" {
		t.Fatal("credential change not stale")
	}
	w = request(a, "GET", "/api/capabilities", nil)
	if strings.Contains(w.Body.String(), "do-not-expose") || strings.Contains(w.Body.String(), "changed") {
		t.Fatal("secret leaked")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "probe" {
			a.sourceRegistry.Sources[i].Enabled = false
		}
	}
	a.mu.Unlock()
	workspace, items = capabilitySnapshot(t, a)
	w = request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, capabilityFind(t, items, "source:probe")))
	requireStatus(t, w, 400)
}
func TestCapabilityModelMetadataAndConcurrentChange(t *testing.T) {
	a := testApp(t)
	started, release := make(chan struct{}, 1), make(chan struct{})
	var observedPath string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
		started <- struct{}{}
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture"}]}`))
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings.BaseURL = provider.URL
	a.settings.Model = "fixture"
	a.mu.Unlock()
	workspace, items := capabilitySnapshot(t, a)
	item := capabilityFind(t, items, "model")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, item)) }()
	<-started
	w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, item))
	requireStatus(t, w, 409)
	a.mu.Lock()
	a.settings.Model = "different"
	a.mu.Unlock()
	close(release)
	w = <-done
	requireStatus(t, w, 409)
	if observedPath != "/models" {
		t.Fatal("generated instead of metadata")
	}
	_, items = capabilitySnapshot(t, a)
	if capabilityFind(t, items, "model").Check.State != "not_run" {
		t.Fatal("adopted changed configuration")
	}
	// A stable configuration accepts a real HTTP metadata response, without generating.
	provider2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Error(r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture"}]}`))
	}))
	defer provider2.Close()
	a.mu.Lock()
	a.settings.BaseURL = provider2.URL
	a.mu.Unlock()
	workspace, items = capabilitySnapshot(t, a)
	w = request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, capabilityFind(t, items, "model")))
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "未验证生成") {
		t.Fatal(w.Body.String())
	}
}

func TestCapabilityMCPDiscoveryDoesNotCallTools(t *testing.T) {
	a := testApp(t)
	marker := filepath.Join(t.TempDir(), "tool-called")
	stub := filepath.Join(t.TempDir(), "mcp")
	script := `#!/bin/sh
while IFS= read -r line; do
 case "$line" in
 *'"method":"initialize"'*) echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}}' ;;
 *'"method":"tools/list"'*) echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"fixture_read","annotations":{"readOnlyHint":true}}]}}' ;;
 *'"method":"tools/call"'*) touch "$1"; exit 2 ;;
 esac
done
`
	if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "mcp-probe", Name: "MCP fixture", Type: "mcp", Enabled: true, Config: SourceConfig{Command: stub, Args: []string{marker}, Transport: "stdio"}})
	a.mu.Unlock()
	workspace, items := capabilitySnapshot(t, a)
	w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, capabilityFind(t, items, "source:mcp-probe")))
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "发现 1 个工具") || !strings.Contains(w.Body.String(), "未验证工具执行") {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("tool was called")
	}
}
func TestCapabilityParserDependencyObservations(t *testing.T) {
	a := testApp(t)
	workspace, items := capabilitySnapshot(t, a)
	for _, module := range []string{"pypdf", "docx", "openpyxl", "pptx"} {
		w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, capabilityFind(t, items, "parser:"+module)))
		requireStatus(t, w, 200)
		var result CapabilityCheck
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.State != "pass" && result.State != "failed" {
			t.Fatal(result)
		}
		t.Logf("%s dependency import: %s (%s)", module, result.State, result.Message)
	}
}
