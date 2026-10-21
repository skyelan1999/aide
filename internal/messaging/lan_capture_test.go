package messaging

import (
	"context"
	"io"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestCaptureRelayOnDemandLeaseAndFixedDestination(t *testing.T) {
	target, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	root, e := bridgeOpen(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &hostBridge{root: root, ctx: ctx, cancel: cancel, captureTarget: target.Addr().String()}
	defer func() {
		if h.capture != nil {
			h.capture.close()
		}
	}()
	h.scanCapture()
	if h.capture != nil {
		t.Fatal("listener without opt-in")
	}
	owner := randomID()
	bridgeWrite(root, "capture-request.json", CaptureLease{Owner: owner, Expires: time.Now().Add(45 * time.Second).Unix()})
	h.scanCapture()
	if h.capture == nil || len(h.capture.tcp) == 0 {
		t.Skip("no physical interface")
	}
	endpoint := h.capture.lease.Addresses[0]
	u, _ := url.Parse(endpoint)
	conn, e := net.DialTimeout("tcp", u.Host, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte("opaque TLS bytes"))
	b := make([]byte, 16)
	if _, e = io.ReadFull(conn, b); e != nil || string(b) != "opaque TLS bytes" {
		t.Fatal(e, string(b))
	}
	bridgeWrite(root, "capture-request.json", CaptureLease{Owner: owner})
	h.scanCapture()
	if h.capture != nil {
		t.Fatal("listener survived stop")
	}
	if c, e := net.DialTimeout("tcp", u.Host, time.Second); e == nil {
		c.Close()
		t.Fatal("stopped port accessible")
	}
	if e := RunLANBridgeWithCapture(ctx, t.TempDir(), "127.0.0.1:9", "192.168.1.1:9"); e == nil {
		t.Fatal("non-loopback relay target accepted")
	}
}
