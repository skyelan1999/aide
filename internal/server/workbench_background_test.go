package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkbenchBackgroundAssetPolicy(t *testing.T) {
	a := testApp(t)
	for _, path := range []string{"/", "/workbench-background.js", "/workbench.css"} {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		policy := rec.Header().Get("Content-Security-Policy")
		for _, rule := range []string{"img-src 'self' data: blob:", "media-src 'self' blob:", "script-src 'self' 'wasm-unsafe-eval'", "connect-src 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(policy, rule) {
				t.Errorf("%s missing %s", path, rule)
			}
		}
		if strings.Contains(policy, "https:") || strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "script-src 'self' blob:") {
			t.Errorf("unexpected remote or executable source in %s", policy)
		}
	}
}
