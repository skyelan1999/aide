package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Public attestation/signature vector produced by the native encoder using an
// ephemeral software TEST key. No private keys or hardware credentials in Git.
func TestNativeTouchIDWebAuthnInterop(t *testing.T) {
	var vector struct {
		Origin                string          `json:"origin"`
		RegistrationChallenge string          `json:"registrationChallenge"`
		AssertionChallenge    string          `json:"assertionChallenge"`
		Registration          json.RawMessage `json:"registration"`
		Assertion             json.RawMessage `json:"assertion"`
	}
	data, err := os.ReadFile("testdata/native-touchid-webauthn.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "wrong origin", "expired", "wrong challenge", "bad signature", "revoked", "relocked"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Mkdir(directory+"/config", 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(directory+"/auth", 0700); err != nil {
				t.Fatal(err)
			}
			a := newWebAuthnTestApp(t, directory)
			a.dataPath = directory
			locked, err := a.writeLockState(true)
			if err != nil {
				t.Fatal(err)
			}
			a.webAuthn.sessions[vector.RegistrationChallenge] = &webauthn.SessionData{
				Challenge: vector.RegistrationChallenge, RelyingPartyID: "localhost",
				UserID: []byte(webAuthnUserHandleStr), Expires: time.Now().Add(time.Minute),
				UserVerification: protocol.VerificationRequired,
				CredParams:       []protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}},
			}
			a.webAuthn.origins[vector.RegistrationChallenge] = vector.Origin
			status, message := finishErrText(t, a.webAuthnRegisterFinish,
				"/api/webauthn/register/finish?challenge="+vector.RegistrationChallenge, vector.Registration)
			if status != http.StatusOK {
				t.Fatalf("native registration: %d %s", status, message)
			}
			var response struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(vector.Assertion, &response); err != nil {
				t.Fatal(err)
			}
			id, err := base64.RawURLEncoding.DecodeString(response.ID)
			if err != nil {
				t.Fatal(err)
			}
			session := &webauthn.SessionData{Challenge: vector.AssertionChallenge, RelyingPartyID: "localhost",
				UserID: []byte(webAuthnUserHandleStr), AllowedCredentialIDs: [][]byte{id},
				UserVerification: protocol.VerificationRequired, Expires: time.Now().Add(time.Minute)}
			a.webAuthn.sessions[vector.AssertionChallenge] = session
			a.webAuthn.origins[vector.AssertionChallenge] = vector.Origin
			body := append([]byte(nil), vector.Assertion...)
			port := "9999"
			switch scenario {
			case "wrong origin":
				port = "10000"
			case "expired":
				session.Expires = time.Now().Add(-time.Minute)
			case "wrong challenge":
				session.Challenge = "different-challenge"
			case "bad signature":
				var value map[string]any
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				value["response"].(map[string]any)["signature"] = base64.RawURLEncoding.EncodeToString(make([]byte, 70))
				body, _ = json.Marshal(value)
			case "revoked":
				a.webAuthn.store.Credentials = nil
			case "relocked":
				if _, err := a.writeLockState(true); err != nil {
					t.Fatal(err)
				}
			}
			target := "/api/webauthn/assertion/finish?challenge=" + vector.AssertionChallenge + "&unlockScreen=1&lockGen=" + strconv.FormatInt(locked.Gen, 10)
			req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
			setWebAuthnTestOrigin(req, port)
			rec := httptest.NewRecorder()
			a.webAuthnAssertionFinish(rec, req)
			if scenario == "success" {
				if rec.Code != http.StatusOK || a.readLockState().Locked {
					t.Fatalf("native unlock rejected: %d %s", rec.Code, rec.Body.String())
				}
				// One challenge cannot authorize a second unlock.
				status, _ := finishErrText(t, a.webAuthnAssertionFinish, target, vector.Assertion)
				if status == http.StatusOK {
					t.Fatal("assertion replay accepted")
				}
			} else if rec.Code == http.StatusOK || !a.readLockState().Locked {
				t.Fatalf("unsafe native assertion unlocked: %s %d", scenario, rec.Code)
			}
		})
	}
}
