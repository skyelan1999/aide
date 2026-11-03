package server

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const lanFixtureAddress = "https://192.168.1.4:45678"

func lanFixture(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.lan.runtime = true
	a.lan.addresses = []string{lanFixtureAddress}
	a.settings.UserPasswordHash = mustHashPassword("lan-test-password")
	return a
}
func lanRequest(a *App, method, path, token string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, lanFixtureAddress+path, &b)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	a.lanHandler().ServeHTTP(w, r)
	return w
}
func enableLAN(t *testing.T, a *App) {
	t.Helper()
	w := request(a, "PUT", "/api/remote-access", map[string]any{"enabled": true, "password": "lan-test-password"})
	requireStatus(t, w, 200)
	a.lan.addresses = []string{lanFixtureAddress} // simulate a fresh helper lease after re-enabling
}
func loginLAN(t *testing.T, a *App) string {
	t.Helper()
	w := lanRequest(a, "POST", "/remote-login", "", map[string]string{"password": "lan-test-password"})
	requireStatus(t, w, 200)
	var value struct {
		Token string `json:"token"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil {
		t.Fatal(e)
	}
	if value.Token == a.token || len(value.Token) != 64 {
		t.Fatal("login did not return an independent session")
	}
	return value.Token
}
func TestLANAccessOptInPasswordAndTimeoutPolicy(t *testing.T) {
	a := lanFixture(t)
	if a.settings.RemoteAccessEnabled {
		t.Fatal("LAN enabled by default")
	}
	requireStatus(t, lanRequest(a, "GET", "/", "", nil), 403)
	requireStatus(t, request(a, "PUT", "/api/remote-access", map[string]any{"enabled": true, "password": "bad"}), 401)
	a.settings.UserPasswordHash = ""
	requireStatus(t, request(a, "PUT", "/api/remote-access", map[string]any{"enabled": true, "password": "anything"}), 400)
	a.settings.UserPasswordHash = mustHashPassword("lan-test-password")
	enableLAN(t, a)
	if a.settings.LockTimeoutSec != 300 {
		t.Fatal("zero timeout was not made finite")
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"lockTimeoutSec": 0}), 400)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"lockTimeoutSec": 30}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"remoteAccessEnabled": false, "userName": "retained"}), 200)
	if !a.settings.RemoteAccessEnabled || a.settings.LockTimeoutSec != 30 {
		t.Fatal("ordinary settings bypassed remote policy")
	}
	raw, e := os.ReadFile(SettingsPath(a.dataPath))
	if e != nil {
		t.Fatal(e)
	}
	var disk Settings
	if json.Unmarshal(raw, &disk) != nil || !disk.RemoteAccessEnabled {
		t.Fatal("consent not persisted")
	}
	requireStatus(t, request(a, "PUT", "/api/remote-access", map[string]any{"enabled": false}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"lockTimeoutSec": 0}), 200)
}
func TestLANAccessIndependentSessionAndRevocation(t *testing.T) {
	a := lanFixture(t)
	enableLAN(t, a)
	requireStatus(t, lanRequest(a, "GET", "/api/config", a.token, nil), 401)
	requireStatus(t, lanRequest(a, "POST", "/remote-login", "", map[string]string{"password": ""}), 401)
	requireStatus(t, lanRequest(a, "POST", "/remote-login", "", map[string]string{"password": "wrong"}), 401)
	token := loginLAN(t, a)
	w := lanRequest(a, "GET", "/api/config", token, nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"remoteSession":true`) {
		t.Fatal("remote session missing")
	}
	if strings.Contains(w.Body.String(), a.token) {
		t.Fatal("remote API leaked master token")
	}
	// A device token is never accepted by the local master-token interface.
	r := httptest.NewRequest("GET", "/api/config", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	local := httptest.NewRecorder()
	a.Handler().ServeHTTP(local, r)
	requireStatus(t, local, 401)
	requireStatus(t, lanRequest(a, "PUT", "/api/remote-access", token, map[string]any{"enabled": false}), 403)
	requireStatus(t, lanRequest(a, "POST", "/api/remote-access/logout", token, map[string]any{}), 200)
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 401)
	token = loginLAN(t, a)
	requireStatus(t, request(a, "PUT", "/api/remote-access", map[string]any{"enabled": false}), 200)
	off := lanRequest(a, "GET", "/api/config", token, nil)
	requireStatus(t, off, 403)
	if off.Header().Get("X-Aide-Auth-Required") != "lan-password" {
		t.Fatal("revoked browser did not receive login requirement")
	}
	enableLAN(t, a)
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 401)
	token = loginLAN(t, a)
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("changed-password")
	a.mu.Unlock()
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 401)
}
func TestLANAccessServerIdleAndLockCancelStreams(t *testing.T) {
	a := lanFixture(t)
	enableLAN(t, a)
	token := loginLAN(t, a)
	a.lan.mu.Lock()
	s := a.lan.sessions[hashString(token)]
	s.last = time.Now().Add(-301 * time.Second)
	a.lan.mu.Unlock()
	// Automated API reads do not renew user activity.
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 401)
	select {
	case <-s.ctx.Done():
	default:
		t.Fatal("expired stream context survived")
	}
	token = loginLAN(t, a)
	a.lan.mu.Lock()
	s = a.lan.sessions[hashString(token)]
	s.last = time.Now().Add(-100 * time.Second)
	a.lan.mu.Unlock()
	requireStatus(t, lanRequest(a, "POST", "/api/remote-access/activity", token, map[string]any{}), 200)
	a.lan.mu.Lock()
	last := s.last
	a.lan.mu.Unlock()
	if time.Since(last) > time.Second {
		t.Fatal("user activity did not renew lease")
	}
	if _, e := a.writeLockState(true); e != nil {
		t.Fatal(e)
	}
	a.expireLANSessions()
	select {
	case <-s.ctx.Done():
	default:
		t.Fatal("lock did not cancel remote stream")
	}
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 401)
	token = loginLAN(t, a)
	requireStatus(t, lanRequest(a, "GET", "/api/config", token, nil), 200)
}
func TestLANAccessTransportOriginAndTokenBypasses(t *testing.T) {
	a := lanFixture(t)
	enableLAN(t, a)
	token := loginLAN(t, a)
	for _, test := range []struct {
		name, url, origin string
		plain             bool
	}{
		{"plaintext", lanFixtureAddress + "/api/config", "", true},
		{"spoof localhost", "https://localhost:45678/api/config", "", false},
		{"cross-site", lanFixtureAddress + "/api/config", "https://other.invalid", false},
		{"wrong scheme", lanFixtureAddress + "/api/config", "http://192.168.1.4:45678", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", test.url, nil)
			if test.plain {
				r.TLS = nil
			}
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			a.lanHandler().ServeHTTP(w, r)
			requireStatus(t, w, 403)
		})
	}
	requireStatus(t, lanRequest(a, "GET", "/api/config?access_token="+token, "", nil), 401)
	requireStatus(t, lanRequest(a, "GET", "/api/debug/overview", token, nil), 403)
	requireStatus(t, lanRequest(a, "POST", "/mobile-capture/api", token, map[string]any{}), 404)
}
func TestLANCertificatePrivateSANAndLocalPinPreserved(t *testing.T) {
	dir := t.TempDir()
	certFile, _, e := ensureTLSCert(dir)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(certFile)
	cert, fingerprint, e := ensureLANCertificate(dir, []string{lanFixtureAddress})
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil || leaf.VerifyHostname("192.168.1.4") != nil || len(fingerprint) != 64 {
		t.Fatal("invalid LAN certificate", e)
	}
	_, again, e := ensureLANCertificate(dir, []string{lanFixtureAddress})
	if e != nil || again != fingerprint {
		t.Fatal("stable SAN rotated certificate")
	}
	after, _ := os.ReadFile(certFile)
	if !bytes.Equal(before, after) {
		t.Fatal("localhost certificate changed")
	}
	st, _ := os.Stat(filepath.Join(dir, "lan-key.pem"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("private key permissions")
	}
	for _, address := range []string{"http://192.168.1.4:9", "https://8.8.8.8:9", "https://localhost:9", "https://192.168.1.4:9/path"} {
		if _, _, e := ensureLANCertificate(dir, []string{address}); e == nil {
			t.Fatal("invalid LAN address accepted", address)
		}
	}
}

func TestLANAccessRejectsMalformedAndThrottlesPasswords(t *testing.T) {
	a := lanFixture(t)
	enableLAN(t, a)
	for _, body := range []string{"{", `{"password":"p"} {}`} {
		r := httptest.NewRequest("POST", lanFixtureAddress+"/remote-login", strings.NewReader(body))
		w := httptest.NewRecorder()
		a.lanHandler().ServeHTTP(w, r)
		requireStatus(t, w, 400)
	}
	for i := 0; i < masterPasswordFailureLimit; i++ {
		requireStatus(t, lanRequest(a, "POST", "/remote-login", "", map[string]string{"password": "wrong"}), 401)
	}
	w := lanRequest(a, "POST", "/remote-login", "", map[string]string{"password": "lan-test-password"})
	requireStatus(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no password cooldown")
	}
}
