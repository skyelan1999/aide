package server

// WebAuthn challenge 会话 key 回归测试。
//
// 背景根因（go-webauthn v0.18.2）：SessionData.Challenge 是 string，库在
// registration.go:125 / login.go:142 已将其赋值为 Response.Challenge.String()，
// 即对原始 challenge 字节做单层 base64url 编码后的字符串——与 JSON 下发给前端、
// 前端 finish 时原样回传的值严格一致。旧代码又对该字符串二次 base64 编码（双重编码），
// 导致存进 sessions map 的 key 与前端回传值永远对不上，finish 一律 400"会话无效"。
//
// 这些测试在 manager / handler 层复现 start→finish 链路，断言：
//  1. start 返回给前端的 challenge 字符串，正是 sessions map 里的 key；
//  2. 旧的"双重编码" key 不再出现；
//  3. 用正确 challenge 调 finish（空 body）返回解析/格式错误，而非"会话无效"。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestWebAuthnManagerCloseStopsPruner(t *testing.T) {
	m := newWebAuthnManager(t.TempDir())
	if !m.enabled() {
		t.Fatal("WebAuthn manager is unavailable")
	}
	done := m.doneCh
	m.Close()
	select {
	case <-done:
	default:
		t.Fatal("Close returned before the challenge cleanup worker stopped")
	}
	m.Close() // App.Close and test cleanup may both own shutdown.
}

// newWebAuthnTestApp 构造一个最小可用 App：启用 WebAuthn 管理器 + 已知登录密码。
func newWebAuthnTestApp(t *testing.T, dataDir string) *App {
	t.Helper()
	m := newWebAuthnManager(dataDir)
	t.Cleanup(m.Close)
	if !m.enabled() {
		t.Fatal("WebAuthn 管理器未启用（webauthn.New 失败）")
	}
	return &App{
		webAuthn: m,
		settings: Settings{UserPasswordHash: mustHashPassword("webauthn-test-pw")},
	}
}

// doubleEncodedKey 复现旧 bug：对 session.Challenge（已是单层 base64url 字符串）
// 再做一次 RawURLEncoding 编码，用于断言这个错误 key 不再被使用。
func doubleEncodedKey(single string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(single))
}

// readChallenge 从 start 的 JSON 响应里取出下发给前端的 challenge 字段。
func readChallenge(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("start 响应不是合法 JSON: %v, body=%s", err, rec.Body.String())
	}
	ch, _ := body["challenge"].(string)
	if ch == "" {
		t.Fatalf("start 响应缺少 challenge 字段: %s", rec.Body.String())
	}
	return ch
}

// finishErrText 调 finish 并返回 (status, errorMessage)。
func finishErrText(t *testing.T, handler http.HandlerFunc, target string, body []byte) (int, string) {
	return finishErrTextForOrigin(t, handler, target, body, "9999")
}

func finishErrTextForOrigin(t *testing.T, handler http.HandlerFunc, target string, body []byte, port string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	setWebAuthnTestOrigin(req, port)
	rec := httptest.NewRecorder()
	handler(rec, req)
	var parsed struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, parsed.Error
}

func setWebAuthnTestOrigin(req *http.Request, port string) {
	req.Host = "localhost:" + port
	req.Header.Set("Origin", "https://localhost:"+port)
}

func TestWebAuthnOriginAllowsDynamicLocalPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/register/start", nil)
	setWebAuthnTestOrigin(req, "9999")
	got, ok := webAuthnOrigin(req)
	if !ok || got != "https://localhost:9999" {
		t.Fatalf("dynamic localhost origin rejected: origin=%q ok=%v", got, ok)
	}

	for _, tc := range []struct {
		name, origin, host string
	}{
		{name: "remote hostname", origin: "https://example.com:9999", host: "example.com:9999"},
		{name: "origin host mismatch", origin: "https://localhost:9998", host: "localhost:9999"},
		{name: "insecure scheme with malformed authority", origin: "file://localhost", host: "localhost"},
		{name: "path in origin", origin: "https://localhost:9999/path", host: "localhost:9999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			if got, ok := webAuthnOrigin(r); ok {
				t.Fatalf("unsafe origin accepted: %q", got)
			}
		})
	}
}

