package server

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// Isolated real-workbench password flow; never changes production credentials.
func TestLockBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_LOCK_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in isolated lock browser fixture")
	}
	a := testApp(t)
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("123456")
	a.settings.LockTimeoutSec = 0
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>锁屏验收</title><script src="/fixture-bootstrap.js" defer></script>`))
	})
	mux.HandleFunc("GET /fixture-bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		token, _ := json.Marshal(a.token)
		_, _ = w.Write(append(append([]byte("localStorage.setItem('aide-token',"), token...), []byte(");location.replace('/');")...))
	})
	mux.Handle("/", a.Handler())
	server := &http.Server{Addr: ":18620", Handler: mux}
	t.Cleanup(func() { _ = server.Close() })
	t.Log("Isolated lock workbench :18620, synthetic password 123456")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
