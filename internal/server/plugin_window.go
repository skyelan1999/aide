package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"sort"
	"time"
)

// A window is bound to a packaged HTML view. Viewer is the only adapter that
// adds a transport, and is reserved for Terminal's existing viewer grant.
type PluginWorkbenchWindow struct {
	Entry string `json:"entry"`
	Kind  string `json:"kind,omitempty"`
}

type workbenchWindowGrant struct {
	Spec   PluginWorkbenchWindow
	Digest [sha256.Size]byte
}

type workbenchWindowCapability struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type pluginWindowTicket struct {
	Plugin, Bridge, View string
	Expires              time.Time
}

const pluginWindowCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; connect-src 'none'; frame-src 'none'; frame-ancestors 'none'; object-src 'none'; form-action 'none'; base-uri 'none'"

func validatePluginWorkbenchWindows(windows map[string]PluginWorkbenchWindow) error {
	if len(windows) > 8 {
		return errors.New("too many workbench windows")
	}
	for id, window := range windows {
		if !pluginIDPattern.MatchString(id) || !validWorkbenchHTMLPath(window.Entry) ||
			(window.Kind != "" && (window.Kind != "viewer" || window.Entry != "viewer.html")) {
			return errors.New("invalid workbench window declaration")
		}
	}
	return nil
}

func (a *App) readWorkbenchWindowLocked(id, entry string) ([]byte, error) {
	return retryWorkbenchPageRead(func() ([]byte, error) {
		return readPluginWorkbenchPage(filepath.Join(a.pluginsPath, id), entry)
	})
}

func (a *App) workbenchWindowGrantsLocked(p PluginManifest, bridge *pluginWorkbenchBridge) []workbenchWindowCapability {
	var capabilities []workbenchWindowCapability
	for view, spec := range p.Workbench.Windows {
		kind := spec.Kind
		var digest [sha256.Size]byte
		if kind == "viewer" {
			if p.ID != "terminal" || p.Workbench.Viewer == nil || bridge.ViewerWindow == [sha256.Size]byte{} {
				continue
			}
			digest = bridge.ViewerWindow
		} else {
			page, err := a.readWorkbenchWindowLocked(p.ID, spec.Entry)
			if err != nil {
				continue
			}
			digest = sha256.Sum256(page)
			kind = "static"
		}
		if bridge.Windows == nil {
			bridge.Windows = make(map[string]workbenchWindowGrant)
		}
		bridge.Windows[view] = workbenchWindowGrant{Spec: spec, Digest: digest}
		capabilities = append(capabilities, workbenchWindowCapability{ID: view, Kind: kind})
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].ID < capabilities[j].ID })
	return capabilities
}

func (a *App) workbenchWindowGrantLocked(id, view string, bridge *pluginWorkbenchBridge) (workbenchWindowGrant, bool) {
	grant, ok := bridge.Windows[view]
	if !ok {
		return grant, false
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Enabled && p.Workbench != nil {
			spec, declared := p.Workbench.Windows[view]
			return grant, declared && spec == grant.Spec
		}
	}
	return grant, false
}

func (a *App) openWorkbenchWindowLocked(w http.ResponseWriter, id, key string, bridge *pluginWorkbenchBridge, req workbenchBridgeRequest) {
	view, ok := req.Arguments["view"].(string)
	if !ok || !pluginIDPattern.MatchString(view) || req.Tool != "" || req.ProposalID != "" || len(req.Attachments) != 0 {
		fail(w, http.StatusBadRequest, errors.New("invalid workbench window request"))
		return
	}
	grant, ok := a.workbenchWindowGrantLocked(id, view, bridge)
	if !ok {
		fail(w, http.StatusForbidden, errors.New("workbench window is not declared"))
		return
	}
	if grant.Spec.Kind == "viewer" {
		port, supplied := req.Arguments["viewerPort"]
		if !supplied || len(req.Arguments) != 2 {
			fail(w, http.StatusBadRequest, errors.New("viewer window requires a viewer port"))
			return
		}
		req.Arguments = map[string]any{"openInWindow": true, "port": port}
		a.openWorkbenchViewerLocked(w, id, key, bridge, req, view)
		return
	}
	if len(req.Arguments) != 1 {
		fail(w, http.StatusBadRequest, errors.New("static window accepts only its view ID"))
		return
	}
	if bridge.Busy || bridge.Seen[req.RequestID] || len(bridge.Seen) >= 128 {
		fail(w, http.StatusConflict, errors.New("busy, replayed or exhausted workbench request"))
		return
	}
	page, err := a.readWorkbenchWindowLocked(id, grant.Spec.Entry)
	if err != nil || sha256.Sum256(page) != grant.Digest {
		fail(w, http.StatusConflict, errors.New("window package changed; reopen workbench"))
		return
	}
	now := time.Now()
	for ticket, pending := range a.pluginWindowTickets {
		if !now.Before(pending.Expires) || a.pluginWorkbenchBridges[pending.Bridge] == nil {
			delete(a.pluginWindowTickets, ticket)
		}
	}
	if len(a.pluginWindowTickets) >= 32 {
		fail(w, http.StatusTooManyRequests, errors.New("too many pending workbench windows"))
		return
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	ticket := hex.EncodeToString(secret[:])
	if a.pluginWindowTickets == nil {
		a.pluginWindowTickets = make(map[string]pluginWindowTicket)
	}
	a.pluginWindowTickets[ticket] = pluginWindowTicket{Plugin: id, Bridge: key, View: view, Expires: now.Add(time.Minute)}
	bridge.Seen[req.RequestID] = true
	jsonOut(w, http.StatusOK, map[string]any{"requestId": req.RequestID, "status": "result", "result": map[string]any{
		"protocol": 1, "url": "/plugin-window/" + ticket,
	}})
}

func (a *App) takePluginWindow(ticket string) []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	pending, ok := a.pluginWindowTickets[ticket]
	delete(a.pluginWindowTickets, ticket)
	if !ok || !time.Now().Before(pending.Expires) {
		return nil
	}
	bridge, err := a.workbenchBridgeLocked(pending.Plugin, pending.Bridge, true)
	if err != nil {
		return nil
	}
	grant, ok := a.workbenchWindowGrantLocked(pending.Plugin, pending.View, bridge)
	if !ok || grant.Spec.Kind != "" {
		return nil
	}
	page, err := a.readWorkbenchWindowLocked(pending.Plugin, grant.Spec.Entry)
	if err != nil || sha256.Sum256(page) != grant.Digest {
		return nil
	}
	return page
}

func (a *App) servePluginWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "workbench window requires GET", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "invalid workbench window request", http.StatusBadRequest)
		return
	}
	page := a.takePluginWindow(r.PathValue("ticket"))
	if len(page) == 0 {
		http.Error(w, "workbench window expired or revoked", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Security-Policy", pluginWindowCSP)
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), serial=(), clipboard-read=(), clipboard-write=()")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// The trusted shell stays same-origin so the owning workbench can close it.
	// Only its nested, opaque frame runs plugin code; frame-src blocks navigation
	// from that frame to another document.
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Aide</title><style>iframe{position:fixed;inset:0;width:100%%;height:100%%;border:0}</style><iframe title="Workbench view" sandbox="allow-scripts" srcdoc="%s"></iframe>`, html.EscapeString(string(page)))
}
