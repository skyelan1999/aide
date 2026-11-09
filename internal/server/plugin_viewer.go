package server

// A workbench viewer channel is a manual, plugin-declared capability: the host
// mints a short-lived ticket and proxies only a fixed loopback WebSocket path.
// The frame never receives the Aide token, arbitrary URLs or a general proxy.
import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type pluginViewerTicket struct {
	ID, Bridge, Base string
	Port             int
	Gen              int64
	Expires          time.Time
}

func (a *App) workbenchViewerLocked(id string) bool {
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Enabled && p.Workbench != nil && p.Workbench.Viewer != nil {
			return true
		}
	}
	return false
}

func (a *App) openWorkbenchViewer(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, key := r.PathValue("id"), r.PathValue("instance")
	b, e := a.workbenchBridgeLocked(id, key, true)
	if e != nil || b == nil || !b.Ready {
		fail(w, 403, errors.New("viewer requires an open workbench"))
		return
	}
	if !a.workbenchViewerLocked(id) {
		fail(w, 403, errors.New("viewer capability not declared"))
		return
	}
	lock := a.loadLockStateLocked()
	if lock.Locked {
		fail(w, 403, errors.New("请先解锁工作台"))
		return
	}
	if b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128 {
		fail(w, 409, errors.New("busy, replayed or exhausted workbench request"))
		return
	}
	port := 0
	switch v := req.Arguments["port"].(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	}
	if port < 1024 || port > 65535 {
		fail(w, 400, errors.New("viewer port must be 1024-65535"))
		return
	}
	b.Seen[req.RequestID] = true
	if a.pluginViewerTickets == nil {
		a.pluginViewerTickets = map[string]pluginViewerTicket{}
	}
	now := time.Now()
	for key, t := range a.pluginViewerTickets {
		if now.After(t.Expires) {
			delete(a.pluginViewerTickets, key)
		}
	}
	if len(a.pluginViewerTickets) >= 32 {
		fail(w, 429, errors.New("too many viewer sessions"))
		return
	}
	secret := make([]byte, 32)
	if _, e := rand.Read(secret); e != nil {
		fail(w, 500, e)
		return
	}
	ticket := hex.EncodeToString(secret)
	a.pluginViewerTickets[ticket] = pluginViewerTicket{ID: id, Bridge: key, Base: a.managementBaseLocked("plugins", id), Port: port, Gen: lock.Gen, Expires: now.Add(30 * time.Minute)}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": map[string]any{"protocol": 1, "url": "/plugin-viewer/" + ticket + "/websockify", "expiresIn": 1800}})
}

func (a *App) servePluginViewer(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	t, ok := a.pluginViewerTickets[r.PathValue("ticket")]
	lock := a.loadLockStateLocked()
	valid := ok && !time.Now().After(t.Expires) && !lock.Locked && lock.Gen == t.Gen && a.managementBaseLocked("plugins", t.ID) == t.Base && a.workbenchViewerLocked(t.ID)
	b := a.pluginWorkbenchBridges[t.Bridge]
	a.mu.Unlock()
	if !valid || b == nil {
		http.Error(w, "viewer request expired or revoked", 403)
		return
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") || r.Method != http.MethodGet {
		http.Error(w, "viewer channel requires a WebSocket upgrade", 400)
		return
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Port)), Path: "/websockify", RawQuery: r.URL.RawQuery}
	proxy := &httputil.ReverseProxy{
		Director: func(out *http.Request) {
			out.URL = target
			out.Host = target.Host
			out.Header.Del("Cookie")
			out.Header.Del("Authorization")
			out.Header.Del("X-Forwarded-For")
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          0,
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "viewer backend unavailable", 502)
		},
	}
	proxy.ServeHTTP(w, r)
}
