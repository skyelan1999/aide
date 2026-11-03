package server

// A separate, opt-in HTTPS surface. The ordinary workbench token is never a
// credential on this listener, even when a client spoofs localhost in Host.
import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"aide/internal/messaging"
	qrcode "github.com/skip2/go-qrcode"
)

type lanIdentityKey struct{}
type lanSession struct {
	tokenHash, passwordHash string
	last, deadline          time.Time
	ctx                     context.Context
	cancel                  context.CancelFunc
}
type lanAccess struct {
	owner                string
	mu                   sync.Mutex
	sessions             map[string]*lanSession
	runtime              bool
	addresses            []string
	fingerprint, problem string
	certificate          *tls.Certificate
}

func lanAuthorized(r *http.Request) bool {
	_, ok := r.Context().Value(lanIdentityKey{}).(*lanSession)
	return ok
}
func (l *lanAccess) revoke() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.sessions {
		s.cancel()
	}
	l.sessions = nil
}
func (a *App) lanPolicy() (bool, int, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.RemoteAccessEnabled, a.settings.LockTimeoutSec, a.settings.UserPasswordHash, a.loadLockStateLocked().Locked
}
func (a *App) remoteAccessControl(w http.ResponseWriter, r *http.Request) {
	if lanAuthorized(r) {
		fail(w, 403, errors.New("请在本机设置中管理局域网授权"))
		return
	}
	if r.Method == http.MethodPut {
		var in struct {
			Enabled  bool   `json:"enabled"`
			Password string `json:"password"`
		}
		if err := decode(w, r, &in); err != nil {
			fail(w, 400, errors.New("无效授权请求"))
			return
		}
		_, _, hash, _ := a.lanPolicy()
		if in.Enabled {
			if hash == "" {
				fail(w, 400, errors.New("请先设置账户密码"))
				return
			}
			ok, _, err := a.verifyMasterPassword(in.Password, hash)
			if writeMasterPasswordCooldown(w, err) {
				return
			}
			if !ok {
				fail(w, 401, errors.New("密码错误"))
				return
			}
			a.lan.mu.Lock()
			ready := a.lan.runtime
			a.lan.mu.Unlock()
			if !ready {
				fail(w, 503, errors.New("局域网服务未就绪，请使用新版启动器重新启动"))
				return
			}
		}
		a.mu.Lock()
		if hash != a.settings.UserPasswordHash {
			a.mu.Unlock()
			fail(w, 409, errors.New("密码已变化，请重试"))
			return
		}
		next := a.settings
		next.RemoteAccessEnabled = in.Enabled
		if in.Enabled && next.LockTimeoutSec <= 0 {
			next.LockTimeoutSec = 300
		}
		err := atomicJSON(SettingsPath(a.dataPath), next)
		if err == nil {
			a.settings = next
		}
		a.mu.Unlock()
		if err != nil {
			fail(w, 500, err)
			return
		}
		a.lan.revoke()
		if !in.Enabled {
			messaging.ReleaseRemoteAccessLAN(os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), a.lanOwner())
			a.lan.mu.Lock()
			a.lan.addresses = nil
			a.lan.certificate = nil
			a.lan.mu.Unlock()
		}
	}
	enabled, timeout, _, _ := a.lanPolicy()
	a.lan.mu.Lock()
	addresses := append([]string{}, a.lan.addresses...)
	fingerprint, problem := a.lan.fingerprint, a.lan.problem
	a.lan.mu.Unlock()
	entries := []map[string]string{}
	if enabled {
		for _, address := range addresses {
			entry := address + "/lan-login.html"
			png, err := qrcode.Encode(entry, qrcode.Medium, 256)
			if err == nil {
				entries = append(entries, map[string]string{"url": entry, "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
			}
		}
	}
	jsonOut(w, 200, map[string]any{"enabled": enabled, "ready": enabled && len(entries) > 0, "entries": entries, "fingerprint": fingerprint, "problem": problem, "lockTimeoutSec": timeout})
}
func (a *App) lanOwner() string {
	a.lan.mu.Lock()
	defer a.lan.mu.Unlock()
	if a.lan.owner == "" {
		a.lan.owner = captureSecret()[:32]
	}
	return a.lan.owner
}

func lanRequestToken(r *http.Request) string {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = assistantSocketToken(r)
	}
	if token == "" && r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/events") || r.URL.Path == "/api/file/raw" || r.URL.Path == "/api/file/download" || r.URL.Path == "/api/file/stream") {
		token = r.URL.Query().Get("access_token")
	}
	return token
}
func lanOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	return e == nil && u.Scheme == "https" && u.Host == r.Host && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func (a *App) lanAllowedHost(r *http.Request) bool {
	a.lan.mu.Lock()
	defer a.lan.mu.Unlock()
	for _, address := range a.lan.addresses {
		u, e := url.Parse(address)
		if e == nil && u.Host == r.Host {
			return true
		}
	}
	return false
}
func (a *App) lanLogin(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, errors.New("无效登录请求"))
		return
	}
	enabled, timeout, hash, _ := a.lanPolicy()
	if !enabled || hash == "" || timeout <= 0 {
		fail(w, 403, errors.New("局域网访问未授权"))
		return
	}
	if in.Password == "" {
		fail(w, 401, errors.New("请输入账户密码"))
		return
	}
	lock := a.readLockState()
	ok, _, err := a.verifyMasterPassword(in.Password, hash)
	if writeMasterPasswordCooldown(w, err) {
		return
	}
	if !ok {
		fail(w, 401, errors.New("密码错误"))
		return
	}
	// Recheck after the expensive password derivation; never resurrect a revoked grant.
	a.mu.Lock()
	if !a.settings.RemoteAccessEnabled || a.settings.UserPasswordHash != hash {
		a.mu.Unlock()
		fail(w, 409, errors.New("访问授权已变化，请重新登录"))
		return
	}
	a.mu.Unlock()
	if lock.Locked {
		if err := a.unlockScreenState(&lock.Gen); err != nil {
			fail(w, 409, err)
			return
		}
	}
	token := captureSecret()
	now := time.Now()
	ctx, cancel := context.WithCancel(a.bgCtx)
	s := &lanSession{tokenHash: hashString(token), passwordHash: hash, last: now, deadline: now.Add(8 * time.Hour), ctx: ctx, cancel: cancel}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.settings.RemoteAccessEnabled || a.settings.UserPasswordHash != hash || a.loadLockStateLocked().Locked {
		cancel()
		fail(w, 409, errors.New("授权或锁屏状态已变化，请重新登录"))
		return
	}
	a.lan.mu.Lock()
	defer a.lan.mu.Unlock()
	if len(a.lan.sessions) >= 32 {
		cancel()
		fail(w, 429, errors.New("远程会话数量已达上限"))
		return
	}
	if a.lan.sessions == nil {
		a.lan.sessions = map[string]*lanSession{}
	}
	a.lan.sessions[s.tokenHash] = s
	jsonOut(w, 200, map[string]any{"token": token, "expiresAt": s.deadline, "lockTimeoutSec": timeout})
}
func hashString(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (a *App) lanAuthenticate(r *http.Request) *lanSession {
	enabled, timeout, hash, locked := a.lanPolicy()
	if !enabled || timeout <= 0 || hash == "" || locked {
		return nil
	}
	token := lanRequestToken(r)
	if len(token) != 64 {
		return nil
	}
	a.lan.mu.Lock()
	defer a.lan.mu.Unlock()
	s := a.lan.sessions[hashString(token)]
	if s == nil {
		return nil
	}
	now := time.Now()
	if s.passwordHash != hash || now.Sub(s.last) >= time.Duration(timeout)*time.Second || !now.Before(s.deadline) || s.ctx.Err() != nil {
		s.cancel()
		delete(a.lan.sessions, s.tokenHash)
		return nil
	}
	return s
}
func (a *App) lanHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		enabled, _, _, _ := a.lanPolicy()
		if !enabled {
			w.Header().Set("X-Aide-Auth-Required", "lan-password")
			fail(w, 403, errors.New("局域网访问未授权"))
			return
		}
		if r.TLS == nil {
			fail(w, 403, errors.New("局域网访问必须使用 HTTPS"))
			return
		}
		if !a.lanAllowedHost(r) || !lanOriginOK(r) {
			fail(w, 403, errors.New("局域网地址或来源不匹配"))
			return
		}
		if r.URL.Path == "/remote-login" && r.Method == http.MethodPost {
			a.lanLogin(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s := a.lanAuthenticate(r)
			if s == nil {
				w.Header().Set("X-Aide-Auth-Required", "lan-password")
				fail(w, 401, errors.New("请重新输入账户密码登录"))
				return
			}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			stop := context.AfterFunc(s.ctx, cancel)
			defer stop()
			r = r.WithContext(context.WithValue(ctx, lanIdentityKey{}, s))
			if r.URL.Path == "/api/remote-access/session" && r.Method == http.MethodGet {
				jsonOut(w, 200, map[string]bool{"ok": true})
				return
			}
			if r.URL.Path == "/api/remote-access/activity" && r.Method == http.MethodPost {
				a.lan.mu.Lock()
				s.last = time.Now()
				a.lan.mu.Unlock()
				jsonOut(w, 200, map[string]bool{"ok": true})
				return
			}
			if r.URL.Path == "/api/remote-access/logout" && r.Method == http.MethodPost {
				a.lan.mu.Lock()
				s.cancel()
				delete(a.lan.sessions, s.tokenHash)
				a.lan.mu.Unlock()
				jsonOut(w, 200, map[string]bool{"ok": true})
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/debug/") {
				fail(w, 403, errors.New("诊断接口仅支持本机"))
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		a.Handler().ServeHTTP(w, r)
	})
}

func (a *App) expireLANSessions() {
	enabled, timeout, hash, locked := a.lanPolicy()
	a.lan.mu.Lock()
	defer a.lan.mu.Unlock()
	now := time.Now()
	for k, s := range a.lan.sessions {
		if !enabled || locked || timeout <= 0 || hash != s.passwordHash || now.Sub(s.last) >= time.Duration(timeout)*time.Second || !now.Before(s.deadline) {
			s.cancel()
			delete(a.lan.sessions, k)
		}
	}
}
func (a *App) startLANAccessService() error {
	listener, err := net.Listen("tcp", env("AIDE_LAN_ADDR", "127.0.0.1:45842"))
	if err != nil {
		return err
	}
	config := tlsConfig()
	config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		enabled, _, _, _ := a.lanPolicy()
		a.lan.mu.Lock()
		defer a.lan.mu.Unlock()
		if !enabled || a.lan.certificate == nil {
			return nil, errors.New("LAN access disabled or not ready")
		}
		return a.lan.certificate, nil
	}
	srv := &http.Server{Handler: a.lanHandler(), TLSConfig: config, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	a.lan.mu.Lock()
	a.lan.runtime = true
	a.lan.mu.Unlock()
	a.background(func() { _ = srv.Serve(tls.NewListener(listener, config)) })
	a.background(func() { <-a.bgCtx.Done(); _ = srv.Close() })
	a.background(func() {
		defer a.lan.revoke()
		defer messaging.ReleaseRemoteAccessLAN(os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), a.lanOwner())
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		next := time.Time{}
		active := false
		for {
			select {
			case <-a.bgCtx.Done():
				return
			case <-ticker.C:
				a.expireLANSessions()
				enabled, _, _, _ := a.lanPolicy()
				if !enabled {
					if active {
						messaging.ReleaseRemoteAccessLAN(os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), a.lanOwner())
						a.lan.mu.Lock()
						a.lan.addresses = nil
						a.lan.certificate = nil
						a.lan.mu.Unlock()
						active = false
					}
					continue
				}
				active = true
				if time.Now().Before(next) {
					continue
				}
				next = time.Now().Add(10 * time.Second)
				ctx, cancel := context.WithTimeout(a.bgCtx, 2*time.Second)
				lease, e := messaging.RemoteAccessLAN(ctx, os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), a.lanOwner(), false)
				cancel()
				var cert *tls.Certificate
				fingerprint := ""
				if e == nil {
					cert, fingerprint, e = ensureLANCertificate(CertsDir(a.dataPath), lease.Addresses)
				}
				a.mu.Lock()
				stillEnabled := a.settings.RemoteAccessEnabled
				if !stillEnabled {
					a.mu.Unlock()
					messaging.ReleaseRemoteAccessLAN(os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), a.lanOwner())
					continue
				}
				a.lan.mu.Lock()
				if e != nil {
					a.lan.problem = "局域网桥未就绪，请使用新版启动器启动，并检查网络与防火墙。"
					a.lan.addresses = nil
					a.lan.certificate = nil
				} else {
					a.lan.addresses = lease.Addresses
					a.lan.certificate = cert
					a.lan.fingerprint = fingerprint
					a.lan.problem = ""
				}
				a.lan.mu.Unlock()
				a.mu.Unlock()
			}
		}
	})
	return nil
}
