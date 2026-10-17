package messaging

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type wireCapture struct {
	sync.Mutex
	data []byte
}
type captureConn struct {
	net.Conn
	capture *wireCapture
}

func (c captureConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	c.capture.Lock()
	c.capture.data = append(c.capture.data, p[:n]...)
	c.capture.Unlock()
	return n, e
}

type captureListener struct {
	net.Listener
	capture *wireCapture
}

func (l captureListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return captureConn{c, l.capture}, nil
}

func TestLANBridgeDockerStyleDiscoveryConsentAndOpaqueTLS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := newTestNode(t, 91), newTestNode(t, 92)
	type side struct {
		host    *hostBridge
		network *Network
		node    *testNode
		wire    *wireCapture
		done    chan error
	}
	makeSide := func(node *testNode) *side {
		root, e := bridgeOpen(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		n, e := StartNetwork(ctx, node.s, NetworkConfig{Listen: "127.0.0.1:0", LAN: true, BridgeDir: root.Name()})
		if e != nil {
			t.Fatal(e)
		}
		tcp, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		udp, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if e != nil {
			t.Fatal(e)
		}
		hctx, hcancel := context.WithCancel(ctx)
		wire := &wireCapture{}
		host := &hostBridge{root: root, ctx: hctx, cancel: hcancel, target: n.listener.Addr().String(), lease: bridgeLease{Instance: randomID(), Addresses: []string{"https://" + tcp.Addr().String()}}, tcp: []net.Listener{tcp}, udp: []*net.UDPConn{udp}, slots: make(chan struct{}, 64), jobs: make(chan struct{}, 4)}
		if e = host.heartbeat(); e != nil {
			t.Fatal(e)
		}
		if !n.publishBridge() {
			t.Fatal("bridge did not publish signed host addresses")
		}
		host.wg.Add(2)
		go func() { defer host.wg.Done(); host.serveTCP(captureListener{tcp, wire}) }()
		go func() { defer host.wg.Done(); host.serveUDP(udp) }()
		return &side{host: host, network: n, node: node, wire: wire, done: make(chan error, 1)}
	}
	left, right := makeSide(a), makeSide(b)
	for _, pair := range [][2]*side{{left, right}, {right, left}} {
		local, remote := pair[0], pair[1]
		local.host.lookup = func(ctx context.Context, code string) ([]Card, error) {
			conn, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if e != nil {
				return nil, e
			}
			defer conn.Close()
			return discoverOnConn(ctx, conn, code, remote.host.udp[0].LocalAddr().(*net.UDPAddr))
		}
		go func() { local.done <- local.host.run() }()
		t.Cleanup(func() { local.host.cancel(); <-local.done; local.network.Close(); local.host.close() })
	}
	cards, e := left.network.Discover(ctx, b.s.identity.Device().Code)
	if e != nil || len(cards) != 1 {
		t.Fatalf("Docker-side mailbox discovery: %v, %d cards", e, len(cards))
	}
	if cards[0].Device.Fingerprint != b.s.identity.Device().Fingerprint || cards[0].Addresses[0] != right.host.lease.Addresses[0] {
		t.Fatal("discovery changed identity or exposed container address")
	}
	a.card, _ = a.s.Card()
	b.card = cards[0]
	if e = a.s.SendText(ctx, b.card.Device.Fingerprint, "", "before consent"); e == nil {
		t.Fatal("bridge bypassed contact consent")
	}
	connectTestNodes(t, a, b)
	const secret = "private chat through Docker LAN bridge"
	if e = a.s.SendText(ctx, b.card.Device.Fingerprint, "", secret); e != nil {
		t.Fatal(e)
	}
	b.s.mu.Lock()
	messages := append([]Message(nil), b.s.state.Messages...)
	b.s.mu.Unlock()
	if len(messages) != 1 || messages[0].Text != secret {
		t.Fatal("encrypted message did not arrive")
	}
	right.wire.Lock()
	wire := append([]byte(nil), right.wire.data...)
	right.wire.Unlock()
	if len(wire) == 0 || bytes.Contains(wire, []byte(secret)) {
		t.Fatal("native forwarder observed message plaintext")
	}
	// A helper can advertise a route, but cannot substitute another device key.
	tampered := cards[0]
	tampered.Name = "changed by host"
	if tampered.Validate(time.Now()) == nil {
		t.Fatal("unsigned bridge substitution accepted")
	}
	if e = b.s.Block(a.card.Device.Fingerprint, true); e != nil {
		t.Fatal(e)
	}
	if e = a.s.SendText(ctx, b.card.Device.Fingerprint, "", "blocked"); e == nil {
		t.Fatal("bridge bypassed block")
	}
}

