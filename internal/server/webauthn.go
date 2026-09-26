package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Touch ID / WebAuthn 解锁：浏览器端 WebAuthn platform authenticator（macOS Touch ID）。
// 后端仅存公钥与签名计数，私钥永不离设备；锁屏解锁 = 通过断言后调用与密码解锁相同的前端 dismiss 逻辑。
//
// 端点（全部挂在既有 Bearer 中间件之后）：
//   POST /api/webauthn/register/start      {oldPassword}        → PublicKeyCredentialCreationOptions
//   POST /api/webauthn/register/finish     ?label=...  body=raw → {ok,id,name}
//   GET  /api/webauthn/credentials                             → [{id,name,createdAt,signCount}]
//   PUT  /api/webauthn/credentials/{id}     {label}              → {ok}
//   DELETE /api/webauthn/credentials/{id}   {oldPassword}        → {ok}
//   POST /api/webauthn/assertion/start                          → PublicKeyCredentialRequestOptions
//   POST /api/webauthn/assertion/finish     ?challenge=... body=raw → {ok}

const (
	webAuthnCredsFileName = "webauthn-credentials.json"
	webAuthnUserHandleStr = "aide-local-v1" // userHandle，固定 ≤64 字节
	webAuthnSessionTTL    = 120 * time.Second
)

// webAuthnCredEntry 一条已注册的平台认证器凭证（公钥材料，无私钥）。
type webAuthnCredEntry struct {
	ID         string              `json:"id"` // base64url credential ID
	Name       string              `json:"name"`
	CreatedAt  int64               `json:"createdAt"`
	Credential webauthn.Credential `json:"credential"`
}

type webAuthnStore struct {
	Version     int                 `json:"version"`
	RPID        string              `json:"rpId"`
	Credentials []webAuthnCredEntry `json:"credentials"`
}

type webAuthnManager struct {
	mu       sync.Mutex
	wa       *webauthn.WebAuthn
	dataPath string
	store    webAuthnStore
	sessions map[string]*webauthn.SessionData // challenge(base64url) → session
}

// localWebAuthnUser 实现 webauthn.User 接口（单机单用户，固定 userHandle）。
type localWebAuthnUser struct {
	creds []webauthn.Credential
}

func (u localWebAuthnUser) WebAuthnID() []byte                         { return []byte(webAuthnUserHandleStr) }
func (u localWebAuthnUser) WebAuthnName() string                       { return "aide-local" }
func (u localWebAuthnUser) WebAuthnDisplayName() string                { return "aide local" }
func (u localWebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// newWebAuthnManager 初始化 WebAuthn 管理器；RPID 固定 localhost（IP 字面量不合法）。
// 初始化失败（如无密码时仍可用，但不阻塞启动）不返回错误，仅 wa=nil。
func newWebAuthnManager(dataPath string) *webAuthnManager {
	m := &webAuthnManager{
		dataPath: dataPath,
		sessions: map[string]*webauthn.SessionData{},
	}
	// RPID 固定 localhost（IP 字面量不合法）。
	// Origin 白名单同时覆盖 http/https × localhost/127.0.0.1：页面已切 HTTPS（自签），
	// 但浏览器旧标签缓存仍可能以 http:// 发起 register/finish，缺 https origin 会被
	// WebAuthn 以 "origin not allowed" 拒绝。端口取宿主映射 AIDE_PORT（默认 8097）。
	port := env("AIDE_PORT", "8097")
	cfg := &webauthn.Config{
		RPID:          "localhost",
		RPDisplayName: "aide",
		RPOrigins: []string{
			"https://localhost:" + port,
			"https://127.0.0.1:" + port,
			"http://localhost:" + port,
			"http://127.0.0.1:" + port,
		},
	}
	wa, err := webauthn.New(cfg)
	if err != nil {
		// 不阻塞启动；webAuthnReady=false 时前端隐藏按钮。
		return m
	}
	m.wa = wa
	m.loadLocked()
	return m
}

func (m *webAuthnManager) enabled() bool { return m != nil && m.wa != nil }

// hasPlatformCredential 是否已注册至少一把平台凭证（只读布尔，不泄露凭证细节）。
func (m *webAuthnManager) hasPlatformCredential() bool {
	if m == nil || !m.enabled() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.store.Credentials) > 0
}

func (m *webAuthnManager) loadLocked() {
	m.store = webAuthnStore{Version: 1, RPID: "localhost"}
	b, err := os.ReadFile(WebAuthnCredsPath(m.dataPath))
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &m.store)
}

func (m *webAuthnManager) saveLocked() error {
	path := WebAuthnCredsPath(m.dataPath)
	return atomicJSON(path, &m.store)
}

