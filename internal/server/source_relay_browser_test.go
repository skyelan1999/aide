package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in workbench with actual APIs and downloaded relay, using only disposable
// fixtures. It never reads a user's Aide configuration, models or installed plugins.
func TestReferenceRelayBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_RELAY_BROWSER_FIXTURE") != "1" {
		t.Skip("manual browser fixture")
	}
	a := testApp(t)
	a.token = "relay-browser-synthetic"
	src := relayTestSource(t, a, t.TempDir(), true)
	src.Name = "Relay acceptance"
	relayTestSave(t, a, src, "")
	os.WriteFile(filepath.Join(src.Config.Path, "acceptance.md"), []byte("# Relay acceptance\n\nDownloaded agent serves this file.\n"), 0600)
	relayTestStart(t, a, src)
	server := &http.Server{Addr: ":18977", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { server.Close() })
	t.Log("BROWSER_READY http://127.0.0.1:18977/#token=relay-browser-synthetic")
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		t.Fatal(err)
	case <-time.After(25 * time.Minute):
	}
}
