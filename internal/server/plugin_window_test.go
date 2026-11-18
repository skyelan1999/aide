package server

import (
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func openPluginWindowFixture(t *testing.T, a *App, id string) (bridge string, windows []workbenchWindowCapability) {
	t.Helper()
	prefix := "/api/plugins/" + id + "/workbench/"
	opened := workbenchRequest(a, "POST", prefix+"open", true)
	requireStatus(t, opened, 200)
	var out struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string                      `json:"instanceId"`
			Windows  []workbenchWindowCapability `json:"windows"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &out); err != nil || out.Bridge.Instance == "" {
		t.Fatalf("missing workbench instance: %v", err)
	}
	requireStatus(t, workbenchRequest(a, "GET", out.URL, false), 200)
	return prefix + out.Bridge.Instance, out.Bridge.Windows
}

type workbenchWindowResponse struct {
	Protocol    int    `json:"protocol"`
	URL         string `json:"url"`
	FallbackURL string `json:"fallbackUrl"`
}

func requestPluginWindow(t *testing.T, a *App, bridge, requestID string, arguments map[string]any, status int) workbenchWindowResponse {
	t.Helper()
	response := request(a, "POST", bridge+"/call", map[string]any{"protocol": 1, "requestId": requestID, "method": "window.open", "arguments": arguments})
	requireStatus(t, response, status)
	var reply struct{ Result workbenchWindowResponse }
	if status == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || reply.Result.Protocol != 1 {
			t.Fatalf("invalid window response: %v", err)
		}
	}
	return reply.Result
}

func TestPluginWorkbenchStaticWindowIsScopedAndSingleUse(t *testing.T) {
	a := workbenchFixture(t)
	p := &a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1]
	p.Workbench.Windows = map[string]PluginWorkbenchWindow{
		"report": {Entry: "report.html"},
		"viewer": {Entry: "viewer.html", Kind: "viewer"},
	}
	p.Workbench.Viewer = &PluginWorkbenchViewer{Protocol: 1}
	const report = `<!doctype html><title>Report & diagram</title><script>document.body.dataset.loaded='true'</script>`
	asset := filepath.Join(a.pluginsPath, p.ID, "report.html")
	if err := os.WriteFile(asset, []byte(report), 0600); err != nil {
		t.Fatal(err)
	}
	bridge, windows := openPluginWindowFixture(t, a, p.ID)
	if len(windows) != 1 || windows[0] != (workbenchWindowCapability{ID: "report", Kind: "static"}) {
		t.Fatalf("unexpected grants: %#v", windows)
	}
	requestPluginWindow(t, a, strings.Replace(bridge, p.ID, "terminal", 1), "wrong_plugin", map[string]any{"view": "report"}, 403)
	requestPluginWindow(t, a, bridge, "wrong_adapter", map[string]any{"view": "viewer", "viewerPort": 5901}, 403)
	requestPluginWindow(t, a, bridge, "no_arbitrary_port", map[string]any{"view": "report", "viewerPort": 5901}, 400)
	requestPluginWindow(t, a, bridge, "no_arbitrary_path", map[string]any{"view": "report", "path": "report.html"}, 400)
	args := map[string]any{"view": "report"}
	first := requestPluginWindow(t, a, bridge, "static_view", args, 200)
	if !regexp.MustCompile(`^/plugin-window/[a-f0-9]{64}$`).MatchString(first.URL) || first.FallbackURL != "" {
		t.Fatalf("invalid static response: %#v", first)
	}
	requestPluginWindow(t, a, bridge, "static_view", args, 409)
	requireStatus(t, workbenchRequest(a, "HEAD", first.URL, false), 405)
	requireStatus(t, workbenchRequest(a, "GET", first.URL+"?url=elsewhere", false), 400)
	page := workbenchRequest(a, "GET", first.URL, false)
	requireStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), `sandbox="allow-scripts" srcdoc="`+html.EscapeString(report)+`"`) || strings.Contains(page.Body.String(), a.token) {
		t.Fatal("static window must contain the isolated package asset only")
	}
	policy := page.Header().Get("Content-Security-Policy")
	for _, want := range []string{"connect-src 'none'", "frame-ancestors 'none'", "frame-src 'none'"} {
		if !strings.Contains(policy, want) {
			t.Fatalf("missing safe policy: %s", want)
		}
	}
	if strings.Contains(policy, "sandbox") {
		t.Fatal("trusted window shell must stay same-origin so its owner can close it")
	}
	requireStatus(t, workbenchRequest(a, "GET", first.URL, false), 403)

	changed := requestPluginWindow(t, a, bridge, "changed_view", args, 200)
	if err := os.WriteFile(asset, []byte(report+"!"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, workbenchRequest(a, "GET", changed.URL, false), 403)
	requestPluginWindow(t, a, bridge, "changed_before_open", args, 409)
	if err := os.WriteFile(asset, []byte(report), 0600); err != nil {
		t.Fatal(err)
	}
	revoked := requestPluginWindow(t, a, bridge, "closed_view", args, 200)
	requireStatus(t, request(a, "DELETE", bridge, nil), 200)
	requireStatus(t, workbenchRequest(a, "GET", revoked.URL, false), 403)
}

func TestTerminalWorkbenchWindowUsesExistingViewerAdapter(t *testing.T) {
	const viewer = "<!doctype html><title>Terminal Viewer Fixture</title>"
	a := terminalViewerFixture(t, "<!doctype html><title>Terminal workbench</title>", viewer)
	p := &a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1]
	p.Workbench.Windows = map[string]PluginWorkbenchWindow{"graphics": {Entry: "viewer.html", Kind: "viewer"}}
	bridge, windows := openPluginWindowFixture(t, a, "terminal")
	if len(windows) != 1 || windows[0] != (workbenchWindowCapability{ID: "graphics", Kind: "viewer"}) {
		t.Fatalf("unexpected viewer grants: %#v", windows)
	}
	requestPluginWindow(t, a, bridge, "missing_port", map[string]any{"view": "graphics"}, 400)
	requestPluginWindow(t, a, bridge, "bad_port", map[string]any{"view": "graphics", "viewerPort": 5901.5}, 400)
	out := requestPluginWindow(t, a, bridge, "viewer_window", map[string]any{"view": "graphics", "viewerPort": 5901}, 200)
	if !regexp.MustCompile(`^/plugin-viewer/[a-f0-9]{64}/window$`).MatchString(out.URL) || out.FallbackURL != strings.TrimSuffix(out.URL, "/window")+"/websockify" {
		t.Fatalf("unexpected viewer URLs: %#v", out)
	}
	page := workbenchRequest(a, "GET", out.URL, false)
	requireStatus(t, page, 200)
	if page.Body.String() != viewer {
		t.Fatal("adapter must use the exact existing Terminal viewer asset")
	}
	a.mu.Lock()
	p.Workbench.Windows = nil
	a.mu.Unlock()
	requireStatus(t, workbenchRequest(a, "GET", out.URL, false), 403)
	requireStatus(t, workbenchRequest(a, "GET", out.FallbackURL, false), 403)
	legacyBridge, remaining := openPluginWindowFixture(t, a, "terminal")
	if len(remaining) != 0 {
		t.Fatalf("removed viewer adapter still advertised: %#v", remaining)
	}
	legacy := request(a, "POST", legacyBridge+"/call", map[string]any{"protocol": 1, "requestId": "legacy", "method": "viewer.open", "arguments": map[string]any{"openInWindow": true, "port": 5901}})
	requireStatus(t, legacy, 200)
	if strings.Contains(legacy.Body.String(), "fallbackUrl") {
		t.Fatal("legacy viewer response changed")
	}
}
