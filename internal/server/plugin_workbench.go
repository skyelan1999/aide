package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxWorkbenchHTML = 2 << 20
const workbenchCSP = "default-src 'none'; script-src 'unsafe-inline' blob:; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; worker-src blob:; frame-src 'none'; object-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-scripts allow-downloads"

type pluginWorkbenchTicket struct {
	ID, Base string
	Digest   [32]byte
	Gen      int64
	Expires  time.Time
}

func validatePluginWorkbench(w *PluginWorkbench) error {
	if w == nil || w.Protocol != 1 || len([]rune(w.Title)) < 1 || len([]rune(w.Title)) > 64 || len(w.Entry) > 160 || path.Clean(w.Entry) != w.Entry || !strings.HasSuffix(w.Entry, ".html") || strings.ContainsAny(w.Entry, "\\:") || strings.HasPrefix(w.Entry, "/") {
		return errors.New("插件工作台声明无效")
	}
	for _, part := range strings.Split(w.Entry, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return errors.New("插件工作台路径无效")
		}
	}
	return nil
}

// Caller holds a.mu. The plugin must remain registered and enabled. No arbitrary asset routes.
func (a *App) workbenchPageLocked(id string) ([]byte, error) {
	if !pluginIDPattern.MatchString(id) {
		return nil, errors.New("插件标识无效")
	}
	var spec *PluginWorkbench
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Enabled {
			spec = p.Workbench
			break
		}
	}
	if validatePluginWorkbench(spec) != nil {
		return nil, errors.New("插件未启用或未声明工作台")
	}
	dir := filepath.Join(a.pluginsPath, id)
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("插件目录无效")
	}
	current := dir
	for _, part := range strings.Split(spec.Entry, "/") {
		current = filepath.Join(current, part)
		i, e := os.Lstat(current)
		if e != nil || i.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("工作台不允许符号链接")
		}
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(spec.Entry)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > maxWorkbenchHTML {
		return nil, errors.New("工作台 HTML 无效或过大")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxWorkbenchHTML+1))
	if e != nil || len(b) == 0 || len(b) > maxWorkbenchHTML {
		return nil, errors.New("工作台 HTML 无效或过大")
	}
	return b, nil
}
func (a *App) openPluginWorkbench(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lock := a.loadLockStateLocked()
	if lock.Locked {
		fail(w, 403, errors.New("请先解锁工作台"))
		return
	}
	id := r.PathValue("id")
	page, e := a.workbenchPageLocked(id)
	if e != nil {
		fail(w, 400, e)
		return
	}
	if a.pluginWorkbenchTickets == nil {
		a.pluginWorkbenchTickets = map[string]pluginWorkbenchTicket{}
	}
	now := time.Now()
	for key, t := range a.pluginWorkbenchTickets {
		if now.After(t.Expires) {
			delete(a.pluginWorkbenchTickets, key)
		}
	}
	if len(a.pluginWorkbenchTickets) >= 8 {
		fail(w, 429, errors.New("打开请求过多，请稍后重试"))
		return
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		fail(w, 500, e)
		return
	}
	ticket := hex.EncodeToString(secret)
	a.pluginWorkbenchTickets[ticket] = pluginWorkbenchTicket{ID: id, Base: a.managementBaseLocked("plugins", id), Digest: sha256.Sum256(page), Gen: lock.Gen, Expires: now.Add(time.Minute)}
	jsonOut(w, 200, map[string]any{"url": "/plugin-workbench/" + ticket, "protocol": 1})
}
func (a *App) servePluginWorkbench(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := r.PathValue("ticket")
	ticket, ok := a.pluginWorkbenchTickets[key]
	delete(a.pluginWorkbenchTickets, key)
	lock := a.loadLockStateLocked()
	if !ok || time.Now().After(ticket.Expires) || lock.Locked || lock.Gen != ticket.Gen || a.managementBaseLocked("plugins", ticket.ID) != ticket.Base {
		http.Error(w, "Workbench request expired or revoked", 403)
		return
	}
	page, e := a.workbenchPageLocked(ticket.ID)
	if e != nil || sha256.Sum256(page) != ticket.Digest {
		http.Error(w, "Workbench changed; reopen it", 409)
		return
	}
	w.Header().Set("Content-Security-Policy", workbenchCSP)
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), serial=(), clipboard-read=(), clipboard-write=()")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write(page)
}
