package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestIssue153CloneBindingWaitsForPersistentSettings(t *testing.T) {
	a := testApp(t)
	id := ownedVoiceSample(t, a)
	clone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]string{"voice_id": "owned-created-voice", "status": "ready"})
	}))
	defer clone.Close()
	a.mu.Lock()
	a.settings.CloneTTSBaseURL = clone.URL
	a.settings.CloneTTSBackend = "openai"
	a.settings.CloneVoiceID = "owned-previous-voice"
	err := atomicJSON(SettingsPath(a.dataPath), a.settings)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	p := SettingsPath(a.dataPath)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	backup := p + ".owned-fixture"
	if err := os.Rename(p, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"sampleIds": []string{id}}
	w := request(a, "POST", "/api/voice-clone/create", body)
	requireStatus(t, w, 500)
	var result struct {
		VoiceID string `json:"voiceID"`
		Bound   bool   `json:"bound"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Bound || result.VoiceID != "owned-created-voice" {
		t.Fatal("failed binding concealed the created remote voice or claimed success", err)
	}
	a.mu.Lock()
	current := a.settings.CloneVoiceID
	a.mu.Unlock()
	preserved, err := os.ReadFile(backup)
	if err != nil || current != "owned-previous-voice" || !bytes.Equal(before, preserved) {
		t.Fatal("failed binding changed the old setting", err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, p); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/voice-clone/create", body)
	requireStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Bound {
		t.Fatal("successful binding not confirmed", err)
	}
	var saved Settings
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &saved) != nil || saved.CloneVoiceID != "owned-created-voice" {
		t.Fatal("bound voice was not persisted", err)
	}
}
