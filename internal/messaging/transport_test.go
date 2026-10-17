package messaging

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRealMutualTLSBoundToDeviceNotAddress(t *testing.T) {
	a, b, wrong := testIdentity(t, 1), testIdentity(t, 2), testIdentity(t, 3)
	ca, e := a.Certificate()
	if e != nil {
		t.Fatal(e)
	}
	cb, e := b.Certificate()
	if e != nil {
		t.Fatal(e)
	}
	cw, e := wrong.Certificate()
	if e != nil {
		t.Fatal(e)
	}
	ln, e := tls.Listen("tcp", "127.0.0.1:0", ServerTLS(cb))
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := AuthenticatedPeer(r)
		if err != nil || peer.Fingerprint != a.Device().Fingerprint {
			http.Error(w, "denied", 403)
			return
		}
		io.WriteString(w, "private payload")
	}), ReadHeaderTimeout: time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	address := "https://" + ln.Addr().String()
	card, e := b.Card(cb, "test", []string{address})
	if e != nil {
		t.Fatal(e)
	}
	client, e := PeerClient(ca, card, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.CloseIdleConnections()
	response, e := client.Get(address)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != "private payload" {
		t.Fatal("encrypted peer exchange failed")
	}
	// A routing endpoint can change; redirecting the selected key to a different
	// TLS identity must fail before sending the HTTP request.
	evil, e := tls.Listen("tcp", "127.0.0.1:0", ServerTLS(cw))
	if e != nil {
		t.Fatal(e)
	}
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("wrong identity received payload") }), ReadHeaderTimeout: time.Second}
	go other.Serve(evil)
	defer other.Close()
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, evil.Addr().String())
	}
	pinned, e := PeerClient(ca, card, dial)
	if e != nil {
		t.Fatal(e)
	}
	defer pinned.CloseIdleConnections()
	if res, err := pinned.Get(address); err == nil {
		res.Body.Close()
		t.Fatal("wrong identity accepted")
	}
	card.Device = wrong.Device()
	if card.Validate(time.Now()) == nil {
		t.Fatal("substituted discovery identity accepted")
	}
}

// A trusted discovery certificate must not delegate the selected identity to a
// different leaf key even when the ordinary X.509 chain is otherwise valid.
func TestRealTLSRejectsDelegatedDifferentLeafKey(t *testing.T) {
	a, b, other := testIdentity(t, 11), testIdentity(t, 12), testIdentity(t, 13)
	ca, _ := a.Certificate()
	root, _ := b.Certificate()
	template := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{peerName}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, root.Leaf, other.private.Public(), b.private)
	if err != nil {
		t.Fatal(err)
	}
	leaf := tls.Certificate{Certificate: [][]byte{der, root.Certificate[0]}, PrivateKey: other.private}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{leaf}})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("delegated leaf received request") }), ReadHeaderTimeout: time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	address := "https://" + ln.Addr().String()
	card, err := b.Card(root, "selected", []string{address})
	if err != nil {
		t.Fatal(err)
	}
	client, err := PeerClient(ca, card, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if response, err := client.Get(address); err == nil {
		response.Body.Close()
		t.Fatal("accepted different leaf key under selected device root")
	}
}
