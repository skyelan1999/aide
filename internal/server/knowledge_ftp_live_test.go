package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"
)

// This fixture speaks FTP on real TCP sockets and implicit FTPS with a local
// certificate. The adapter is exercised with the system curl binary, not a
// subprocess stub, and the listener remains bound to loopback only.
func TestKnowledgeFTPAndFTPSLiveAdapters(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl is not installed")
	}
	fixtureDir := t.TempDir()
	caPath := writeFTPCertificate(t, fixtureDir)
	wrapperPath := path.Join(fixtureDir, "curl-with-local-ca")
	wrapper := fmt.Sprintf("#!/bin/sh\nCURL_CA_BUNDLE=%s\nexport CURL_CA_BUNDLE\nexec %s \"$@\"\n", shellQuote(caPath), shellQuote(curl))
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{"ftp", "ftps"} {
		t.Run(kind, func(t *testing.T) {
			server := newLiveFTPFixture(t, kind == "ftps", caPath)
			defer server.Close()

			a := testApp(t)
			a.curlBin = wrapperPath
			url := fmt.Sprintf("%s://127.0.0.1:%d/pub", kind, server.Port())
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
				"sources": []any{sourceBody("live-ftp", "live ftp", kind, map[string]any{"url": url, "username": "fixture"}, false)},
				"secrets": map[string]any{"live-ftp": map[string]any{"password": "fixture-password"}},
			}), 200)

			ar := liveFTPSourceArea(t, a)
			entries, err := a.knowledgeAreaList(context.Background(), ar, ".")
			if err != nil {
				t.Fatalf("live %s directory listing: %v", kind, err)
			}
			if !liveFTPHas(entries, "selected", true) || !liveFTPHas(entries, "root.md", false) {
				t.Fatalf("live %s listing missing expected entries: %#v", kind, entries)
			}

			content, err := a.knowledgeAreaRead(context.Background(), ar, "root.md", 64*1024)
			if err != nil || string(content) != "FTP fixture root body\n" {
				t.Fatalf("live %s RETR content=%q err=%v", kind, content, err)
			}

			var result knowledgeRemoteResult
			for i := 0; i < 8; i++ {
				a.knowledgeUpdates.remoteMu.Lock()
				for _, memo := range a.knowledgeUpdates.remote {
					memo.checked = time.Time{}
				}
				a.knowledgeUpdates.remoteMu.Unlock()
				result = a.knowledgeRemoteCached(context.Background(), ar, false, 2, 1)
				if result.coverage.TotalKnown {
					break
				}
			}
			if !result.coverage.Progressive || !result.coverage.TotalKnown || result.coverage.Files != 2 {
				t.Fatalf("live %s progressive scan did not finish with two files: coverage=%+v message=%q", kind, result.coverage, result.message)
			}
			if !liveFTPGraphHas(result.nodes, "root.md") || !liveFTPGraphHas(result.nodes, "selected/note.md") {
				t.Fatalf("live %s progressive graph is missing files: %#v", kind, result.nodes)
			}
		})
	}
}

func liveFTPSourceArea(t *testing.T, a *App) knowledgeArea {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ar := range a.knowledgeAreasLocked(a.wsID(), a.workspace.Name(), a.reference.Name()) {
		if ar.source == "live-ftp" {
			return ar
		}
	}
	t.Fatal("live FTP source was not registered")
	return knowledgeArea{}
}

func liveFTPHas(entries []map[string]any, name string, wantDir bool) bool {
	for _, entry := range entries {
		if entry["name"] == name && entry["dir"] == wantDir {
			return true
		}
	}
	return false
}

func liveFTPGraphHas(nodes []knowledgeNode, name string) bool {
	for _, node := range nodes {
		if node.Path == name && node.Kind == "file" {
			return true
		}
	}
	return false
}

func writeFTPCertificate(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := path.Join(dir, "ftp-ca.pem")
	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		_ = certFile.Close()
		t.Fatal(err)
	}
	if err := certFile.Close(); err != nil {
		t.Fatal(err)
	}
	keyPath := path.Join(dir, "ftp-server-key.pem")
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		_ = keyFile.Close()
		t.Fatal(err)
	}
	if err := keyFile.Close(); err != nil {
		t.Fatal(err)
	}
	return certPath
}

type liveFTPFixture struct {
	listener net.Listener
	tls      bool
	certFile string
	keyFile  string
	done     chan struct{}
}

func newLiveFTPFixture(t *testing.T, useTLS bool, certPath string) *liveFTPFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &liveFTPFixture{listener: listener, tls: useTLS, certFile: certPath, keyFile: strings.TrimSuffix(certPath, "ftp-ca.pem") + "ftp-server-key.pem", done: make(chan struct{})}
	go f.serve()
	t.Cleanup(f.Close)
	return f
}

func (f *liveFTPFixture) Port() int { return f.listener.Addr().(*net.TCPAddr).Port }

func (f *liveFTPFixture) Close() {
	_ = f.listener.Close()
	select {
	case <-f.done:
	case <-time.After(time.Second):
	}
}

