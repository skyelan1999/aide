package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
)

// Explicitly opt-in, loopback-published fixture. It serves the actual embedded
// workbench and LAN handler with synthetic credentials; no live settings change.
func TestLANAccessBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_LAN_BROWSER_FIXTURE") != "1" {
		t.Skip("isolated browser fixture")
	}
	a := testApp(t)
	if os.Getenv("AIDE_LAN_BROWSER_NO_PASSWORD") != "1" {
		a.settings.UserPasswordHash = mustHashPassword("lan-browser-password")
	}
	a.lan.runtime = true
	localAddress, remoteAddress := ":18640", ":18641"
	a.lan.addresses = []string{"https://127.0.0.1:18641"}
	certFile, keyFile, e := ensureTLSCert(CertsDir(a.dataPath))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(certFile)
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("fixture certificate invalid")
	}
	sum := sha256.Sum256(block.Bytes)
	a.lan.fingerprint = hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture", func(w http.ResponseWriter, r *http.Request) {
		token, _ := json.Marshal(a.token)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>localStorage.setItem('aide-token'," + string(token) + ");location.replace('/')</script>"))
	})
	mux.Handle("/", a.Handler())
	main := &http.Server{Addr: localAddress, Handler: mux}
	remote := &http.Server{Addr: remoteAddress, Handler: a.lanHandler()}
	t.Cleanup(func() { _ = main.Close(); _ = remote.Close() })
	listener, e := net.Listen("tcp", remoteAddress)
	if e != nil {
		t.Fatal(e)
	}
	go func() { _ = remote.ServeTLS(listener, certFile, keyFile) }()
	t.Log("loopback-published actual workbench :18640 and isolated LAN handler :18641; synthetic password")
	if e := main.ListenAndServeTLS(certFile, keyFile); e != nil && !errors.Is(e, http.ErrServerClosed) {
		t.Fatal(e)
	}
}
