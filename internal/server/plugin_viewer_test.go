package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
	requireStatus(t, request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "v3", "arguments": map[string]any{"port": 5901, "openInWindow": true}}), 403)

	plain, key := viewerFixture(t, false)
	requireStatus(t, request(plain, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "viewer.open", "requestId": "v2", "arguments": map[string]any{"port": 5901}}), 403)
}

type viewerOpenFixture struct {
	URL    string `json:"url"`
	Bridge struct {
		Instance string `json:"instanceId"`
		Window   bool   `json:"viewerWindow"`
	} `json:"bridge"`
}

func terminalViewerFixture(t *testing.T, workbench, viewer string) *App {
	t.Helper()
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "terminal")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"workbench.html": workbench, "viewer.html": viewer} {
		if body != "" {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "terminal", Name: "Terminal", Enabled: true, Main: "index.js", Workbench: &PluginWorkbench{Protocol: 1, Viewer: &PluginWorkbenchViewer{Protocol: 1}, Entry: "workbench.html", Title: "Terminal"}})
	return a
}

func openTerminalViewerFixture(t *testing.T, a *App) viewerOpenFixture {
	t.Helper()
	var open viewerOpenFixture
	w := workbenchRequest(a, "POST", "/api/plugins/terminal/workbench/open", true)
	requireStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &open); err != nil || open.Bridge.Instance == "" {
		t.Fatal(w.Body.String(), err)
	}
	requireStatus(t, workbenchRequest(a, "GET", open.URL, false), 200)
	return open
}

func TestTerminalViewerDetachedRouteAndRevocation(t *testing.T) {
	const body = "<!doctype html><title>Detached fixture</title>"
	a := terminalViewerFixture(t, "<!doctype html><title>Terminal</title>", body)
	open := openTerminalViewerFixture(t, a)
	if !open.Bridge.Window {
		t.Fatal("packaged Terminal window not advertised")
	}
	bridge := "/api/plugins/terminal/workbench/" + open.Bridge.Instance
	mint := func(id string, window any, port any, status int) string {
		t.Helper()
		w := request(a, "POST", bridge+"/call", map[string]any{"protocol": 1, "requestId": id, "method": "viewer.open", "arguments": map[string]any{"port": port, "openInWindow": window}})
		requireStatus(t, w, status)
		if status != 200 {
			return ""
		}
		var reply struct{ Result struct{ URL string } }
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || !strings.HasSuffix(reply.Result.URL, "/websockify") {
			t.Fatal(w.Body.String(), err)
		}
		return strings.TrimSuffix(reply.Result.URL, "/websockify") + "/window"
	}
	inline := mint("inline", false, 5901, 200)
	requireStatus(t, workbenchRequest(a, "GET", inline, false), 403)
	mint("fraction", true, 5901.5, 400)
	mint("non_boolean", "true", 5901, 400)
	window := mint("window", true, 5901, 200)
	testPage := workbenchRequest(a, "GET", window, false)
	requireStatus(t, testPage, 200)
	if testPage.Body.String() != body || strings.Contains(testPage.Body.String(), a.token) {
		t.Fatal("viewer must be the exact packaged page")
	}
	csp := testPage.Header().Get("Content-Security-Policy")
	channel := strings.TrimSuffix(window, "/window") + "/websockify"
	for _, want := range []string{"sandbox allow-scripts", "frame-ancestors 'none'", "connect-src ws://example.com" + channel + " wss://example.com" + channel + ";"} {
		if !strings.Contains(csp, want) {
			t.Fatal(csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "allow-popups") || testPage.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal(testPage.Header())
	}
	requireStatus(t, workbenchRequest(a, "GET", window+"?unused=x", false), 400)
	r := httptest.NewRequest(http.MethodGet, window, nil)
	r.Host = "example.com;default-src"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 400)
	file := filepath.Join(a.pluginsPath, "terminal", "viewer.html")
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, workbenchRequest(a, "GET", window, false), 403)
	mint("modified", true, 5901, 409)
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	renew := request(a, "POST", bridge+"/renew", map[string]any{})
	requireStatus(t, renew, 200)
	var rotated struct {
		Instance string `json:"instanceId"`
	}
	if err := json.Unmarshal(renew.Body.Bytes(), &rotated); err != nil || rotated.Instance == "" || rotated.Instance == open.Bridge.Instance {
		t.Fatal(renew.Body.String(), err)
	}
	requireStatus(t, workbenchRequest(a, "GET", window, false), 200)
	bridge = "/api/plugins/terminal/workbench/" + rotated.Instance
	requireStatus(t, workbenchRequest(a, "DELETE", bridge, true), 200)
	requireStatus(t, workbenchRequest(a, "GET", window, false), 403)
	requireStatus(t, workbenchRequest(a, "GET", channel, false), 403)
}

