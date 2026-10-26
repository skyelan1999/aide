package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type nativeEditorLease struct {
	Ticket, LeaseID, Workspace, Package string
	Revision                            uint64
	LockGen                             int64
	Target                              nativeEditorTarget
	Expires                             time.Time
}
type nativeEditorContextKey struct{}

// Admission is shared by all host write paths. Open never starts an editor
// while a host operation is already in flight. Nested calls inherit admission.
func (a *App) nativeEditorEnter(ctx context.Context) (context.Context, func()) {
	if ctx.Value(nativeEditorContextKey{}) == a {
		return ctx, func() {}
	}
	a.nativeEditorAdmission.RLock()
	return context.WithValue(ctx, nativeEditorContextKey{}, a), a.nativeEditorAdmission.RUnlock
}
func nativeEditorConfig() (nativeEditorTargetConfig, *url.URL, string, error) {
	cfg := nativeEditorTargetConfig{os.Getenv("AIDE_BRDPLAN_EDITOR_WORKSPACE"), os.Getenv("AIDE_BRDPLAN_EDITOR_SESSION")}
	u, e := url.Parse(os.Getenv("AIDE_BRDPLAN_EDITOR_URL"))
	if e != nil || u == nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || cfg.WorkspaceRoot == "" || !nativeEditorRelative(cfg.SessionPath) {
		return cfg, nil, "", errors.New("Native editor is not configured")
	}
	raw, e := os.ReadFile(os.Getenv("AIDE_BRDPLAN_EDITOR_TOKEN_FILE"))
	token := strings.TrimSpace(string(raw))
	if e != nil || len(token) < 32 || len(token) > 1024 {
		return cfg, nil, "", errors.New("Native editor credential unavailable")
	}
	return cfg, u, token, nil
}
func nativeEditorRPC(ctx context.Context, method, path string, body any) (map[string]any, error) {
	_, u, token, e := nativeEditorConfig()
	if e != nil {
		return nil, e
	}
	var b io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		b = bytes.NewReader(raw)
	}
	target := *u
	target.Path = path
	req, e := http.NewRequestWithContext(ctx, method, target.String(), b)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Editor redirects forbidden") }}
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("Native editor service unavailable")
	}
	defer resp.Body.Close()
	var out map[string]any
	if e = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&out); e != nil {
		return nil, errors.New("Invalid editor response")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Native editor refused: %v", out["error"])
	}
	return out, nil
}

