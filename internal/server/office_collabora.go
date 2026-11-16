package server

// Collabora receives short-lived, document-scoped capabilities, never the Aide
// bearer token. All callbacks recheck the original workspace/source and version.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type collaboraState struct {
	mu       sync.Mutex
	docs     map[string]*collaboraDocument
	sessions map[string]*collaboraSession
}
type collaboraDocument struct {
	Area                    knowledgeArea
	Path, Hash, Stamp, Lock string
	ID                      string
	Outdated                bool
	LockUntil               time.Time
}
type collaboraSession struct {
	ID, TokenHash, ProxyTokenHash, Config, ParentOrigin string
	Doc                                                 *collaboraDocument
	Write, Dirty                                        bool
	Expires, ActiveUntil                                time.Time
	LockGen                                             int64
}
type collaboraConfiguration struct {
	Public, Internal, Callback *url.URL
	Fingerprint                string
}

func collaboraURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t") {
		return nil, errors.New("Collabora 地址必须是无凭据和查询参数的 HTTP(S) 地址")
	}
	return u, nil
}
func (a *App) collaboraConfig() (collaboraConfiguration, error) {
	if !a.officeViewerEnabled() {
		return collaboraConfiguration{}, errors.New("Office 插件已停用")
	}
	a.officeRuntime.mu.Lock()
	defer a.officeRuntime.mu.Unlock()
	if a.officeRuntime.config == nil {
		return collaboraConfiguration{}, errors.New("本地 Office 引擎尚未启动")
	}
	if a.officeRuntime.done != nil {
		select {
		case <-a.officeRuntime.done:
			return collaboraConfiguration{}, errors.New("本地 Office 引擎已停止，请重新打开文档")
		default:
		}
	}
	return *a.officeRuntime.config, nil
}
func collaboraExtension(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".docx", ".xlsx", ".pptx", ".doc", ".xls", ".ppt", ".odt", ".ods", ".odp":
		return true
	}
	return false
}
func (a *App) collaboraStatus(w http.ResponseWriter, r *http.Request) {
	err := a.ensureOfficeEngine(r.Context())
	if err != nil {
		jsonOut(w, 200, map[string]any{"enabled": false, "builtin": true, "message": err.Error()})
		return
	}
	jsonOut(w, 200, map[string]any{"enabled": true, "builtin": true})
}

func collaboraDiscovery(ctx context.Context, cfg collaboraConfiguration, ext string, write bool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", cfg.Internal.String()+"/hosting/discovery", nil)
	if err != nil {
		return "", err
	}
	client := http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("Collabora discovery redirects are disabled")
	}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("本地 Office 引擎连接中断，请重新打开文档")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("Collabora discovery 请求失败")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return "", errors.New("Collabora discovery 响应无效或过大")
	}
	var discovery struct {
		Zones []struct {
			Apps []struct {
				Actions []struct {
					Name string `xml:"name,attr"`
					Ext  string `xml:"ext,attr"`
					URL  string `xml:"urlsrc,attr"`
				} `xml:"action"`
			} `xml:"app"`
		} `xml:"net-zone"`
	}
	if xml.Unmarshal(b, &discovery) != nil {
		return "", errors.New("Collabora discovery XML 无效")
	}
	wanted := "view"
	if write {
		wanted = "edit"
	}
	for _, zone := range discovery.Zones {
		for _, app := range zone.Apps {
			for _, action := range app.Actions {
				if action.Ext != strings.TrimPrefix(ext, ".") || action.Name != wanted {
					continue
				}
				u, e := url.Parse(action.URL)
				if e != nil {
					continue
				}
				editorPath := u.Path
				if cfg.Public.Path != "" {
					editorPath = strings.TrimPrefix(editorPath, cfg.Public.Path)
				}
				if u.User != nil || !strings.HasPrefix(editorPath, "/browser/") || !strings.HasSuffix(u.Path, "/cool.html") {
					continue
				}
				// Discovery may advertise an internal hostname. Only its editor path is
				// used; navigation is pinned to the operator-configured public origin.
				u.Scheme = cfg.Public.Scheme
				u.Host = cfg.Public.Host
				u.Path = cfg.Public.Path + editorPath
				u.RawPath = ""
				q := u.Query()
				for k, values := range q {
					if strings.ContainsAny(strings.Join(values, ""), "<>") {
						q.Del(k)
					}
				}
				u.RawQuery = q.Encode()
				u.Fragment = ""
				return u.String(), nil
			}
		}
	}
	return "", errors.New("Collabora 不支持此文件格式的所选模式")
}