func TestTerminalViewerWindowAssetMustBePackaged(t *testing.T) {
	a := terminalViewerFixture(t, "<!doctype html>Terminal", "")
	if openTerminalViewerFixture(t, a).Bridge.Window {
		t.Fatal("older Terminal must remain inline-only")
	}
	external := filepath.Join(t.TempDir(), "viewer.html")
	if err := os.WriteFile(external, []byte("outside package"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(a.pluginsPath, "terminal", "viewer.html")); err != nil {
		t.Fatal(err)
	}
	if openTerminalViewerFixture(t, a).Bridge.Window {
		t.Fatal("symlink cannot supply a viewer window")
	}
}

// TestTerminalViewerWindowBrowserFixture is an opt-in local browser fixture.
// It serves the actual workbench JS, ticket endpoints and opaque-sandbox HTML.
func TestTerminalViewerWindowBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_TERMINAL_WINDOW_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in isolated browser fixture")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	back := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/websockify" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			http.Error(w, "invalid websocket scope", 403)
			return
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "null" }}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			defer ws.Close()
			_ = ws.WriteMessage(websocket.TextMessage, []byte("scoped websocket connected"))
			_ = ws.SetReadDeadline(time.Now().Add(30 * time.Second))
			_, _, _ = ws.ReadMessage()
		}
	})}
	go back.Serve(listener)
	t.Cleanup(func() { _ = back.Close() })
	frame := `<!doctype html><button id="viewer">Open viewer</button><output id="status"></output><script>
let port,seq=0; addEventListener('message',e=>{if(e.source!==parent||e.data?.type!=='aide.workbench.connect')return;port=e.ports[0];port.onmessage=e=>{document.querySelector('#status').value=JSON.stringify(e.data)};document.querySelector('#viewer').dataset.capability=e.data.capabilities.includes('viewer.window');});
document.querySelector('#viewer').onclick=()=>port?.postMessage({protocol:1,requestId:'r_'+(++seq),method:'viewer.open',arguments:{port:` + strconv.Itoa(port) + `,openInWindow:true}});
parent.postMessage({type:'aide.workbench.ready',protocol:1,requestIds:'monotonic'},'*');
</script>`
	viewer := `<!doctype html><title>Terminal viewer</title><output id="state"></output><script>
const root=document.documentElement;root.dataset.origin=self.origin;root.dataset.opener=String(opener===null);
try{localStorage.getItem('aide-token');root.dataset.storage='available'}catch(e){root.dataset.storage=e.name}
const endpoint=new URL(location.href);endpoint.protocol=endpoint.protocol==='https:'?'wss:':'ws:';endpoint.pathname=endpoint.pathname.replace(/\/window$/,'/websockify');
const socket=new WebSocket(endpoint);socket.onmessage=e=>document.querySelector('#state').value=e.data;
socket.onclose=()=>document.querySelector('#state').value='scoped websocket disconnected';
</script>`
	a := terminalViewerFixture(t, frame, viewer)
	bootstrapToken, _ := json.Marshal(a.token)
	server := &http.Server{Addr: ":18622", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fixture" {
			a.Handler().ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><body><script src="/plugin-workbench.js"></script><script>
const token=` + string(bootstrapToken) + `;let scope=0;
window.revokeViewerScope=()=>scope++;
const api=async(path,opts)=>{const res=await fetch('/api'+path,{...opts,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'}});const value=await res.json();if(!res.ok)throw Error(value.error);return value};
AidePluginWorkbench.open({id:'terminal',enabled:true,workbench:{title:'Terminal',tools:[]}},{inPage:true,t:x=>x,scope:()=>scope,blocked:()=>false,enabled:()=>true,api,toast:e=>document.body.dataset.error=e});
</script>`))
	})}
	t.Cleanup(func() { _ = server.Close() })
	t.Log("Isolated Terminal viewer workbench fixture at :18622/fixture")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