// Caller holds a.mu. The persistent marker survives lost viewers and restarts.
// This deliberately protects the whole host workspace, including unknown tools.
func (a *App) nativeEditorWriteErrorLocked() error {
	root, session := os.Getenv("AIDE_BRDPLAN_EDITOR_WORKSPACE"), os.Getenv("AIDE_BRDPLAN_EDITOR_SESSION")
	if root == "" || !nativeEditorRelative(session) {
		return nil
	}
	if a.nativeEditorOpening {
		return errors.New("原生编辑器正在打开，写入暂时锁定")
	}
	locks, _ := filepath.Glob(filepath.Join(root, session, "project", "*.lck"))
	if len(locks) > 0 {
		return errors.New("原生工程仍有编辑锁，请正常保存关闭编辑器")
	}
	marker := filepath.Join(root, session, "bridge", "editor-active.json")
	raw, e := os.ReadFile(marker)
	if os.IsNotExist(e) {
		return nil
	}
	var state struct {
		Active bool `json:"active"`
	}
	if e != nil || json.Unmarshal(raw, &state) != nil || state.Active {
		return errors.New("原生编辑器占用工程：请保存并正常关闭原生窗口后再写入；断连不代表已保存")
	}
	return nil
}
func (a *App) nativeEditorWriteError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nativeEditorWriteErrorLocked()
}
func nativeEditorReadTool(name string) bool {
	switch name {
	case "browse_plugin_market", "brdplan_bridge_status", "brdplan_bridge_result", "brdplan_workspace_projects", "brdplan_guide", "brdplan_engine_status", "read_file", "list_files", "search_files", "document_search", "semantic_search":
		return true
	}
	return false
}
func nativeEditorMutationRoute(r *http.Request) bool {
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		return false
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/api/file") || strings.HasPrefix(p, "/api/directory") || strings.HasPrefix(p, "/api/command") || strings.HasSuffix(p, "/apply")
}
func (a *App) openNativeEditor(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "brdplan" {
		fail(w, 404, errors.New("Unknown editor provider"))
		return
	}
	var in nativeEditorOpenRequest
	if e := decode(w, r, &in); e != nil {
		fail(w, 400, e)
		return
	}
	if !a.nativeEditorAdmission.TryLock() {
		fail(w, 409, errors.New("Host operation in progress; wait before opening editor"))
		return
	}
	defer a.nativeEditorAdmission.Unlock()
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, _, _, e := nativeEditorConfig()
	if e != nil {
		fail(w, 503, e)
		return
	}
	lock := a.loadLockStateLocked()
	if lock.Locked || sandboxMode(a.settings.SandboxMode) == "read-only" || a.workspaceMode() == "ssh" {
		fail(w, 403, errors.New("Unlock a writable local workspace first"))
		return
	}
	if _, e = a.workbenchPageLocked("brdplan"); e != nil {
		fail(w, 403, e)
		return
	}
	if len(a.commands) > 0 {
		fail(w, 409, errors.New("Close running commands before native editing"))
		return
	}
	for _, s := range a.sessions {
		for _, t := range s.Runs {
			if t.Status == "running" || t.Status == "queued" {
				fail(w, 409, errors.New("Wait for active AI tasks before native editing"))
				return
			}
		}
	}
	target, e := prepareNativeEditorTarget(a.workspace, cfg, in)
	if e != nil {
		fail(w, 409, e)
		return
	}
	a.nativeEditorOpening = true
	defer func() { a.nativeEditorOpening = false }()
	for ticket, old := range a.nativeEditors {
		nativeEditorRPC(r.Context(), "POST", "/lease/close", map[string]any{"leaseId": old.LeaseID})
		delete(a.nativeEditors, ticket)
	}
	response, e := nativeEditorRPC(r.Context(), "POST", "/lease/open", map[string]any{"generation": target.Generation, "project": target.ProjectStem, "view": target.View})
	if e != nil {
		fail(w, 409, e)
		return
	}
	rid, ok := response["leaseId"].(string)
	if !ok || len(rid) != 64 || response["project"] != target.ProjectStem || response["view"] != target.View || response["generation"] != target.Generation {
		fail(w, 502, errors.New("Editor identity mismatch; editing state unknown"))
		return
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		fail(w, 500, e)
		return
	}
	ticket := hex.EncodeToString(secret)
	lease := &nativeEditorLease{Ticket: ticket, LeaseID: rid, Workspace: a.wsID(), Revision: a.wsRevision, Package: a.pluginPackageDigestLocked("brdplan"), LockGen: lock.Gen, Target: target, Expires: time.Now().Add(60 * time.Second)}
	if a.nativeEditors == nil {
		a.nativeEditors = map[string]*nativeEditorLease{}
	}
	a.nativeEditors[ticket] = lease
	go a.watchNativeEditor(ticket)
	jsonOut(w, 200, map[string]any{"protocol": 1, "status": "ready", "editorId": ticket, "url": "/plugin-editor/" + ticket + "/", "expiresAt": lease.Expires})
}
func (a *App) nativeEditorLeaseLocked(ticket string) (*nativeEditorLease, error) {
	lease := a.nativeEditors[ticket]
	if lease == nil {
		return nil, errors.New("Editor capability revoked")
	}
	lock := a.loadLockStateLocked()
	if time.Now().After(lease.Expires) || lock.Locked || lock.Gen != lease.LockGen || a.wsRevision != lease.Revision || a.wsID() != lease.Workspace || a.pluginPackageDigestLocked("brdplan") != lease.Package {
		return nil, errors.New("Editor scope expired or revoked")
	}
	if _, e := a.workbenchPageLocked("brdplan"); e != nil {
		return nil, e
	}
	cfg, _, _, e := nativeEditorConfig()
	if e != nil {
		return nil, e
	}
	raw, _, e := nativeEditorRead(a.workspace, cfg.SessionPath+"/bridge/session.json", 32768)
	if e != nil {
		return nil, e
	}
	var descriptor struct {
		Generation string `json:"generation"`
	}
	if json.Unmarshal(raw, &descriptor) != nil || descriptor.Generation != lease.Target.Generation {
		return nil, errors.New("Editor generation changed")
	}
	return lease, nil
}
func (a *App) watchNativeEditor(ticket string) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for range timer.C {
		a.mu.Lock()
		lease := a.nativeEditors[ticket]
		if lease == nil {
			a.mu.Unlock()
			return
		}
		_, e := a.nativeEditorLeaseLocked(ticket)
		if e != nil {
			delete(a.nativeEditors, ticket)
		}
		a.mu.Unlock()
		if e != nil {
			nativeEditorRPC(context.Background(), "POST", "/lease/close", map[string]any{"leaseId": lease.LeaseID})
			return
		}
	}
}
func (a *App) closeNativeEditor(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	lease := a.nativeEditors[r.PathValue("ticket")]
	delete(a.nativeEditors, r.PathValue("ticket"))
	a.mu.Unlock()
	if lease == nil {
		jsonOut(w, 200, map[string]any{"state": "unknown", "saved": "unknown"})
		return
	}
	out, e := nativeEditorRPC(r.Context(), "POST", "/lease/close", map[string]any{"leaseId": lease.LeaseID})
	if e != nil {
		fail(w, 409, e)
		return
	}
	jsonOut(w, 200, out)
}
func (a *App) serveNativeEditor(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	lease, e := a.nativeEditorLeaseLocked(r.PathValue("ticket"))
	var copyLease nativeEditorLease
	if lease != nil {
		copyLease = *lease
	}
	a.mu.Unlock()
	if e != nil {
		http.Error(w, e.Error(), 403)
		return
	}
	asset := r.PathValue("asset")
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "null" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host {
			http.Error(w, "Editor origin rejected", 403)
			return
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "null")
	w.Header().Set("Cache-Control", "no-store")
	if asset == "heartbeat" && r.Method == "POST" {
		_, e := nativeEditorRPC(r.Context(), "POST", "/lease/heartbeat", map[string]any{"leaseId": copyLease.LeaseID})
		if e != nil {
			http.Error(w, "Editor heartbeat failed", 409)
			return
		}
		a.mu.Lock()
		if a.nativeEditors[copyLease.Ticket] == lease {
			lease.Expires = time.Now().Add(60 * time.Second)
		}
		a.mu.Unlock()
		jsonOut(w, 200, map[string]bool{"active": true})
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Unsupported method", 405)
		return
	}
	if asset == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' wss://"+r.Host+" ws://"+r.Host+"; sandbox allow-scripts; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
		io.WriteString(w, nativeEditorViewer)
		return
	}
	if asset != "websockify" && !strings.HasPrefix(asset, "core/") && !strings.HasPrefix(asset, "vendor/") {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(asset, "..") || strings.ContainsAny(asset, "\\\x00") || r.URL.RawQuery != "" {
		http.Error(w, "Invalid editor asset", 400)
		return
	}
	_, target, token, e := nativeEditorConfig()
	if e != nil {
		http.Error(w, "Editor unavailable", 503)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/" + asset
		req.URL.RawPath = ""
		req.URL.RawQuery = ""
		req.Host = target.Host
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-BrdPlan-Lease", copyLease.LeaseID)
		req.Header.Del("Cookie")
		req.Header.Del("Origin")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "null")
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Del("Set-Cookie")
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Editor connection unavailable; save state unknown", 502)
	}
	// ReverseProxy copies backend headers with Add. Set CORS exactly once in ModifyResponse.
	w.Header().Del("Access-Control-Allow-Origin")
	proxy.ServeHTTP(w, r)
}