// TestRegisterStartChallengeKeyMatch 验证 register/start 存会话的 key == 下发 challenge。
func TestRegisterStartChallengeKeyMatch(t *testing.T) {
	a := newWebAuthnTestApp(t, t.TempDir())

	// start: POST /api/webauthn/register/start {oldPassword}
	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/register/start",
		strings.NewReader(`{"oldPassword":"webauthn-test-pw"}`))
	setWebAuthnTestOrigin(req, "9999")
	rec := httptest.NewRecorder()
	a.webAuthnRegisterStart(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register/start 期望 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	ch := readChallenge(t, rec)

	// 关键断言：下发给前端的 challenge 正是 sessions map 的 key。
	a.webAuthn.mu.Lock()
	_, ok := a.webAuthn.sessions[ch]
	_, doubleOK := a.webAuthn.sessions[doubleEncodedKey(ch)]
	boundOrigin := a.webAuthn.origins[ch]
	total := len(a.webAuthn.sessions)
	a.webAuthn.mu.Unlock()

	if !ok {
		t.Fatalf("下发 challenge %q 未能命中 sessions 会话（双重编码 bug 仍在）", ch)
	}
	if boundOrigin != "https://localhost:9999" {
		t.Fatalf("expected start to bind actual host port origin, got %q", boundOrigin)
	}
	if doubleOK {
		t.Fatalf("旧的双重编码 key 仍存在于 sessions（说明修复未生效）")
	}
	if total != 1 {
		t.Fatalf("期望恰好 1 条会话，实际 %d", total)
	}
	status, msg := finishErrTextForOrigin(t, a.webAuthnRegisterFinish,
		"/api/webauthn/register/finish?challenge="+ch, []byte{}, "10000")
	if status != http.StatusBadRequest || !strings.Contains(msg, "来源已变化") {
		t.Fatalf("跨端口 finish 应拒绝且指出来源变化，status=%d msg=%q", status, msg)
	}

	// finish 用正确 challenge + 空 body：应命中会话后在解析阶段 400，而非"会话无效"。
	status, msg = finishErrText(t, a.webAuthnRegisterFinish,
		"/api/webauthn/register/finish?challenge="+ch, []byte{})
	if status != http.StatusBadRequest {
		t.Fatalf("register/finish 期望 400（解析错误），实际 %d msg=%q", status, msg)
	}
	if strings.Contains(msg, "会话无效") || strings.Contains(msg, "已过期") {
		t.Fatalf("finish 仍报会话无效，说明会话未命中: %q", msg)
	}
}

// TestAssertionStartChallengeKeyMatch 验证 assertion/start 同样以正确 challenge 作 key。
func TestAssertionStartChallengeKeyMatch(t *testing.T) {
	a := newWebAuthnTestApp(t, t.TempDir())

	// assertion/start 在无凭证时走早退分支、不落会话；伪造一条凭证以进入 BeginLogin 路径。
	// 凭证对象本身只需非零：BeginLogin 最终用 WithAllowedCredentials(allowList) 覆盖，
	// allowList 来自下面这条 webAuthnCredEntry.ID（合法 base64url）。
	credID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 32))
	a.webAuthn.mu.Lock()
	a.webAuthn.store.Credentials = []webAuthnCredEntry{
		{ID: credID, Name: "test-device", CreatedAt: 1, Credential: webauthn.Credential{}},
	}
	a.webAuthn.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/assertion/start", nil)
	setWebAuthnTestOrigin(req, "9999")
	rec := httptest.NewRecorder()
	a.webAuthnAssertionStart(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assertion/start 期望 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	ch := readChallenge(t, rec)

	a.webAuthn.mu.Lock()
	_, ok := a.webAuthn.sessions[ch]
	_, doubleOK := a.webAuthn.sessions[doubleEncodedKey(ch)]
	a.webAuthn.mu.Unlock()
	if !ok {
		t.Fatalf("assertion 下发 challenge %q 未能命中 sessions 会话", ch)
	}
	if doubleOK {
		t.Fatalf("assertion 旧的双重编码 key 仍存在")
	}

	// finish 用正确 challenge + 空 body：命中会话后在解析阶段 400，而非"会话无效"。
	status, msg := finishErrText(t, a.webAuthnAssertionFinish,
		"/api/webauthn/assertion/finish?challenge="+ch, []byte{})
	if status != http.StatusBadRequest {
		t.Fatalf("assertion/finish 期望 400（解析错误），实际 %d msg=%q", status, msg)
	}
	if strings.Contains(msg, "会话无效") || strings.Contains(msg, "已过期") {
		t.Fatalf("assertion finish 仍报会话无效: %q", msg)
	}
}
