package server

import (
	"bytes"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestIssue80PersonalityAdmissionDoesNotMutateOnFailure(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{"id": "aide", "enabled": true, "prompt": "original control"}), 200)
	original := request(a, "GET", "/api/personality?id=aide", nil).Body.String()
	persisted, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		`{"id":"aide","enabled":true,"prompt":"` + strings.Repeat("界", 4001) + `"}`,
		`{"id":"aide","prompt":"small","unused":"` + strings.Repeat("x", (1<<20)+1) + `"}`,
		`{"id":"aide","prompt":"first"} {"id":"aide","prompt":"second"}`,
	}
	for i, body := range cases {
		r := httptest.NewRequest("PUT", "/api/personality", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+a.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Errorf("case%d accepted oversized/multiple input: %d", i, w.Code)
		}
		if now := request(a, "GET", "/api/personality?id=aide", nil).Body.String(); now != original {
			t.Errorf("case%d mutated in-memory personality", i)
		}
		current, err := os.ReadFile(SettingsPath(a.dataPath))
		if err != nil || !bytes.Equal(current, persisted) {
			t.Errorf("case%d changed persisted settings: %v", i, err)
		}
	}
}
func TestIssue80PersonalityUnicodeBoundary(t *testing.T) {
	a := testApp(t)
	prompt := strings.Repeat("界", 4000)
	requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{"id": "xiaomi", "prompt": prompt, "enabled": true}), 200)
	if got := a.settings.Personalities["xiaomi"].Prompt; got != prompt {
		t.Fatal("4000-character Unicode control rejected/truncated")
	}
}