type officeDocumentRequest struct {
	Path      string           `json:"path"`
	Source    string           `json:"source"`
	Root      string           `json:"root"`
	ReadOnly  bool             `json:"readOnly"`
	Origin    string           `json:"knowledgeOrigin"`
	Workspace string           `json:"knowledgeWorkspace"`
	Hash      string           `json:"hash"`
	Changes   []map[string]any `json:"changes"`
	Offset    int              `json:"offset"`
}

func (a *App) officeDocumentArea(in officeDocumentRequest) (knowledgeArea, error) {
	if safePath(in.Path) != nil || !collaboraExtension(in.Path) {
		return knowledgeArea{}, errors.New("Office 文件路径无效或为备份文件")
	}
	return a.knowledgeViewerArea(in.Root, in.Source, in.Origin, in.Workspace)
}
func collaboraAreaWritable(ar knowledgeArea) bool {
	return (ar.source == "" && ar.root == "workspace") || ar.source != "" && ar.src.RW && (ar.kind == "local" || ar.kind == "ssh" || ar.kind == "sftp" || ar.kind == "workspace-sftp" || ar.kind == "relay")
}
func collaboraKey(ar knowledgeArea, p string) string {
	return hash([]byte(ar.scope + "\x00" + ar.origin + "\x00" + p))
}
func collaboraRandom() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

