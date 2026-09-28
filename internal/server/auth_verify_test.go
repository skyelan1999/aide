package server

import (
	"encoding/json"
	"testing"
)

func TestMasterAuthUnlocksRequestedAssistantSession(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("account-password")
	s := a.findAssistantSessionLocked()
	a.mu.Unlock()
	if s == nil {
		t.Fatal("test app should have an assistant session")
	}

	w := request(a, "POST", "/api/auth/verify", map[string]string{
		"password": "account-password", "assistantSessionId": s.ID,
	})
	requireStatus(t, w, 200)
	if !a.isAssistantUnlocked(s.ID) {
		t.Fatal("successful master authentication should unlock the requested assistant session")
	}
}

func TestMasterAuthCannotUnlockNonAssistantSession(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("account-password")
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "ordinary"})
	var s Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}

	w = request(a, "POST", "/api/auth/verify", map[string]string{
		"password": "account-password", "assistantSessionId": s.ID,
	})
	requireStatus(t, w, 404)
	if a.isAssistantUnlocked(s.ID) {
		t.Fatal("master authentication must not unlock a regular session")
	}
}