// pruneSessionsLocked 惰性清理过期 challenge 会话。
func (m *webAuthnManager) pruneSessionsLocked() {
	now := time.Now()
	for k, s := range m.sessions {
		if !s.Expires.IsZero() && s.Expires.Before(now) {
			delete(m.sessions, k)
		}
	}
}

func (m *webAuthnManager) userLocked() localWebAuthnUser {
	creds := make([]webauthn.Credential, 0, len(m.store.Credentials))
	for _, c := range m.store.Credentials {
		creds = append(creds, c.Credential)
	}
	return localWebAuthnUser{creds: creds}
}

// findCredLocked 按 base64url credential ID 查找。
func (m *webAuthnManager) findCredLocked(idB64 string) *webAuthnCredEntry {
	for i := range m.store.Credentials {
		if m.store.Credentials[i].ID == idB64 {
			return &m.store.Credentials[i]
		}
	}
	return nil
}

// ── 端点：POST /api/webauthn/register/start ──
func (a *App) webAuthnRegisterStart(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	var in struct {
		OldPassword string `json:"oldPassword"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if valid, _ := VerifyPassword(in.OldPassword, hash); !valid {
		fail(w, 401, errors.New("密码错误"))
		return
	}

	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	a.webAuthn.pruneSessionsLocked()
	user := a.webAuthn.userLocked()

	// excludeCredentials：避免同一台机器重复登记。
	excludeList := []protocol.CredentialDescriptor{}
	for _, c := range a.webAuthn.store.Credentials {
		credID, _ := base64.RawURLEncoding.DecodeString(c.ID)
		excludeList = append(excludeList, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: credID,
		})
	}

	creation, session, err := a.webAuthn.wa.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			AuthenticatorAttachment: protocol.Platform,
			UserVerification:        protocol.VerificationPreferred,
			ResidentKey:             protocol.ResidentKeyRequirementPreferred,
			RequireResidentKey:      nil,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(excludeList),
	)
	if err != nil {
		fail(w, 500, err)
		return
	}
	// 以 challenge 的 base64url 字符串为 key 存会话。
	//
	// 根因（go-webauthn v0.18.2 SessionData 语义）：SessionData.Challenge 是 string，
	// 库在 registration.go:125 已将其赋值为 creation.Response.Challenge.String()——
	// 即对原始 challenge 字节做单层 base64url 编码后的字符串，与 JSON 下发给前端、
	// 前端 finish 时原样回传的值严格一致。旧代码又对 session.Challenge 这个字符串
	// 再做一次 base64.RawURLEncoding.EncodeToString([]byte(...))，等于对"已编码的
	// base64 文本"再编码一次（双重编码），导致 map key 与前端回传值永远对不上，
	// finish 一律 400"会话无效或已过期"。这里直接用 Response.Challenge.String() 作 key，
	// 保证与下发值逐字节一致。
	challengeKey := creation.Response.Challenge.String()
	a.webAuthn.sessions[challengeKey] = session
	jsonOut(w, 200, creation.Response)
}

// ── 端点：POST /api/webauthn/register/finish?label=... ──
func (a *App) webAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	challengeB64 := r.URL.Query().Get("challenge")
	label := strings.TrimSpace(r.URL.Query().Get("label"))
	if label == "" {
		label = "MacBook 触控 ID"
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}

	a.webAuthn.mu.Lock()
	session, ok := a.webAuthn.sessions[challengeB64]
	if !ok {
		a.webAuthn.mu.Unlock()
		fail(w, 400, errors.New("会话无效或已过期，请重新开始"))
		return
	}
	delete(a.webAuthn.sessions, challengeB64) // 一次性
	user := a.webAuthn.userLocked()
	a.webAuthn.mu.Unlock()

	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		fail(w, 400, err)
		return
	}
	cred, err := a.webAuthn.wa.CreateCredential(user, *session, parsed)
	if err != nil {
		fail(w, 400, err)
		return
	}

	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	// 幂等：同 ID 已存在则更新而非追加。
	if existing := a.webAuthn.findCredLocked(credID); existing != nil {
		existing.Credential = *cred
		existing.Name = label
	} else {
		a.webAuthn.store.Credentials = append(a.webAuthn.store.Credentials, webAuthnCredEntry{
			ID:         credID,
			Name:       label,
			CreatedAt:  time.Now().Unix(),
			Credential: *cred,
		})
	}
	if err := a.webAuthn.saveLocked(); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "id": credID, "name": label})
}

// ── 端点：GET /api/webauthn/credentials ──
func (a *App) webAuthnListCredentials(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		jsonOut(w, 200, []any{})
		return
	}
	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	out := make([]map[string]any, 0, len(a.webAuthn.store.Credentials))
	for _, c := range a.webAuthn.store.Credentials {
		out = append(out, map[string]any{
			"id":        c.ID,
			"name":      c.Name,
			"createdAt": c.CreatedAt,
			"signCount": c.Credential.Authenticator.SignCount,
		})
	}
	jsonOut(w, 200, out)
}

// ── 端点：PUT /api/webauthn/credentials/{id} ──
func (a *App) webAuthnRenameCredential(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	id := r.PathValue("id")
	var in struct {
		Label string `json:"label"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	c := a.webAuthn.findCredLocked(id)
	if c == nil {
		fail(w, 404, errors.New("凭证不存在"))
		return
	}
	c.Name = strings.TrimSpace(in.Label)
	if c.Name == "" {
		c.Name = "触控 ID 设备"
	}
	if err := a.webAuthn.saveLocked(); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// ── 端点：DELETE /api/webauthn/credentials/{id} ──
func (a *App) webAuthnDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	id := r.PathValue("id")
	var in struct {
		OldPassword string `json:"oldPassword"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if valid, _ := VerifyPassword(in.OldPassword, hash); !valid {
		fail(w, 401, errors.New("密码错误"))
		return
	}
	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	for i := range a.webAuthn.store.Credentials {
		if a.webAuthn.store.Credentials[i].ID == id {
			a.webAuthn.store.Credentials = append(a.webAuthn.store.Credentials[:i], a.webAuthn.store.Credentials[i+1:]...)
			if err := a.webAuthn.saveLocked(); err != nil {
				fail(w, 500, err)
				return
			}
			jsonOut(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, errors.New("凭证不存在"))
}

// ── 端点：POST /api/webauthn/assertion/start ──
func (a *App) webAuthnAssertionStart(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	a.webAuthn.mu.Lock()
	defer a.webAuthn.mu.Unlock()
	a.webAuthn.pruneSessionsLocked()
	user := a.webAuthn.userLocked()
	if len(a.webAuthn.store.Credentials) == 0 {
		// 未注册任何凭证：返回空 allowCredentials，前端据此隐藏按钮。
		jsonOut(w, 200, map[string]any{"challenge": "", "allowCredentials": []any{}, "rpId": "localhost"})
		return
	}
	allowList := []protocol.CredentialDescriptor{}
	for _, c := range a.webAuthn.store.Credentials {
		credID, _ := base64.RawURLEncoding.DecodeString(c.ID)
		allowList = append(allowList, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: credID,
		})
	}
	assertion, session, err := a.webAuthn.wa.BeginLogin(user,
		webauthn.WithAllowedCredentials(allowList),
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		fail(w, 500, err)
		return
	}
	// 同 register start：SessionData.Challenge 在 v0.18.2 已是单层 base64url 字符串
	// （login.go:142 赋值为 assertion.Response.Challenge.String()），必须直接用它作 key，
	// 不得再二次 base64 编码，否则前端回传值命中不了会话（双重编码 bug）。
	challengeKey := assertion.Response.Challenge.String()
	a.webAuthn.sessions[challengeKey] = session
	jsonOut(w, 200, assertion.Response)
}

// ── 端点：POST /api/webauthn/assertion/finish?challenge=... ──
func (a *App) webAuthnAssertionFinish(w http.ResponseWriter, r *http.Request) {
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("WebAuthn 不可用"))
		return
	}
	challengeB64 := r.URL.Query().Get("challenge")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}

	a.webAuthn.mu.Lock()
	session, ok := a.webAuthn.sessions[challengeB64]
	if !ok {
		a.webAuthn.mu.Unlock()
		fail(w, 400, errors.New("会话无效或已过期，请重试"))
		return
	}
	delete(a.webAuthn.sessions, challengeB64)
	user := a.webAuthn.userLocked()
	a.webAuthn.mu.Unlock()

	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		fail(w, 400, err)
		return
	}
	cred, err := a.webAuthn.wa.ValidateLogin(user, *session, parsed)
	if err != nil {
		fail(w, 401, errors.New("验证失败："+err.Error()))
		return
	}

	// 回写最新 signCount（ValidateLogin 返回的 credential 已含更新后的计数）。
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	a.webAuthn.mu.Lock()
	if entry := a.webAuthn.findCredLocked(credID); entry != nil {
		entry.Credential.Authenticator.SignCount = cred.Authenticator.SignCount
		_ = a.webAuthn.saveLocked()
	}
	a.webAuthn.mu.Unlock()

	jsonOut(w, 200, map[string]bool{"ok": true})
}