// Caller holds collabora.mu. Expiration bounds both capability lifetime and RAM.
func (a *App) collaboraPrune() {
	now := time.Now()
	for id, s := range a.collabora.sessions {
		if now.After(s.Expires) {
			delete(a.collabora.sessions, id)
		}
	}
	used := map[*collaboraDocument]bool{}
	for _, s := range a.collabora.sessions {
		used[s.Doc] = true
	}
	for id, d := range a.collabora.docs {
		if !used[d] {
			delete(a.collabora.docs, id)
		}
	}
}
func (a *App) collaboraOpen(w http.ResponseWriter, r *http.Request) {
	var in officeDocumentRequest
	if e := decode(w, r, &in); e != nil {
		fail(w, 400, e)
		return
	}
	if e := a.ensureOfficeEngine(r.Context()); e != nil {
		fail(w, 503, e)
		return
	}
	cfg, e := a.collaboraConfig()
	if e != nil {
		fail(w, 503, e)
		return
	}
	ar, e := a.officeDocumentArea(in)
	if e != nil {
		fail(w, 400, e)
		return
	}
	lock := a.readLockState()
	if lock.Locked {
		fail(w, 409, errors.New("工作台已锁定"))
		return
	}
	write := !in.ReadOnly && in.Origin == "" && collaboraAreaWritable(ar)
	action, e := collaboraDiscovery(r.Context(), cfg, strings.ToLower(path.Ext(in.Path)), write)
	if e != nil {
		fail(w, 502, e)
		return
	}
	b, e := a.knowledgeAreaRead(r.Context(), ar, in.Path, maxRawFile)
	if e != nil {
		fail(w, 400, e)
		return
	}
	a.collabora.mu.Lock()
	defer a.collabora.mu.Unlock()
	a.collaboraPrune()
	if len(a.collabora.sessions) >= 512 {
		fail(w, 429, errors.New("Office 会话过多，请稍后重试"))
		return
	}
	if a.collabora.docs == nil {
		a.collabora.docs = map[string]*collaboraDocument{}
		a.collabora.sessions = map[string]*collaboraSession{}
	}
	id := collaboraKey(ar, in.Path)
	d := a.collabora.docs[id]
	if d == nil || d.Hash != hash(b) {
		// A fresh broker identity reloads external AI changes. Existing tokens
		// retain their old version and cannot overwrite that newer document.
		d = &collaboraDocument{Area: ar, Path: in.Path, ID: id + "-" + collaboraRandom()[:16], Hash: hash(b), Stamp: time.Now().UTC().Format(time.RFC3339Nano)}
		a.collabora.docs[id] = d
	}
	parent := r.Header.Get("Origin")
	if parent == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		parent = scheme + "://" + r.Host
	}
	token := collaboraRandom()
	s := &collaboraSession{ID: collaboraRandom(), TokenHash: hash([]byte(token)), Config: cfg.Fingerprint, ParentOrigin: parent, Doc: d, Write: write, Expires: time.Now().Add(8 * time.Hour), LockGen: lock.Gen}
	proxyToken := collaboraRandom()
	s.ProxyTokenHash = hash([]byte(proxyToken))
	a.collabora.sessions[s.ID] = s
	http.SetCookie(w, &http.Cookie{Name: officeEngineCookie, Value: s.ID + "." + proxyToken, Path: officeEnginePrefix + "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: s.Expires})
	u, _ := url.Parse(action)
	q := u.Query()
	q.Set("WOPISrc", cfg.Callback.String()+"/wopi/files/"+d.ID)
	q.Set("lang", "zh-CN")
	q.Set("closebutton", "1")
	u.RawQuery = q.Encode()
	jsonOut(w, 200, map[string]any{"id": s.ID, "url": u.String(), "origin": parent, "accessToken": token, "accessTokenTtl": s.Expires.UnixMilli(), "readOnly": !write, "canWrite": in.Origin == "" && collaboraAreaWritable(ar), "hash": d.Hash})
}
func (a *App) collaboraSessionCurrent(s *collaboraSession) bool {
	cfg, e := a.collaboraConfig()
	lock := a.readLockState()
	return e == nil && cfg.Fingerprint == s.Config && !lock.Locked && lock.Gen == s.LockGen && time.Now().Before(s.Expires) && a.knowledgeAreaCurrent(s.Doc.Area)
}
func (a *App) collaboraHeartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active bool `json:"active"`
		Dirty  bool `json:"dirty"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, 400, e)
		return
	}
	a.collabora.mu.Lock()
	defer a.collabora.mu.Unlock()
	s := a.collabora.sessions[r.PathValue("id")]
	if s == nil || !a.collaboraSessionCurrent(s) {
		fail(w, 409, errors.New("Office 会话已失效，请重新打开"))
		return
	}
	if s.Write {
		s.Dirty = in.Dirty
		if in.Active || in.Dirty {
			s.ActiveUntil = time.Now().Add(45 * time.Second)
		} else {
			s.ActiveUntil = time.Time{}
		}
	}
	jsonOut(w, 200, map[string]any{"hash": s.Doc.Hash, "reloadEditor": s.Doc.Outdated})
}

// Viewing never reserves a write lease. A recently active editor or unsaved
// changes do; the host requests a save on idle before allowing AI edits.
func (a *App) collaboraBusy(ar knowledgeArea, p string) bool {
	a.collabora.mu.Lock()
	defer a.collabora.mu.Unlock()
	key := collaboraKey(ar, p)
	now := time.Now()
	for _, s := range a.collabora.sessions {
		if s.Write && now.Before(s.Expires) && now.Before(s.ActiveUntil) && collaboraKey(s.Doc.Area, s.Doc.Path) == key {
			return true
		}
	}
	return false
}

// Existing native Office write tools share the same human activity protection.
func (a *App) collaboraWriteError(p, source string) error {
	ar, e := a.knowledgeViewerArea("workspace", strings.TrimPrefix(source, "source:"), "", "")
	if e == nil && a.collaboraBusy(ar, p) {
		return errors.New("人工正在编辑 Office 文档，请等待空闲保存后重试")
	}
	return nil
}
func (a *App) collaboraWOPI(w http.ResponseWriter, r *http.Request) {
	if a.runtimeStopping.Load() {
		fail(w, http.StatusServiceUnavailable, errors.New("Aide is stopping"))
		return
	}
	a.collabora.mu.Lock()
	defer a.collabora.mu.Unlock()
	var s *collaboraSession
	token := r.URL.Query().Get("access_token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token != "" {
		digest := hash([]byte(token))
		for _, candidate := range a.collabora.sessions {
			if candidate.TokenHash == digest && candidate.Doc.ID == r.PathValue("id") {
				s = candidate
				break
			}
		}
	}
	if s == nil || !a.collaboraSessionCurrent(s) {
		fail(w, 401, errors.New("Invalid Office capability"))
		return
	}
	d := s.Doc
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if time.Now().After(d.LockUntil) {
		d.Lock = ""
	}
	conflict := func() { w.Header().Set("X-WOPI-Lock", d.Lock); jsonOut(w, 409, map[string]any{"LOOLStatusCode": 1010}) }
	if strings.HasSuffix(r.URL.Path, "/contents") && r.Method == "POST" {
		if !s.Write || !collaboraAreaWritable(d.Area) {
			fail(w, 403, errors.New("Read-only document"))
			return
		}
		if op := r.Header.Get("X-WOPI-Override"); op != "" && op != "PUT" {
			fail(w, 501, errors.New("Unsupported WOPI operation"))
			return
		}
		if d.Lock != "" && d.Lock != r.Header.Get("X-WOPI-Lock") {
			conflict()
			return
		}
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRawFile))
		if e != nil {
			fail(w, 413, errors.New("Office file too large"))
			return
		}
		if len(b) == 0 {
			fail(w, 400, errors.New("Empty Office file"))
			return
		}
		switch strings.ToLower(path.Ext(d.Path)) {
		case ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp":
			if e = checkOfficeArchive(b); e != nil {
				fail(w, 400, e)
				return
			}
		}
		a.filesMu.Lock()
		defer a.filesMu.Unlock()
		mutation, finish, e := a.beginMessageMutation(ctx, s.LockGen)
		if e != nil {
			fail(w, 409, e)
			return
		}
		defer finish()
		fs, e := a.streamTransferFS(mutation, checkoutSource(d.Area), true)
		if e != nil {
			fail(w, 403, e)
			return
		}
		defer fs.Close()
		guard := func() error {
			if !a.collaboraSessionCurrent(s) {
				return errors.New("Office source changed")
			}
			current, exists, e := checkoutRead(fs, d.Path, false)
			if e != nil || !exists || hash(current) != d.Hash {
				return errors.New("Office version conflict")
			}
			return nil
		}
		if e = guard(); e != nil {
			conflict()
			return
		}
		if stamp := r.Header.Get("X-LOOL-WOPI-Timestamp"); stamp != "" && stamp != d.Stamp {
			conflict()
			return
		}
		if e = fs.writeAtomicChecked(d.Path, b, false, guard); e != nil {
			conflict()
			return
		}
		d.Hash = hash(b)
		d.Stamp = time.Now().UTC().Format(time.RFC3339Nano)
		w.Header().Set("X-WOPI-ItemVersion", d.Hash)
		jsonOut(w, 200, map[string]string{"LastModifiedTime": d.Stamp})
		return
	}
	if r.Method == "POST" {
		if !s.Write {
			fail(w, 403, errors.New("Read-only document"))
			return
		}
		lock := r.Header.Get("X-WOPI-Lock")
		if len(lock) > 1024 {
			fail(w, 400, errors.New("Lock too large"))
			return
		}
		switch r.Header.Get("X-WOPI-Override") {
		case "LOCK":
			old := r.Header.Get("X-WOPI-OldLock")
			if lock == "" || d.Lock != "" && d.Lock != lock && d.Lock != old {
				conflict()
				return
			}
			d.Lock = lock
			d.LockUntil = time.Now().Add(30 * time.Minute)
		case "REFRESH_LOCK":
			if d.Lock == "" || d.Lock != lock {
				conflict()
				return
			}
			d.LockUntil = time.Now().Add(30 * time.Minute)
		case "UNLOCK":
			if d.Lock == "" || d.Lock != lock {
				conflict()
				return
			}
			d.Lock = ""
		case "GET_LOCK":
			w.Header().Set("X-WOPI-Lock", d.Lock)
		default:
			fail(w, 501, errors.New("Unsupported WOPI operation"))
			return
		}
		w.WriteHeader(200)
		return
	}
	if r.Method != "GET" {
		fail(w, 405, errors.New("Method not allowed"))
		return
	}
	b, e := a.knowledgeAreaRead(ctx, d.Area, d.Path, maxRawFile)
	if e != nil {
		fail(w, 404, e)
		return
	}
	if !a.collaboraSessionCurrent(s) {
		fail(w, 401, errors.New("Office source changed"))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/contents") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(b)
		return
	}
	jsonOut(w, 200, map[string]any{"BaseFileName": path.Base(d.Path), "OwnerId": "aide", "UserId": "aide", "UserFriendlyName": "Aide", "Size": len(b), "Version": hash(b), "LastModifiedTime": d.Stamp, "UserCanWrite": s.Write, "ReadOnly": !s.Write, "UserCanNotWriteRelative": true, "SupportsUpdate": true, "SupportsLocks": true, "PostMessageOrigin": s.ParentOrigin, "EnableOwnerTermination": false})
}
