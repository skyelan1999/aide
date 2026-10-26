package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/webauthn"
)

// A virtual authenticator signs real WebAuthn assertion bytes, without weakening verification.
func TestMasterPasskeyDynamicOriginAndReplay(t *testing.T) {
	for _, kind := range []string{"valid", "changed-http-origin", "changed-signed-origin", "invalid-signature", "missing-user-verification"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			id := []byte("virtual-passkey-fixture")
			public, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
			if err != nil {
				t.Fatal(err)
			}
			a.webAuthn.mu.Lock()
			a.webAuthn.store.Credentials = []webAuthnCredEntry{{ID: base64.RawURLEncoding.EncodeToString(id), Credential: webauthn.Credential{ID: id, PublicKey: public}}}
			a.webAuthn.mu.Unlock()
			a.mu.Lock()
			a.settings.UserPasswordHash = mustHashPassword("fixture-account-password")
			assistant := a.findAssistantSessionLocked()
			a.mu.Unlock()
			post := func(path string, body any, port string) *httptest.ResponseRecorder {
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
				req.Header.Set("Authorization", "Bearer "+a.token)
				req.Header.Set("Content-Type", "application/json")
				setWebAuthnTestOrigin(req, port)
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, req)
				return w
			}
			start := post("/api/webauthn/assertion/start", map[string]any{}, "9999")
			requireStatus(t, start, 200)
			var options struct {
				Challenge string `json:"challenge"`
			}
			if err = json.Unmarshal(start.Body.Bytes(), &options); err != nil {
				t.Fatal(err)
			}
			signedOrigin := "https://localhost:9999"
			port := "9999"
			if kind == "changed-http-origin" {
				port = "9998"
			}
			if kind == "changed-signed-origin" {
				signedOrigin = "https://localhost:9998"
			}
			client, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": options.Challenge, "origin": signedOrigin, "crossOrigin": false})
			rpHash := sha256.Sum256([]byte("localhost"))
			auth := make([]byte, 37)
			copy(auth, rpHash[:])
			auth[32] = 5
			if kind == "missing-user-verification" {
				auth[32] = 1
			}
			binary.BigEndian.PutUint32(auth[33:], 1)
			clientHash := sha256.Sum256(client)
			signed := append(append([]byte{}, auth...), clientHash[:]...)
			digest := sha256.Sum256(signed)
			signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			if kind == "invalid-signature" {
				signature[len(signature)-1] ^= 1
			}
			enc := base64.RawURLEncoding.EncodeToString
			body := map[string]any{"challenge": options.Challenge, "assistantSessionId": assistant.ID, "assertion": map[string]any{"id": enc(id), "rawId": enc(id), "type": "public-key", "response": map[string]any{"clientDataJSON": enc(client), "authenticatorData": enc(auth), "signature": enc(signature), "userHandle": enc([]byte(webAuthnUserHandleStr))}}}
			result := post("/api/auth/verify", body, port)
			if kind != "valid" {
				requireStatus(t, result, 401)
				if a.isAssistantUnlocked(assistant.ID) {
					t.Fatal("invalid assertion unlocked assistant")
				}
				return
			}
			requireStatus(t, result, 200)
			if !a.isAssistantUnlocked(assistant.ID) {
				t.Fatal("valid signed assertion did not unlock assistant")
			}
			a.webAuthn.mu.Lock()
			_, sessionExists := a.webAuthn.sessions[options.Challenge]
			_, originExists := a.webAuthn.origins[options.Challenge]
			a.webAuthn.mu.Unlock()
			if sessionExists || originExists {
				t.Fatal("verified challenge or origin binding retained")
			}
			requireStatus(t, post("/api/auth/verify", body, "9999"), 401)
		})
	}
}
func TestMasterAuthTokenFailureIsDistinct(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/verify", bytes.NewBufferString(`{"password":"wrong"}`))
	req.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	requireStatus(t, w, 401)
	if w.Header().Get("X-Aide-Auth-Required") != "token" {
		t.Fatal("missing access-token failure marker")
	}
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("fixture-password")
	a.mu.Unlock()
	w = request(a, "POST", "/api/auth/verify", map[string]string{"password": "wrong"})
	requireStatus(t, w, 401)
	if w.Header().Get("X-Aide-Auth-Required") != "" {
		t.Fatal("wrong account password was labeled invalid access token")
	}
}
