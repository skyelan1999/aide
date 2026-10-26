package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestIssue93ReadOnlyPermanentlyRevokesOpenedEditor(t *testing.T) {
	a, _ := editorAppFixture(t)
	viewer := editorOpen(t, a)
	requireStatus(t, workbenchRequest(a, "GET", viewer+"core/rfb.js", false), 200)
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()
	settings.SandboxMode = "read-only"
	requireStatus(t, request(a, "PUT", "/api/settings", settings), 200)
	for _, asset := range []string{"", "core/rfb.js", "vendor/probe.js", "websockify", "heartbeat"} {
		method := "GET"
		if asset == "heartbeat" {
			method = "POST"
		}
		if w := workbenchRequest(a, method, viewer+asset, false); w.Code != 403 {
			t.Errorf("read-only retained editor %s: %d", asset, w.Code)
		}
	}
	settings.SandboxMode = "workspace-write"
	requireStatus(t, request(a, "PUT", "/api/settings", settings), 200)
	if w := workbenchRequest(a, "GET", viewer, false); w.Code != 403 {
		t.Errorf("old editor revived after writable mode restored: %d", w.Code)
	}
}

func TestIssue93ReadOnlyDisconnectsExistingEditorWebSocket(t *testing.T) {
	a, _ := editorAppFixture(t)
	original, err := url.Parse(os.Getenv("AIDE_BRDPLAN_EDITOR_URL"))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(original)
	backendClosed := make(chan struct{})
	closeRequested := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lease/close" {
			select {
			case closeRequested <- struct{}{}:
			default:
			}
		}
		if r.URL.Path != "/websockify" {
			proxy.ServeHTTP(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(backendClosed)
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, payload); err != nil {
				return
			}
		}
	}))
	defer backend.Close()
	t.Setenv("AIDE_BRDPLAN_EDITOR_URL", backend.URL)
	viewer := editorOpen(t, a)
	host := httptest.NewServer(a.Handler())
	defer host.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http")+viewer+"websockify", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("owned-editor-input")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil || string(payload) != "owned-editor-input" {
		t.Fatal("writable editor socket failed", err)
	}
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()
	settings.SandboxMode = "read-only"
	requireStatus(t, request(a, "PUT", "/api/settings", settings), 200)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("revoked socket still reading")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Error("revoked socket was not disconnected")
	}
	select {
	case <-backendClosed:
	case <-time.After(time.Second):
		t.Error("backend socket retained after revocation")
	}
	select {
	case <-closeRequested:
	case <-time.After(2 * time.Second):
		t.Error("backend lease close not requested")
	}
}

func TestIssue93FailedSettingsSaveKeepsWritableLease(t *testing.T) {
	a, _ := editorAppFixture(t)
	viewer := editorOpen(t, a)
	settingsPath := SettingsPath(a.dataPath)
	if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0700); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()
	settings.SandboxMode = "read-only"
	requireStatus(t, request(a, "PUT", "/api/settings", settings), 500)
	requireStatus(t, workbenchRequest(a, "GET", viewer, false), 200)
}