func TestLANBridgeStaleLeaseAndRootedMailbox(t *testing.T) {
	dir := t.TempDir()
	root, e := bridgeOpen(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	lease := bridgeLease{Instance: randomID(), Expires: time.Now().Add(-time.Second).Unix(), Addresses: []string{"https://127.0.0.1:12345"}}
	if e = bridgeWrite(root, "host.json", lease); e != nil {
		t.Fatal(e)
	}
	if LANBridgeReady(dir) {
		t.Fatal("expired helper lease accepted")
	}
	if _, e = bridgeDiscover(context.Background(), dir, "ABCDEF"); e == nil {
		t.Fatal("lookup used stale helper")
	}
	lease.Expires = time.Now().Add(time.Hour).Unix()
	_ = bridgeWrite(root, "host.json", lease)
	if LANBridgeReady(dir) {
		t.Fatal("unbounded lease accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	_ = os.WriteFile(outside, []byte(`{"code":"ABCDEF"}`), 0600)
	if e = os.Symlink(outside, filepath.Join(dir, "query-"+randomID()+".json")); e == nil {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "query-") {
				var q bridgeQuery
				if bridgeRead(root, entry.Name(), &q) == nil {
					t.Fatal("mailbox escaped its root")
				}
			}
		}
	}
	for _, name := range []string{"query-../../secret.json", "query-abc.json", "query-ABCDEF.json"} {
		if bridgeJobID(name, "query-") != "" {
			t.Fatal("unsafe job name accepted")
		}
	}
}

func TestLANBridgeKeepsAnnouncingDuringSlowApprovalRetry(t *testing.T) {
	a, b := newTestNode(t, 93), newTestNode(t, 94)
	root, err := bridgeOpen(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lease := bridgeLease{Instance: randomID(), Expires: time.Now().Add(15 * time.Second).Unix(), Addresses: []string{"https://127.0.0.1:12345"}}
	if err = bridgeWrite(root, "host.json", lease); err != nil {
		t.Fatal(err)
	}
	n, err := StartNetwork(context.Background(), a.s, NetworkConfig{Listen: "127.0.0.1:0", LAN: true, BridgeDir: root.Name()})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err = b.s.SetAddresses(nil); err != nil {
		t.Fatal(err)
	}
	b.card, err = b.s.Card()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.s.Remember(b.card); err != nil {
		t.Fatal(err)
	}
	if _, err = a.s.consent.Request(b.card.Device.Fingerprint, Contact, "contact", "outgoing", ""); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	a.s.mu.Lock()
	a.s.dial = func(ctx context.Context, _ string, _ Card) (net.Conn, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a.s.mu.Unlock()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("approval retry did not begin")
	}
	var before, after bridgeAnnouncement
	if err = bridgeRead(root, "announce.json", &before); err != nil {
		t.Fatal(err)
	}
	// The retry has a 3s deadline; the independent heartbeat must renew inside it.
	time.Sleep(2200 * time.Millisecond)
	if err = bridgeRead(root, "announce.json", &after); err != nil || after.Expires <= before.Expires {
		t.Fatal("slow approval retry stalled the bridge announcement", err)
	}
}

func TestLANBridgeRejectsForeignReplyAndUnsafeTarget(t *testing.T) {
	dir := t.TempDir()
	root, _ := bridgeOpen(dir)
	defer root.Close()
	lease := bridgeLease{Instance: randomID(), Expires: time.Now().Add(5 * time.Second).Unix(), Addresses: []string{"https://127.0.0.1:12345"}}
	_ = bridgeWrite(root, "host.json", lease)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, e := bridgeDiscover(ctx, dir, "ABCDEF"); result <- e }()
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(dir)
		found := false
		for _, entry := range entries {
			if id := bridgeJobID(entry.Name(), "query-"); id != "" {
				_ = bridgeWrite(root, "reply-"+id+".json", bridgeReply{Instance: randomID()})
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e := <-result; e == nil {
		t.Fatal("reply from another bridge generation accepted")
	}
	// The launcher never accepts a public/non-loopback forwarding destination.
	for _, target := range []string{"example.com:443", "192.168.1.1:45840", "127.0.0.1:0"} {
		if e := RunLANBridge(ctx, dir, target); e == nil {
			t.Fatal("unsafe target accepted")
		}
	}
}

func TestLANBridgeStopRequiresMatchingGeneration(t *testing.T) {
	root, e := bridgeOpen(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &hostBridge{root: root, ctx: ctx, cancel: cancel, target: "127.0.0.1:1", lease: bridgeLease{Instance: randomID(), Addresses: []string{"https://127.0.0.1:2"}}, jobs: make(chan struct{}, 4)}
	if e = h.heartbeat(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { e := h.run(); h.close(); done <- e }()
	_ = bridgeWrite(root, "stop.json", struct {
		Instance string `json:"instance"`
	}{randomID()})
	select {
	case e := <-done:
		t.Fatalf("foreign generation stopped helper: %v", e)
	case <-time.After(160 * time.Millisecond):
	}
	if e = StopLANBridge(root.Name()); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if LANBridgeReady(root.Name()) {
		t.Fatal("stopped helper lease remained active")
	}
}
