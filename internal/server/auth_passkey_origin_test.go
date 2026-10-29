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
	for _, route := range []string{"master", "finish"} {
		for _, kind := range []string{"valid", "changed-http-origin", "changed-signed-origin", "invalid-signature", "missing-user-verification", "counter-equal", "counter-regression", "counterless", "counter-increase"} {
			t.Run(route+"/"+kind, func(t *testing.T) {
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
				if kind == "counter-equal" || kind == "counter-increase" {
					a.webAuthn.store.Credentials[0].Credential.Authenticator.SignCount = 1
				}
				if kind == "counter-regression" {
					a.webAuthn.store.Credentials[0].Credential.Authenticator.SignCount = 2
				}
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
				counter := uint32(1)
				if kind == "counterless" {
					counter = 0
				}
				if kind == "counter-increase" {
					counter = 2
				}
				binary.BigEndian.PutUint32(auth[33:], counter)
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
				endpoint, payload := "/api/auth/verify", any(body)
				if route == "finish" {
					endpoint = "/api/webauthn/assertion/finish?challenge=" + options.Challenge
					payload = body["assertion"]
				}
				result := post(endpoint, payload, port)
				success := kind == "valid" || kind == "counterless" || kind == "counter-increase"
				if !success {
					expected := 401
					if route == "finish" && kind == "changed-http-origin" {
						expected = 400
					}
					requireStatus(t, result, expected)
					if a.isAssistantUnlocked(assistant.ID) {
						t.Fatal("invalid assertion unlocked assistant")
					}
					return
				}
				requireStatus(t, result, 200)
				if route == "master" && !a.isAssistantUnlocked(assistant.ID) {
					t.Fatal("valid signed assertion did not unlock assistant")
				}
				a.webAuthn.mu.Lock()
				_, sessionExists := a.webAuthn.sessions[options.Challenge]
				_, originExists := a.webAuthn.origins[options.Challenge]
				a.webAuthn.mu.Unlock()
				if sessionExists || originExists {
					t.Fatal("verified challenge or origin binding retained")
				}
				replayed := post(endpoint, payload, "9999")
				if replayed.Code < 400 || replayed.Code >= 500 {
					t.Fatal("replayed challenge accepted", replayed.Code)
				}
			})
		}
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

func TestPasskeyCounterCommitRechecksCurrentState(t *testing.T) {
	a := testApp(t)
	id := []byte("counter-fixture")
	stored := webauthn.Credential{ID: id}
	stored.Authenticator.SignCount = 1
	a.webAuthn.store.Credentials = []webAuthnCredEntry{{ID: base64.RawURLEncoding.EncodeToString(id), Credential: stored}}
	next := stored
	next.Authenticator.SignCount = 2
	if err := a.commitWebAuthnCounter(&next); err != nil {
		t.Fatal(err)
	}
	if err := a.commitWebAuthnCounter(&next); err == nil {
		t.Fatal("concurrent/stale validation reused accepted counter")
	}
	warning := next
	warning.Authenticator.SignCount = 3
	warning.Authenticator.CloneWarning = true
	if err := a.commitWebAuthnCounter(&warning); err == nil {
		t.Fatal("library clone warning ignored")
	}
	if a.webAuthn.store.Credentials[0].Credential.Authenticator.SignCount != 2 {
		t.Fatal("rejection changed stored counter")
	}
}
