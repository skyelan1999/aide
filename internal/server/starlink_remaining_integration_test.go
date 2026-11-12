package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opt in with the separately reviewed plugin source. No installed/user plugin
// or published ZIP is modified; this is a real temporary host + Node IPC test.
func TestRemainingStarLinkDaemon(t *testing.T) {
	source := os.Getenv("AIDE_STARLINK_SOURCE")
	if source == "" {
		t.Skip("separately checked-out StarLink source required")
	}
	a := sourceActionFixture(t)
	a.sourceRegistry.Sources = a.sourceRegistry.Sources[1:]
	directory, err := os.MkdirTemp("/workspace", "remaining-starlink-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	a.workPath = directory
	a.workspace = root
	a.wsConfig.Workspace.Mode = "local"
	a.wsConfig.Workspace.Path = directory
	a.wsRoots[a.wsID()] = root
	if err := root.WriteFile("unicode.md", []byte("# Straße café ΟΣ ﬃ\n\noriginal cafe\u0301 and Straße"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"worker.ts":     "export function StarLinkTypeScriptWorker(value:number):number{return value+1;}",
		"caller.ts":     "import {StarLinkTypeScriptWorker} from './worker'; export function StarLinkTypeScriptCaller(value:number):number{return StarLinkTypeScriptWorker(value);}",
		"component.tsx": "export function TypedComponent(){return <span>fixture</span>;}",
	} {
		if err := root.WriteFile(name, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plugin := filepath.Join(a.pluginsPath, "starlink")
	os.MkdirAll(plugin, 0700)
	var manifest PluginManifest
	for _, name := range []string{"manifest.json", "index.js"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(plugin, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if name == "manifest.json" {
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	manifest.Enabled = true
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	cert, key, err := ensureTLSCert(filepath.Join(a.dataPath, "certs"))
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(a.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()
	os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700)
	os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600)
	a.runPluginHost(context.Background())
	releasePublicSettings(t, a, "starlink", map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "dataDir": a.dataPath})
	call := func(query string) map[string]any {
		return releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_search", map[string]any{"query": query}))
	}
	for _, query := range []string{"Straße", "STRASSE", "café", "cafe\u0301", "οσ", "FFI"} {
		if call(query)["totalMatches"].(float64) < 1 {
			t.Fatalf("Unicode query missed %q", query)
		}
	}
	ids := map[string]string{}
	for _, name := range []string{"StarLinkTypeScriptWorker", "StarLinkTypeScriptCaller", "TypedComponent"} {
		result := releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_search", map[string]any{"query": name, "kind": "symbol"}))
		for _, entry := range result["results"].([]any) {
			node := entry.(map[string]any)["node"].(map[string]any)
			if node["name"] == name {
				ids[name] = node["id"].(string)
			}
		}
		if ids[name] == "" {
			t.Fatalf("typed symbol not indexed via daemon: %s %+v", name, result)
		}
	}
	path := releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_path", map[string]any{"from": ids["StarLinkTypeScriptCaller"], "to": ids["StarLinkTypeScriptWorker"]}))
	if path["found"] != true {
		t.Fatalf("typed candidate path missing via actual daemon: %+v", path)
	}
	if call("reference bytes")["totalMatches"].(float64) < 1 {
		t.Fatal("visible reference fixture missing")
	}
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": "readonly-actions", "workspaceId": a.wsID(), "visible": false}), 200)
	if got := call("reference bytes"); got["totalMatches"].(float64) != 0 {
		t.Fatalf("daemon cache retained hidden source: %+v", got)
	}
	if body := request(a, "GET", "/api/knowledge-map/updates", nil).Body.String(); !strings.Contains(body, "reference bytes") {
		t.Fatal("human graph no longer sees hidden source")
	}
}
