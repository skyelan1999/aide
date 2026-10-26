package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIssue115MasterPasswordBudgetSharedAcrossUnlockRoutes(t *testing.T) {
	a := testApp(t)
	a.settings.UserPasswordHash = mustHashPassword("owned-correct-password")
	routes := []string{"/api/auth/verify", "/api/unlock"}
	for i := 0; i < 5; i++ {
		requireStatus(t, request(a, "POST", routes[i%len(routes)], map[string]string{"password": "owned-wrong-password"}), 401)
	}
	a.mu.Lock()
	s := a.findAssistantSessionLocked()
	a.mu.Unlock()
	for _, route := range append(routes, "/api/account/verify-password", "/api/workspace/unlock-vault", "/api/sessions/"+s.ID+"/unlock-assistant") {
		w := request(a, "POST", route, map[string]string{"password": "owned-correct-password"})
		if w.Code != 429 {
			t.Errorf("%s bypassed shared exhausted budget: status%d", route, w.Code)
		}
		if w.Header().Get("Retry-After") == "" {
			t.Errorf("%s missing bounded retry delay", route)
		}
	}
	if a.isAssistantUnlocked(s.ID) {
		t.Fatal("throttled password unlocked assistant")
	}
	w := request(a, "POST", "/api/factory-reset", map[string]any{"confirmText": "重置", "password": "owned-correct-password", "scope": map[string]bool{"sessionsAndMemory": true}})
	requireStatus(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("factory-reset bypass missing retry delay")
	}
}

func TestIssue115PasswordBudgetExpiresWithoutRejectedAttemptsExtendingLockout(t *testing.T) {
	a := &App{}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	a.masterPasswordAttempts.now = func() time.Time { return now }
	hash := sha256Hex("owned-password")
	for i := 0; i < masterPasswordFailureLimit; i++ {
		valid, _, err := a.verifyMasterPassword("wrong", hash)
		if valid || err != nil {
			t.Fatalf("initial attempt%d: valid=%v err=%v", i, valid, err)
		}
	}
	now = now.Add(59 * time.Second)
	_, _, err := a.verifyMasterPassword("owned-password", hash)
	var cooldown *masterPasswordCooldownError
	if !errors.As(err, &cooldown) || cooldown.retryAfter != time.Second {
		t.Fatalf("expected one-second cooldown: %v", err)
	}
	now = now.Add(time.Second)
	valid, upgrade, err := a.verifyMasterPassword("owned-password", hash)
	if !valid || !upgrade || err != nil {
		t.Fatalf("expiry must allow legacy positive path: %v %v %v", valid, upgrade, err)
	}
	for i := 0; i < masterPasswordFailureLimit-1; i++ {
		_, _, _ = a.verifyMasterPassword("wrong", hash)
	}
	now = now.Add(masterPasswordCooldown)
	valid, _, err = a.verifyMasterPassword("owned-password", hash)
	if !valid || err != nil {
		t.Fatal("expired partial failure window must allow success")
	}
	if a.masterPasswordAttempts.failures != 0 {
		t.Fatal("successful verification must clear prior failures")
	}
}

func TestIssue115ParallelAttemptsCannotOverrunSharedBudget(t *testing.T) {
	a := &App{}
	hash := sha256Hex("owned-password")
	var attempts, throttled atomic.Int32
	var workers sync.WaitGroup
	for range 30 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			valid, _, err := a.verifyMasterPassword("wrong", hash)
			if valid {
				t.Error("invalid password accepted")
			}
			if err == nil {
				attempts.Add(1)
			} else {
				throttled.Add(1)
			}
		}()
	}
	workers.Wait()
	if attempts.Load() != masterPasswordFailureLimit || throttled.Load() != 25 {
		t.Fatalf("concurrent budget overrun: attempts%d throttled%d", attempts.Load(), throttled.Load())
	}
	other := &App{}
	if valid, _, err := other.verifyMasterPassword("owned-password", hash); !valid || err != nil {
		t.Fatal("independent app inherited another app's budget")
	}
}

func TestIssue115PasswordlessRoutesDoNotConsumePasswordBudget(t *testing.T) {
	a := testApp(t)
	a.settings.UserPasswordHash = ""
	a.masterPasswordAttempts.blockedUntil = time.Now().Add(time.Minute)
	for _, route := range []string{"/api/auth/verify", "/api/unlock"} {
		requireStatus(t, request(a, "POST", route, map[string]string{"password": "owned-nonempty"}), 200)
	}
}