func (f *liveFTPFixture) serve() {
	defer close(f.done)
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *liveFTPFixture) handle(conn net.Conn) {
	defer conn.Close()
	if f.tls {
		cert, err := tlsLoadKeyPair(f.certFile, f.keyFile)
		if err != nil {
			return
		}
		tlsConn := tlsServer(conn, cert)
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		conn = tlsConn
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	send := func(code int, msg string) bool {
		if _, err := fmt.Fprintf(w, "%d %s\r\n", code, msg); err != nil {
			return false
		}
		return w.Flush() == nil
	}
	if !send(220, "aide test FTP service") {
		return
	}
	cwd := "/"
	var passive net.Listener
	defer func() {
		if passive != nil {
			_ = passive.Close()
		}
	}()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		command, arg := strings.ToUpper(parts[0]), ""
		if len(parts) > 1 {
			arg = parts[1]
		}
		switch command {
		case "USER":
			if !send(331, "password required") {
				return
			}
		case "PASS":
			if arg != "fixture-password" {
				_ = send(530, "login incorrect")
				return
			}
			if !send(230, "logged in") {
				return
			}
		case "SYST":
			if !send(215, "UNIX Type: L8") {
				return
			}
		case "FEAT":
			if _, err := w.WriteString("211-Features\r\n UTF8\r\n211 End\r\n"); err != nil || w.Flush() != nil {
				return
			}
		case "OPTS", "NOOP", "TYPE", "PBSZ", "PROT":
			if !send(200, "ok") {
				return
			}
		case "PWD":
			if !send(257, "\""+cwd+"\" is current directory") {
				return
			}
		case "CWD":
			if strings.HasPrefix(arg, "/") {
				cwd = path.Clean(arg)
			} else {
				cwd = path.Join(cwd, arg)
			}
			if cwd != "/pub" && cwd != "/pub/selected" {
				_ = send(550, "not found")
				continue
			}
			if !send(250, "directory changed") {
				return
			}
		case "EPSV":
			if passive != nil {
				_ = passive.Close()
			}
			passive, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				_ = send(425, "passive listener failed")
				return
			}
			p := passive.Addr().(*net.TCPAddr).Port
			if !send(229, fmt.Sprintf("Entering Extended Passive Mode (|||%d|)", p)) {
				return
			}
		case "PASV":
			if passive != nil {
				_ = passive.Close()
			}
			passive, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				_ = send(425, "passive listener failed")
				return
			}
			p := passive.Addr().(*net.TCPAddr).Port
			if !send(227, fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d)", p/256, p%256)) {
				return
			}
		case "LIST", "NLST":
			if passive == nil {
				_ = send(425, "use passive mode")
				continue
			}
			dataListener := passive
			passive = nil
			if !send(150, "opening data connection") {
				_ = dataListener.Close()
				return
			}
			data, acceptErr := dataListener.Accept()
			_ = dataListener.Close()
			if acceptErr != nil {
				_ = send(425, "data connection failed")
				continue
			}
			if f.tls {
				cert, certErr := tlsLoadKeyPair(f.certFile, f.keyFile)
				if certErr != nil {
					_ = data.Close()
					return
				}
				secured := tlsServer(data, cert)
				if handshakeErr := secured.Handshake(); handshakeErr != nil {
					_ = data.Close()
					_ = send(425, "TLS data connection failed")
					continue
				}
				data = secured
			}
			listing := "-rw-r--r-- 1 fixture fixture 23 Oct 13 12:00 root.md\r\n"
			if cwd == "/pub" {
				listing = "drwxr-xr-x 1 fixture fixture 0 Oct 13 12:00 selected\r\n" + listing
			}
			if cwd == "/pub/selected" {
				listing = "-rw-r--r-- 1 fixture fixture 30 Oct 13 12:00 note.md\r\n"
			}
			_, _ = data.Write([]byte(listing))
			_ = data.Close()
			if !send(226, "transfer complete") {
				return
			}
		case "RETR":
			if passive == nil {
				_ = send(425, "use passive mode")
				continue
			}
			dataListener := passive
			passive = nil
			content := []byte("FTP fixture root body\n")
			if strings.Contains(arg, "note.md") {
				content = []byte("FTP fixture nested note body\n")
			}
			if !send(150, "opening data connection") {
				_ = dataListener.Close()
				return
			}
			data, acceptErr := dataListener.Accept()
			_ = dataListener.Close()
			if acceptErr != nil {
				_ = send(425, "data connection failed")
				continue
			}
			if f.tls {
				cert, certErr := tlsLoadKeyPair(f.certFile, f.keyFile)
				if certErr != nil {
					_ = data.Close()
					return
				}
				secured := tlsServer(data, cert)
				if handshakeErr := secured.Handshake(); handshakeErr != nil {
					_ = data.Close()
					_ = send(425, "TLS data connection failed")
					continue
				}
				data = secured
			}
			_, _ = data.Write(content)
			_ = data.Close()
			if !send(226, "transfer complete") {
				return
			}
		case "QUIT":
			_ = send(221, "goodbye")
			return
		default:
			if !send(200, "ok") {
				return
			}
		}
	}
}

// Keep TLS calls behind helpers so the protocol fixture stays visually focused.
func tlsLoadKeyPair(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}
func tlsServer(conn net.Conn, cert tls.Certificate) *tls.Conn {
	return tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
}
