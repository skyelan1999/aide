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
		if r.URL.Path != "/convert" || r.Header.Get("Authorization") != "Bearer "+a.token {
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
	a.handler.ServeHTTP(w, req)
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
