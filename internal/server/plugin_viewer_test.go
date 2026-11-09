package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func viewerFixture(t *testing.T, viewer bool) (*App, string) {
	t.Helper()
	a := workbenchFixture(t)
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
			if viewer {
				a.pluginRegistry.Plugins[i].Workbench.Viewer = &PluginWorkbenchViewer{Protocol: 1}
			} else {
				// Keep a bridge without the viewer capability to prove the denial.
				a.pluginRegistry.Plugins[i].Workbench.Tools = []string{"workbench_noop"}
			}
		}
	}
	w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string `json:"instanceId"`
			Viewer   bool   `json:"viewer"`
		} `json:"bridge"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Bridge.Instance == "" || out.Bridge.Viewer != viewer {
		t.Fatal(w.Body.String())
	}
	if w = workbenchRequest(a, "GET", out.URL, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return a, out.Bridge.Instance
}

func TestWorkbenchViewerCSPStaysScopedToHostOrigin(t *testing.T) {
	a, _ := viewerFixture(t, true)
	w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	var out struct {
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	page := workbenchRequest(a, "GET", out.URL, false)
	csp := page.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"connect-src http://example.com ws://example.com wss://example.com",
		"frame-src 'none'",
		"sandbox allow-scripts allow-downloads",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("missing %q in %s", want, csp)
		}
	}
	for _, forbidden := range []string{"connect-src *", "frame-src *", "allow-same-origin"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("over-broad viewer CSP %q: %s", forbidden, csp)
		}
	}
	if pp := page.Header().Get("Permissions-Policy"); !strings.Contains(pp, "clipboard-write=()") || strings.Contains(pp, "clipboard-write=*") {
		t.Fatal(pp)
	}
	if strings.Contains(page.Body.String(), a.token) {
		t.Fatal("workbench leaked the host token")
	}
}

func TestWorkbenchViewerTicketRequiresDeclaredCapability(t *testing.T) {
	a, key := viewerFixture(t, true)
	w := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "v1", "arguments": map[string]any{"port": 5901}})
	requireStatus(t, w, 200)
	var out struct {
		Result struct {
			URL string `json:"url"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.Result.URL, "/plugin-viewer/") || !strings.HasSuffix(out.Result.URL, "/websockify") {
		t.Fatal(w.Body.String())
	}
	if got := workbenchRequest(a, "GET", out.Result.URL, false); got.Code != 400 {
		t.Fatalf("non-upgrade viewer request accepted: %d", got.Code)
	}
	requireStatus(t, request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "v1", "arguments": map[string]any{"port": 5901}}), 409)
	requireStatus(t, request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "bad-port", "arguments": map[string]any{"port": 80}}), 400)

	plain, key := viewerFixture(t, false)
	requireStatus(t, request(plain, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "v2", "arguments": map[string]any{"port": 5901}}), 403)
}
