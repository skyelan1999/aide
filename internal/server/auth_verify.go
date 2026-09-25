package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
)

// ── 统一本机主身份认证（#43）─────────────────────────────────────────────────
//
// POST /api/auth/verify  body {password?:string, assertion?:object, challenge?:string}
// 密码 或 WebAuthn 断言 二选一通过即返回 {ok:true, scope:"master"}，写审计日志。
// 断言成功的授权范围与输主密码完全一致（master scope），不扩大。
//   - 密码路径：校验账户密码 → 派生主密钥解锁凭证保险库 → 迁移暂存旧明文 key。
//   - 断言路径：复用 /api/webauthn/assertion/finish 同一校验流程；不接触主密钥，
//     仅证明本机用户在场（vault 解锁态以当前驻留主密钥为准，不新增能力）。
//   - 无密码用户（passwordless）：身份天然确认，vault 已由机器密钥自动解锁。

// verifyMasterIdentity 统一主身份认证入口。
func (a *App) verifyMasterIdentity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password  string          `json:"password"`
		Assertion json.RawMessage `json:"assertion"`
		Challenge string          `json:"challenge"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	havePw := strings.TrimSpace(in.Password) != ""
	haveAs := len(in.Assertion) > 0
	if !havePw && !haveAs {
		fail(w, 400, errors.New("需要密码或指纹断言"))
		return
	}

	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()

	if havePw {
		if hash == "" {
			a.vaultAudit("master-auth:passwordless")
			jsonOut(w, 200, map[string]any{"ok": true, "scope": "master", "unlocked": a.vaultIsUnlocked()})
			return
		}
		ok, _ := VerifyPassword(in.Password, hash)
		if !ok {
			a.vaultAudit("master-auth:failed:password")
			fail(w, 401, errors.New("密码错误"))
			return
		}
		a.mu.Lock()
		a.unlockVault(in.Password)
		a.migratePendingLegacyAPIKeyLocked()
		a.mu.Unlock()
		a.vaultAudit("master-auth:password")
		jsonOut(w, 200, map[string]any{"ok": true, "scope": "master", "unlocked": true})
		return
	}

	// WebAuthn 断言路径
	if !a.webAuthn.enabled() {
		fail(w, 503, errors.New("指纹不可用"))
		return
	}
	challengeB64 := strings.TrimSpace(in.Challenge)
	if challengeB64 == "" {
		var wrapper struct {
			Challenge string `json:"challenge"`
		}
		_ = json.Unmarshal(in.Assertion, &wrapper)
		challengeB64 = strings.TrimSpace(wrapper.Challenge)
	}
	if challengeB64 == "" {
		fail(w, 400, errors.New("缺少 challenge"))
		return
	}
	if err := a.validateAssertion(challengeB64, in.Assertion); err != nil {
		a.vaultAudit("master-auth:failed:webauthn")
		fail(w, 401, errors.New("指纹验证失败"))
		return
	}
	a.vaultAudit("master-auth:webauthn")
	jsonOut(w, 200, map[string]any{"ok": true, "scope": "master", "unlocked": a.vaultIsUnlocked()})
}

// validateAssertion 校验一次 WebAuthn 登录断言（与 /api/webauthn/assertion/finish 同一流程）。
// body 为浏览器端 assertionToJSON 产出的原始 JSON。
func (a *App) validateAssertion(challengeB64 string, body []byte) error {
	a.webAuthn.mu.Lock()
	session, ok := a.webAuthn.sessions[challengeB64]
	if !ok {
		a.webAuthn.mu.Unlock()
		return errors.New("会话无效或已过期")
	}
	delete(a.webAuthn.sessions, challengeB64)
	user := a.webAuthn.userLocked()
	a.webAuthn.mu.Unlock()

	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return err
	}
	cred, err := a.webAuthn.wa.ValidateLogin(user, *session, parsed)
	if err != nil {
		return err
	}
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	a.webAuthn.mu.Lock()
	if entry := a.webAuthn.findCredLocked(credID); entry != nil {
		entry.Credential.Authenticator.SignCount = cred.Authenticator.SignCount
		_ = a.webAuthn.saveLocked()
	}
	a.webAuthn.mu.Unlock()
	return nil
}
