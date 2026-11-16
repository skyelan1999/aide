package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestOfficeBuiltinProxyIsAideSessionOnly(t *testing.T) {
	a, _ := collaboraFixture(t)
	for _, path := range []string{"/api/office/document/read", "/api/office/document/edit"} {
		requireStatus(t, request(a, "POST", path, map[string]any{"path": "sample.docx"}), 404)
	}
	cookieResponse := request(a, "POST", "/api/office/collabora/sessions", map[string]any{"path": "sample.docx"})
	requireStatus(t, cookieResponse, 200)
	cookies := cookieResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Path != officeEnginePrefix+"/" || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("proxy cookie is not scoped")
	}
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.EscapedPath()
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("Aide credentials leaked")
		}
		w.Header().Set("Set-Cookie", "upstream=bad")
		w.Write([]byte("editor"))
	}))
	defer upstream.Close()
	a.officeRuntime.mu.Lock()
	u, _ := url.Parse(upstream.URL + officeEnginePrefix)
	a.officeRuntime.config.Internal = u
	a.officeRuntime.mu.Unlock()
	call := func(p string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Authorization", "Bearer "+a.token)
		if cookie {
			r.AddCookie(cookies[0])
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	requireStatus(t, call(officeEnginePrefix+"/browser/test/cool.html", false), 401)
	for _, p := range []string{"/cool/adminws", "/cool/convert-to", "/cool/extract-document-structure", "/hosting/discovery", "/cool/../browser/test", "/browser/test/admin/admin.html", "/cool/%2561dminws"} {
		requireStatus(t, call(officeEnginePrefix+p, true), 404)
	}
	requireStatus(t, call("/wopi/files/not-exposed?access_token=anything", true), 404)
	w := call(officeEnginePrefix+"/browser/test/cool.html", true)
	requireStatus(t, w, 200)
	if upstreamPath != officeEnginePrefix+"/browser/test/cool.html" || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("proxy path/cookie boundary")
	}
	a.collabora.mu.Lock()
	for _, s := range a.collabora.sessions {
		s.Expires = time.Now().Add(-time.Second)
	}
	a.collabora.mu.Unlock()
	requireStatus(t, call(officeEnginePrefix+"/browser/test/cool.html", true), 401)
}

func TestOfficeBuiltinProxyWebSocketPreservesEncodedWOPI(t *testing.T) {
	a, _ := collaboraFixture(t)
	w := request(a, "POST", "/api/office/collabora/sessions", map[string]any{"path": "sample.docx"})
	requireStatus(t, w, 200)
	var session map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &session)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte(r.URL.EscapedPath()))
	}))
	defer upstream.Close()
	a.officeRuntime.mu.Lock()
	u, _ := url.Parse(upstream.URL + officeEnginePrefix)
	a.officeRuntime.config.Internal = u
	a.officeRuntime.mu.Unlock()
	host := httptest.NewServer(a.Handler())
	defer host.Close()
	headers := http.Header{"Cookie": []string{w.Result().Cookies()[0].String()}}
	p := officeEnginePrefix + "/cool/http%3A%2F%2F127.0.0.1%3A1234%2Fwopi%2Ffiles%2Fone/ws"
	c, resp, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+p, headers)
	if e != nil {
		t.Fatalf("websocket: %v (%v)", e, resp)
	}
	defer c.Close()
	_, b, e := c.ReadMessage()
	if e != nil || string(b) != p {
		t.Fatalf("encoded path changed: %q %v", b, e)
	}
}

func TestOfficeBuiltinConfigIgnoresFormerExternalSettings(t *testing.T) {
	a, _ := collaboraFixture(t)
	a.mu.Lock()
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Settings = map[string]any{"collaboraEnabled": false, "collaboraUrl": "https://external.invalid", "collaboraInternalUrl": "https://external.invalid", "collaboraCallbackUrl": "https://external.invalid"}
	a.mu.Unlock()
	cfg, e := a.collaboraConfig()
	if e != nil || cfg.Public.String() != officeEnginePrefix || cfg.Internal.Hostname() != "127.0.0.1" {
		t.Fatalf("old external settings affected builtin: %+v %v", cfg, e)
	}
	requireStatus(t, request(a, "GET", "/api/office/collabora", nil), 200)
	a.officeRuntime.mu.Lock()
	a.officeRuntime.done = make(chan struct{})
	close(a.officeRuntime.done)
	a.officeRuntime.mu.Unlock()
	if _, e = a.collaboraConfig(); e == nil {
		t.Fatal("exited engine kept sessions current")
	}
}

func TestOfficeBuiltinCloseStopsOwnedChildAndCallback(t *testing.T) {
	a := testApp(t)
	cmd := exec.Command("sleep", "60")
	configurePluginProcess(cmd, false)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = killPluginProcess(cmd) })
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	callback := &http.Server{Handler: a.officeCallbackHandler()}
	go func() { _ = callback.Serve(listener) }()
	directory, err := os.MkdirTemp("", "aide-office-close-test-")
	if err != nil {
		t.Fatal(err)
	}
	a.officeRuntime.mu.Lock()
	a.officeRuntime.command, a.officeRuntime.done = cmd, done
	a.officeRuntime.callback, a.officeRuntime.directory = callback, directory
	a.officeRuntime.mu.Unlock()
	a.Close()
	select {
	case <-done:
	default:
		t.Fatal("editor child survived App.Close")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("owned temporary editor files survived", err)
	}
	connection, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("private callback survived App.Close")
	}
}

func TestOfficeBuiltinChildDoesNotInheritProviderCredentials(t *testing.T) {
	t.Setenv("AI_API_KEY", "private-provider-sentinel")
	t.Setenv("HTTPS_PROXY", "http://private-proxy.invalid")
	command := exec.Command("env")
	command.Env = officeEngineEnvironment(t.TempDir())
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "private-provider-sentinel") || strings.Contains(string(output), "HTTPS_PROXY=") {
		t.Fatal("Aide credentials/proxy environment leaked to editor child")
	}
}
