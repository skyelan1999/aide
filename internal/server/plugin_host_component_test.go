package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hostComponentTransport func(*http.Request) (*http.Response, error)

func (f hostComponentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHostComponentProjectionAndRoutes(t *testing.T) {
	c := hostComponentConnection{Token: strings.Repeat("a", 64), Port: 17888}
	for _, action := range []string{"status", "install", "start", "stop", "uninstall", "key", "usage", "models"} {
		t.Run(action, func(t *testing.T) {
			client := &http.Client{Transport: hostComponentTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer "+c.Token || r.URL.Port() != "17888" {
					t.Fatal("wrong credential or port")
				}
				route := "/api/" + action
				if action == "install" || action == "start" || action == "stop" || action == "uninstall" {
					route = "/api/action"
				}
				if r.URL.Path != route {
					t.Fatal(r.URL.Path)
				}
				payload := `{"service":"aide-codex-bridge-manager","installed":true,"running":true,"token":"must-not-leak","bridgeKey":"` + strings.Repeat("b", 64) + `","usage":{"buckets":[],"checkedAt":"now","private":"must-not-leak"},"models":[{"id":"synthetic"}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: http.Header{}}, nil
			})}
			result, e := hostComponentRequest(context.Background(), client, c, action)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "must-not-leak") || (action != "key" && strings.Contains(string(raw), strings.Repeat("b", 64))) {
				t.Fatal("secret leaked")
			}
			if action == "status" && result["phase"] != "ready" {
				t.Fatal(result)
			}
		})
	}
	client := &http.Client{Transport: hostComponentTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported request reached network")
		return nil, nil
	})}
	for _, action := range []string{"exec", "provider", "quit", "../key"} {
		if _, e := hostComponentRequest(context.Background(), client, c, action); e == nil {
			t.Fatal("accepted", action)
		}
	}
}
func TestHostComponentMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "host-components")
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "codex-bridge.json")
	for _, raw := range []string{`{}`, `{"protocol":1,"service":"codex-bridge","port":1}`, strings.Repeat("x", 4097)} {
		os.WriteFile(file, []byte(raw), 0600)
		if _, e := readHostComponentConnection(root); e == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	os.WriteFile(file, []byte(`{"protocol":1,"service":"codex-bridge","port":17888,"token":"`+strings.Repeat("a", 64)+`"}`), 0600)
	if _, e := readHostComponentConnection(root); e != nil {
		t.Fatal(e)
	}
	os.Rename(file, file+".real")
	os.Symlink(file+".real", file)
	if _, e := readHostComponentConnection(root); e == nil {
		t.Fatal("symlink accepted")
	}
}
func hostWorkbenchFixture(t *testing.T) (*App, string) {
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "codex-bridge")
	os.MkdirAll(dir, 0755)
	page := []byte("<!doctype html><p>fixture</p>")
	os.WriteFile(filepath.Join(dir, "ui.html"), page, 0644)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "codex-bridge", Name: "Codex", Enabled: true, Main: "index.js", Workbench: &PluginWorkbench{Protocol: 1, Entry: "ui.html", Title: "Bridge", HostComponent: "codex-bridge"}})
	key := strings.Repeat("f", 64)
	ticket := pluginWorkbenchTicket{ID: "codex-bridge", Base: a.managementBaseLocked("plugins", "codex-bridge"), Gen: a.loadLockStateLocked().Gen, Digest: sha256.Sum256(page)}
	a.pluginWorkbenchBridges = map[string]*pluginWorkbenchBridge{key: {Ticket: ticket, Package: a.pluginPackageDigestLocked("codex-bridge"), Revision: a.wsRevision, Ready: true, Expires: time.Now().Add(time.Minute), Seen: map[string]bool{}}}
	return a, key
}
func TestHostComponentScopeReplayAndMissingHost(t *testing.T) {
	a, key := hostWorkbenchFixture(t)
	call := func(id, action string) *httptest.ResponseRecorder {
		return request(a, "POST", "/api/plugins/codex-bridge/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": "host.component", "arguments": map[string]any{"action": action}})
	}
	w := call("r1", "status")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, call("r1", "status"), 403)
	requireStatus(t, call("r2", "exec"), 400)
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "codex-bridge" {
			a.pluginRegistry.Plugins[i].Workbench.HostComponent = ""
		}
	}
	requireStatus(t, call("r3", "status"), 403)
}

func TestHostComponentQuotaProjection(t *testing.T) {
	for _, test := range []struct {
		used, remaining float64
		available       bool
	}{{25, 75, true}, {125, 0, true}, {-1, 0, false}} {
		client := &http.Client{Transport: hostComponentTransport(func(*http.Request) (*http.Response, error) {
			raw, _ := json.Marshal(map[string]any{"service": "aide-codex-bridge-manager", "usage": map[string]any{"buckets": []any{map[string]any{"primary": map[string]any{"usedPercent": test.used, "remainingPercent": 999, "secret": "hidden"}}}}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
		})}
		got, err := hostComponentRequest(context.Background(), client, hostComponentConnection{}, "usage")
		if err != nil || got["available"] != test.available {
			t.Fatalf("quota: %v %v", got, err)
		}
		if test.available {
			w := got["buckets"].([]any)[0].(map[string]any)["primary"].(map[string]any)
			if w["remainingPercent"] != test.remaining || w["secret"] != nil {
				t.Fatal(w)
			}
		}
	}
}
