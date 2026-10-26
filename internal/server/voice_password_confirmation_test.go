package server

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestIssue149HistoryPasswordRequiresOldSecret(t *testing.T) {
	for _, old := range []string{"wrong-owned-secret", ""} {
		t.Run(old, func(t *testing.T) {
			a := testApp(t)
			va := a.voiceAgent
			if err := va.enable("correct-owned-secret"); err != nil {
				t.Fatal(err)
			}
			va.recordFallback("owned-history", "fixture")
			before, err := os.ReadFile(VoiceHistoryPath(a.dataPath))
			if err != nil {
				t.Fatal(err)
			}
			key, cipher := append([]byte(nil), va.key...), va.cachedCipher
			response := request(a, "POST", "/api/voice-history/change-password", map[string]string{"oldPassword": old, "newPassword": "new-owned-secret"})
			if response.Code != 400 {
				t.Fatal("arbitrary old password rotated an unlocked history key")
			}
			after, err := os.ReadFile(VoiceHistoryPath(a.dataPath))
			if err != nil || !bytes.Equal(before, after) || !bytes.Equal(key, va.key) || cipher != va.cachedCipher {
				t.Fatal("rejected password change modified encryption state", err)
			}
		})
	}
}

func TestIssue149ValidHistoryPasswordRotationPreservesHistory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "argon", true: "legacy"}[legacy], func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(AuthDir(dir), 0700); err != nil {
				t.Fatal(err)
			}
			if err := ensureKdfSalt(dir); err != nil {
				t.Fatal(err)
			}
			va := newVoiceAgent(dir)
			va.recordFallback("owned-private-history", "fixture")
			if err := va.enable("old-owned-secret"); err != nil {
				t.Fatal(err)
			}
			if legacy {
				if err := va.ReWrapAll(deriveKey("old-owned-secret"), DeriveAESKeyLegacy("old-owned-secret")); err != nil {
					t.Fatal(err)
				}
			}
			before := va.historyDesc()
			if err := va.changePassword("old-owned-secret", "new-owned-secret"); err != nil {
				t.Fatal(err)
			}
			restarted := newVoiceAgent(dir)
			if err := restarted.unlock("old-owned-secret"); err == nil {
				t.Fatal("old password opened rotated ciphertext")
			}
			if err := restarted.unlock("new-owned-secret"); err != nil || !reflect.DeepEqual(before, restarted.historyDesc()) {
				t.Fatal("rotation lost history or new secret", err)
			}
		})
	}
}

func TestIssue149EmptyCipherStillRequiresActiveSecret(t *testing.T) {
	va := newVoiceAgent(t.TempDir())
	va.encrypted = true
	va.key = deriveKey("old-owned-secret")
	before := append([]byte(nil), va.key...)
	if err := va.changePassword("wrong-owned-secret", "new-owned-secret"); err == nil || !bytes.Equal(before, va.key) {
		t.Fatal("missing cipher bypassed active-key confirmation")
	}
	va.lock()
	if err := va.changePassword("old-owned-secret", "new-owned-secret"); err == nil {
		t.Fatal("locked history rotated")
	}
}
