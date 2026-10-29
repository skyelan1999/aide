package server

import (
	"encoding/json"
	"testing"
)

func TestIssue106LockedAssistantCannotUseUnconfiguredVoiceFallback(t *testing.T) {
	for _, kind := range []string{"no-model", "no-base", "no-agent", "configured"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			a.settings.UserPasswordHash = sha256Hex("owned-password")
			a.settings.BaseURL = ""
			a.settings.Model = ""
			if kind == "no-base" {
				a.settings.Model = "owned-model"
			}
			if kind == "no-agent" {
				a.voiceAgent = nil
			}
			if kind == "configured" {
				a.settings.BaseURL = "http://127.0.0.1:1"
				a.settings.Model = "owned-model"
			}
			a.mu.Lock()
			session := a.findAssistantSessionLocked()
			beforeMessages := len(session.Messages)
			a.mu.Unlock()
			w := request(a, "POST", "/api/voice-filter", map[string]any{"text": "owned-private-voice-input"})
			requireStatus(t, w, 200)
			var response struct{ Action string }
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Action != "locked" {
				t.Errorf("locked assistant fallback returned action %q", response.Action)
			}
			a.mu.Lock()
			afterMessages := len(session.Messages)
			a.mu.Unlock()
			if afterMessages != beforeMessages {
				t.Errorf("locked voice persisted%d conversation messages", afterMessages-beforeMessages)
			}
			if a.voiceAgent != nil && len(a.voiceAgent.historyDesc()) != 0 {
				t.Error("locked fallback recorded private voice history")
			}
		})
	}
}

func TestIssue106UnlockedAssistantKeepsNoModelFallback(t *testing.T) {
	a := testApp(t)
	a.settings.BaseURL = ""
	a.settings.Model = ""
	unlockAssistantSessionForTest(t, a)
	w := request(a, "POST", "/api/voice-filter", map[string]any{"text": "owned-authorized-voice"})
	requireStatus(t, w, 200)
	var response struct{ Action string }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Action != "send" {
		t.Fatalf("authorized fallback not available: %q", response.Action)
	}
	a.clearAssistantUnlock()
	w = request(a, "POST", "/api/voice-filter", map[string]any{"text": "owned-locked-again"})
	requireStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Action != "locked" {
		t.Error("relocked assistant kept fallback access")
	}
}