const nativeEditorViewer = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>BrdPlan 原生编辑器</title><style>body{margin:0;background:#17251e;color:white;font:14px system-ui}header{height:42px;display:flex;align-items:center;gap:14px;padding:0 12px}button{padding:5px 10px}#screen{height:calc(100vh - 42px);background:#333}#state{margin-left:auto}</style><header><strong>BrdPlan · KiCad 原生编辑</strong><button id="save">保存 Ctrl+S</button><button id="finish">关闭原生窗口 Alt+F4</button><span id="state">连接中；保存状态未知</span></header><main id="screen"></main><script type="module">
import RFB from './core/rfb.js';
const state=document.querySelector('#state'),url=new URL('./websockify',location.href);url.protocol=location.protocol==='https:'?'wss:':'ws:';
const rfb=new RFB(document.querySelector('#screen'),url.href,{shared:true});rfb.scaleViewport=true;rfb.resizeSession=false;
rfb.addEventListener('connect',()=>{state.textContent='已连接 · 请显式保存并关闭原生窗口';rfb.focus();});rfb.addEventListener('disconnect',()=>state.textContent='连接已断开 · 保存状态未知，请恢复核对');
document.querySelector('#save').onclick=()=>{rfb.sendKey(0xffe3,'ControlLeft',true);rfb.sendKey(0x73,'KeyS');rfb.sendKey(0xffe3,'ControlLeft',false);rfb.focus();state.textContent='已发送保存按键 · 请检查KiCad并重开核对';};
document.querySelector('#finish').onclick=()=>{rfb.sendKey(0xffe9,'AltLeft',true);rfb.sendKey(0xffc1,'F4');rfb.sendKey(0xffe9,'AltLeft',false);rfb.focus();state.textContent='请处理KiCad保存对话框；关闭浏览器不等于关闭工程';};
setInterval(async()=>{try{const r=await fetch('./heartbeat',{method:'POST',credentials:'omit'});if(!r.ok)throw Error();}catch(_){rfb.disconnect();state.textContent='作用域已撤销 · 编辑仍可能未保存，请恢复核对';}},15000);
</script></html>`
