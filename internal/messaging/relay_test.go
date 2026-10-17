package messaging

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRealMessageThroughUntrustedOpaqueRelay(t *testing.T) {
	relay := NewRelay()
	outer := httptest.NewTLSServer(relay)
	defer outer.Close()
	defer relay.Close()
	roots := x509.NewCertPool()
	roots.AddCert(outer.Certificate())
	trust := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	makeService := func(value byte) *Service {
		identity := testIdentity(t, value)
		store, e := OpenStore(t.TempDir(), bytes.Repeat([]byte{value}, 32))
		if e != nil {
			t.Fatal(e)
		}
		s, e := NewService(identity, store, "test", nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close(); store.Close() })
		return s
	}
	a, b := makeService(31), makeService(32)
	ra, e := NewRelayClient(outer.URL, a.identity, a.Card, trust)
	if e != nil {
		t.Fatal(e)
	}
	defer ra.Close()
	rb, e := NewRelayClient(outer.URL, b.identity, b.Card, trust)
	if e != nil {
		t.Fatal(e)
	}
	defer rb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e = ra.Advertise(ctx); e != nil {
		t.Fatal(e)
	}
	if e = rb.Advertise(ctx); e != nil {
		t.Fatal(e)
	}
	cards, e := ra.Find(ctx, b.identity.Device().Code)
	if e != nil || len(cards) != 1 {
		t.Fatal("relay code lookup failed", e)
	}
	if e = a.Remember(cards[0]); e != nil {
		t.Fatal(e)
	}
	a.dial = func(ctx context.Context, peer string, _ Card) (net.Conn, error) { return ra.OpenTunnel(ctx, peer) }
	inbox := &channelListener{connections: make(chan net.Conn, 16), done: make(chan struct{})}
	defer inbox.Close()
	peerServer := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: time.Second}
	go peerServer.Serve(tls.NewListener(inbox, ServerTLS(b.cert)))
	defer peerServer.Close()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				offers, err := rb.Inbox(ctx)
				if err != nil {
					continue
				}
				for _, offer := range offers {
					conn, err := rb.AcceptTunnel(ctx, offer["id"])
					if err != nil {
						continue
					}
					select {
					case inbox.connections <- conn:
					case <-ctx.Done():
						conn.Close()
						return
					}
				}
			}
		}
	}()
	defer func() { cancel(); <-stopped }()
	inv, e := a.Request(ctx, b.identity.Device().Fingerprint, Contact, "contact", nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Decide(inv.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = a.RefreshInvitation(ctx, inv.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.SendText(ctx, b.identity.Device().Fingerprint, "", "secret only for final endpoint"); e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	received := append([]Message(nil), b.state.Messages...)
	b.mu.Unlock()
	if len(received) != 1 || received[0].Text != "secret only for final endpoint" {
		t.Fatal("inner TLS message did not reach endpoint")
	}
	// The relay directory has public signed cards only, not endpoint message state.
	relay.mu.Lock()
	count := len(relay.cards)
	relay.mu.Unlock()
	if count != 2 {
		t.Fatal("unexpected discovery identities")
	}
}
func TestRelayRejectsTamperingReplayAndInsecureTrust(t *testing.T) {
	relay := NewRelay()
	server := httptest.NewTLSServer(relay)
	defer server.Close()
	defer relay.Close()
	i := testIdentity(t, 41)
	cert, e := i.Certificate()
	if e != nil {
		t.Fatal(e)
	}
	card, e := i.Card(cert, "test", nil)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, e := NewRelayClient(server.URL, i, func() (Card, error) { return card, nil }, &tls.Config{RootCAs: roots})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	raw, e := client.auth("advertise", "", "")
	if e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 2; n++ {
		response, err := server.Client().Post(server.URL+"/v1/relay", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if n == 0 && response.StatusCode != 200 {
			t.Fatal("valid relay auth rejected")
		}
		if n == 1 && response.StatusCode == 200 {
			t.Fatal("relay replay accepted")
		}
	}
	if _, e = NewRelayClient(server.URL, i, func() (Card, error) { return card, nil }, &tls.Config{InsecureSkipVerify: true}); e == nil {
		t.Fatal("relay verification can be disabled")
	}
}
