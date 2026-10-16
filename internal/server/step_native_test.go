package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStepNativeScopeAndStreaming(t *testing.T) {
	a := testApp(t)
	payload := strings.Repeat("STEP-data", 8192)
	if err := os.WriteFile(filepath.Join(a.workPath, "model.step"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	called := 0
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/convert" || r.Header.Get("Authorization") != "Bearer "+nativeStepKey(a.token) {
			t.Error("incorrect private helper request")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != payload {
			t.Errorf("input truncated: %d", len(body))
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, "{\"kind\":\"heartbeat\"}\n{\"kind\":\"complete\",\"triangles\":12}\n")
	}))
	defer helper.Close()
	t.Setenv("AIDE_CAD_URL", helper.URL)
	req := httptest.NewRequest("GET", "/api/file/step-preview?path=model.step", nil)
	req.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	a.stepNativePreview(w, req)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "complete") || called != 1 {
		t.Fatal(w.Body.String())
	}
	for _, q := range []string{"path=../outside.step", "path=model.step&knowledgeOrigin=stale", "path=model.txt", "path=missing.step"} {
		w = request(a, "GET", "/api/file/step-preview?"+q, nil)
		requireStatus(t, w, 400)
	}
	if called != 1 {
		t.Fatal("invalid source reached converter")
	}
	// No new query-token exception: the native endpoint requires a header token.
	req = httptest.NewRequest("GET", "/api/file/step-preview?path=model.step&access_token="+a.token, nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	requireStatus(t, w, 401)
}
func TestStepNativeDisabledAndBusy(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_CAD_URL", "")
	requireStatus(t, request(a, "GET", "/api/file/step-preview?path=model.step", nil), 501)
	t.Setenv("AIDE_CAD_URL", "http://127.0.0.1:1")
	stepNativeGate <- struct{}{}
	defer func() { <-stepNativeGate }()
	requireStatus(t, request(a, "GET", "/api/file/step-preview?path=model.step", nil), 429)
}

func TestStepNativeCredentialIsScopedAndSymlinkSafe(t *testing.T) {
	a := testApp(t)
	key, err := a.prepareStepNativeToken()
	if err != nil {
		t.Fatal(err)
	}
	if key == a.token || len(key) != 64 {
		t.Fatal("worker credential was not scoped")
	}
	file := filepath.Join(a.dataPath, "cad-auth", "token")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("worker credential mode invalid")
	}
	before, err := os.ReadFile(AccessTokenPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	requireStatus(t, w, 401)
	if next, err := a.prepareStepNativeToken(); err != nil || next != key {
		t.Fatal("worker credential unstable")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(AccessTokenPath(a.dataPath), file); err != nil {
		t.Fatal(err)
	}
	if _, err := a.prepareStepNativeToken(); err == nil {
		t.Fatal("credential symlink accepted")
	}
	after, _ := os.ReadFile(AccessTokenPath(a.dataPath))
	if string(before) != string(after) {
		t.Fatal("login credential changed")
	}
}
