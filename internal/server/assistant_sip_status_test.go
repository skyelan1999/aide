package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantSIPStatusFreshnessAndAuth(t *testing.T) {
	a := testApp(t)
	dir := filepath.Join(a.dataPath, "sip")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{0, time.Minute} {
		value := assistantSIPStatus{Server: "10.0.0.1", Phone: "10.0.0.2", Port: 5060, Account: "1001", Extension: "1000", Mode: "normal", State: "waiting", UpdatedAt: time.Now().Add(-age)}
		raw, _ := json.Marshal(value)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		fields["apiKey"] = "fixture-secret-never-expose"
		raw, _ = json.Marshal(fields)
		if err := os.WriteFile(filepath.Join(dir, "host-status.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		w := request(a, "GET", "/api/assistant-sip/status", nil)
		requireStatus(t, w, 200)
		if strings.Contains(w.Body.String(), "fixture-secret") {
			t.Fatal("credential field escaped into SIP guide")
		}
		var got assistantSIPStatus
		if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Running != (age == 0) || got.Server != value.Server {
			t.Fatal(w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/assistant-sip/status", nil))
	requireStatus(t, w, 401)
}
