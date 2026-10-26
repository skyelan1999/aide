package server

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestIssue100SameInstanceUnlockUsesLatestCipher(t *testing.T) {
	va := newVoiceAgent(t.TempDir())
	if err := va.enable("owned-password"); err != nil {
		t.Fatal(err)
	}
	va.recordFallback("owned-first", "fixture")
	va.recordFallback("owned-second", "fixture")
	va.lock()
	if err := va.unlock("owned-password"); err != nil {
		t.Fatal(err)
	}
	if got := len(va.historyDesc()); got != 2 {
		t.Errorf("same instance restored%d entries, want2", got)
	}
}

func TestIssue100UnlockedClearSurvivesLockAndRestart(t *testing.T) {
	data := t.TempDir()
	va := newVoiceAgent(data)
	va.recordFallback("owned-record", "fixture")
	if err := va.enable("owned-password"); err != nil {
		t.Fatal(err)
	}
	if err := va.clearHistory(); err != nil {
		t.Fatal(err)
	}
	va.lock()
	if err := va.unlock("wrong-password"); err == nil {
		t.Fatal("cleared encrypted history lost password protection")
	}
	if err := va.unlock("owned-password"); err != nil {
		t.Fatal(err)
	}
	if len(va.historyDesc()) != 0 {
		t.Fatal("cleared history reappeared after same-instance unlock")
	}
	restarted := newVoiceAgent(data)
	if err := restarted.unlock("owned-password"); err != nil {
		t.Fatal(err)
	}
	if len(restarted.historyDesc()) != 0 {
		t.Fatal("cleared history reappeared after restart")
	}
}

func TestIssue100FailedClearPreservesHistoryAndCachedCipher(t *testing.T) {
	a := testApp(t)
	va := a.voiceAgent
	va.recordFallback("owned-record", "fixture")
	if err := va.enable("owned-password"); err != nil {
		t.Fatal(err)
	}
	before := va.cachedCipher
	file := VoiceHistoryPath(a.dataPath)
	if err := os.Rename(file, file+".owned-fixture-backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "DELETE", "/api/voice-history", nil), 500)
	if len(va.historyDesc()) != 1 || va.cachedCipher != before {
		t.Fatal("failed clear changed history or cache")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".owned-fixture-backup", file); err != nil {
		t.Fatal(err)
	}
	va.lock()
	if err := va.unlock("owned-password"); err != nil {
		t.Fatal(err)
	}
	if len(va.historyDesc()) != 1 {
		t.Fatal("failed clear lost history on unlock")
	}
}

func TestIssue100FailedEncryptionTransitionKeepsPreviousState(t *testing.T) {
	for _, action := range []string{"enable", "password", "disable", "rewrap"} {
		t.Run(action, func(t *testing.T) {
			va := newVoiceAgent(t.TempDir())
			va.recordFallback("owned-record", "fixture")
			if action != "enable" {
				if err := va.enable("old-owned-password"); err != nil {
					t.Fatal(err)
				}
			}
			previousEncrypted, previousCipher := va.encrypted, va.cachedCipher
			previousKey := append([]byte(nil), va.key...)
			previousHistory := append([]VoiceHistoryEntry(nil), va.history...)
			file := VoiceHistoryPath(va.dataPath)
			if err := os.Rename(file, file+".owned-fixture-backup"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch action {
			case "enable":
				err = va.enable("new-owned-password")
			case "password":
				err = va.changePassword("old-owned-password", "new-owned-password")
			case "disable":
				err = va.disable("old-owned-password")
			case "rewrap":
				err = va.ReWrapAll(deriveKey("old-owned-password"), deriveKey("new-owned-password"))
			}
			if err == nil {
				t.Fatal("failed persistence reported successful encryption transition")
			}
			if va.encrypted != previousEncrypted || va.cachedCipher != previousCipher || !bytes.Equal(va.key, previousKey) || !reflect.DeepEqual(va.history, previousHistory) {
				t.Fatal("failed encryption transition published partial state")
			}
		})
	}
}

func TestIssue100LockedHistoryClearIsRejectedWithoutCipherChange(t *testing.T) {
	a := testApp(t)
	va := newVoiceAgent(a.dataPath)
	a.voiceAgent = va
	va.recordFallback("owned-private-history", "fixture")
	if err := va.enable("owned-password"); err != nil {
		t.Fatal(err)
	}
	va.lock()
	before, err := os.ReadFile(VoiceHistoryPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "DELETE", "/api/voice-history", nil)
	if w.Code != 403 {
		t.Errorf("locked encrypted clear reported%d, want403", w.Code)
	}
	after, err := os.ReadFile(VoiceHistoryPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("rejected locked clear modified ciphertext")
	}
}
