package server

// A workbench viewer channel is a manual, plugin-declared capability: the host
// mints a short-lived ticket and proxies only a fixed loopback WebSocket path.
// The frame never receives the Aide token, arbitrary URLs or a general proxy.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type pluginViewerTicket struct {
	ID, Bridge, Base string
	View             string
	Port             int
	Window           bool
	Connected        bool
	Gen              int64
	Expires          time.Time
}

var pluginViewerHost = regexp.MustCompile(`^(?:[A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(?::[0-9]{1,5})?$`)

func (a *App) readTerminalViewerWindow() ([]byte, error) {
	return readPluginWorkbenchPage(filepath.Join(a.pluginsPath, "terminal"), "viewer.html")
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
	a.openWorkbenchViewerLocked(w, id, key, b, req, "")
}

func (a *App) openWorkbenchViewerLocked(w http.ResponseWriter, id, key string, b *pluginWorkbenchBridge, req workbenchBridgeRequest, view string) {
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
	window, validWindow := req.Arguments["openInWindow"].(bool)
	if _, supplied := req.Arguments["openInWindow"]; supplied && !validWindow {
		fail(w, 400, errors.New("viewer openInWindow must be a boolean"))
		return
	}
	if window {
		if id != "terminal" || b.ViewerWindow == [32]byte{} {
			fail(w, 403, errors.New("viewer window is unavailable"))
			return
		}
		page, err := a.readTerminalViewerWindow()
		if err != nil || sha256.Sum256(page) != b.ViewerWindow {
			fail(w, 409, errors.New("viewer package changed; reopen workbench"))
			return
		}
	}
	port := 0
	switch v := req.Arguments["port"].(type) {
	case float64:
		if v >= 1024 && v <= 65535 && v == float64(int(v)) {
			port = int(v)
		}
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
	a.pluginViewerTickets[ticket] = pluginViewerTicket{ID: id, Bridge: key, Base: a.managementBaseLocked("plugins", id), View: view, Port: port, Window: window, Gen: lock.Gen, Expires: now.Add(30 * time.Minute)}
	base := "/plugin-viewer/" + ticket
	result := map[string]any{"protocol": 1, "url": base + "/websockify", "expiresIn": 1800}
	if view != "" {
		result["url"] = base + "/window"
		result["fallbackUrl"] = base + "/websockify"
	}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": result})
}

func (a *App) servePluginViewerWindow(w http.ResponseWriter, r *http.Request) {
	if len(r.Host) > 255 || !pluginViewerHost.MatchString(r.Host) || r.URL.RawQuery != "" {
		http.Error(w, "invalid viewer window request", http.StatusBadRequest)
		return
	}
	key := r.PathValue("ticket")
	a.mu.Lock()
	t, ok := a.pluginViewerTickets[key]
	var page []byte
	if ok && t.Window && t.ID == "terminal" && time.Now().Before(t.Expires) && a.workbenchViewerLocked(t.ID) && (t.View == "" || a.pluginViewerCurrentLocked(t)) {
		if b, err := a.workbenchBridgeLocked(t.ID, t.Bridge, true); err == nil && b.ViewerWindow != [32]byte{} {
			if body, err := a.readTerminalViewerWindow(); err == nil && sha256.Sum256(body) == b.ViewerWindow {
				page = body
			}
		}
	}
	a.mu.Unlock()
	if len(page) == 0 {
		http.Error(w, "viewer request expired or revoked", http.StatusForbidden)
		return
	}
	channel := r.Host + "/plugin-viewer/" + key + "/websockify"
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline' blob:; style-src 'unsafe-inline'; img-src data: blob:; worker-src blob:; connect-src ws://"+channel+" wss://"+channel+"; frame-ancestors 'none'; object-src 'none'; form-action 'none'; base-uri 'none'; sandbox allow-scripts")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), serial=(), clipboard-read=(), clipboard-write=()")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(page)
}

func (a *App) pluginViewerCurrentLocked(t pluginViewerTicket) bool {
	if !time.Now().Before(t.Expires) || !a.workbenchViewerLocked(t.ID) {
		return false
	}
	if t.Window {
		b, err := a.workbenchBridgeLocked(t.ID, t.Bridge, false)
		if err != nil || b.Ticket.Gen != t.Gen || b.Ticket.Base != t.Base {
			return false
		}
		if t.View != "" {
			grant, valid := a.workbenchWindowGrantLocked(t.ID, t.View, b)
			return valid && grant.Spec.Kind == "viewer" && b.Package == a.pluginPackageDigestLocked(t.ID)
		}
		return true
	}
	lock := a.loadLockStateLocked()
	return !lock.Locked && lock.Gen == t.Gen && a.managementBaseLocked("plugins", t.ID) == t.Base && a.pluginWorkbenchBridges[t.Bridge] != nil
}

// Detached viewers have no opener from which to receive lifecycle events.
// Cancel the upgraded connection when its ticket or workbench scope ends.
func (a *App) detachedViewerContext(ctx context.Context, key string, t pluginViewerTicket) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(ctx, t.Expires)
	go func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				a.mu.Lock()
				live, ok := a.pluginViewerTickets[key]
				current := ok && live.Connected && a.pluginViewerCurrentLocked(live)
				a.mu.Unlock()
				if !current {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func (a *App) servePluginViewer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("ticket")
	upgrade := strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") && r.Method == http.MethodGet
	a.mu.Lock()
	t, ok := a.pluginViewerTickets[key]
	valid := ok && a.pluginViewerCurrentLocked(t)
	claim := valid && t.Window && upgrade && r.URL.RawQuery == "" && !t.Connected
	if claim {
		t.Connected = true
		a.pluginViewerTickets[key] = t
	}
	a.mu.Unlock()
	if !valid {
		http.Error(w, "viewer request expired or revoked", 403)
		return
	}
	if !upgrade {
		http.Error(w, "viewer channel requires a WebSocket upgrade", 400)
		return
	}
	if t.Window {
		if r.URL.RawQuery != "" {
			http.Error(w, "invalid viewer WebSocket request", http.StatusBadRequest)
			return
		}
		if !claim {
			http.Error(w, "viewer window is already connected", http.StatusConflict)
			return
		}
		defer func() {
			a.mu.Lock()
			if live, ok := a.pluginViewerTickets[key]; ok {
				live.Connected = false
				a.pluginViewerTickets[key] = live
			}
			a.mu.Unlock()
		}()
		ctx, cancel := a.detachedViewerContext(r.Context(), key, t)
		defer cancel()
		r = r.WithContext(ctx)
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
