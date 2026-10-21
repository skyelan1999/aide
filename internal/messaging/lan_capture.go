package messaging

// Optional opaque TLS relay for plugin phone collection. Its fixed loopback
// destination is launcher supplied, never read from the writable mailbox.
import (
	"context"
	"errors"
	"net"
	"os"
	"time"
)

type CaptureLease struct {
	Owner     string   `json:"owner"`
	Expires   int64    `json:"expires"`
	Addresses []string `json:"addresses,omitempty"`
}

func CaptureLAN(ctx context.Context, dir, owner string, stop bool) (CaptureLease, error) {
	if !validID(owner) {
		return CaptureLease{}, errors.New("invalid capture owner")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return CaptureLease{}, e
	}
	defer root.Close()
	if _, e = readBridgeLease(root); e != nil {
		return CaptureLease{}, e
	}
	var old CaptureLease
	if bridgeRead(root, "capture-request.json", &old) == nil && old.Expires > time.Now().Unix() && old.Owner != owner {
		return CaptureLease{}, errors.New("another workbench owns phone collection")
	}
	expires := time.Now().Add(45 * time.Second).Unix()
	if stop {
		expires = 0
	}
	if e = bridgeWrite(root, "capture-request.json", CaptureLease{Owner: owner, Expires: expires}); e != nil {
		return CaptureLease{}, e
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var info CaptureLease
		if bridgeRead(root, "capture-host.json", &info) == nil && info.Owner == owner && (stop && info.Expires == 0 || !stop && info.Expires > time.Now().Unix() && len(info.Addresses) > 0) {
			return info, nil
		}
		select {
		case <-ctx.Done():
			return CaptureLease{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func (h *hostBridge) scanCapture() {
	if h.captureTarget == "" {
		return
	}
	var req CaptureLease
	valid := bridgeRead(h.root, "capture-request.json", &req) == nil && validID(req.Owner) && req.Expires > time.Now().Unix() && req.Expires <= time.Now().Add(50*time.Second).Unix()
	if h.capture != nil && (!valid || h.captureOwner != req.Owner) {
		h.capture.close()
		h.capture = nil
	}
	if !valid {
		if validID(req.Owner) {
			_ = bridgeWrite(h.root, "capture-host.json", CaptureLease{Owner: req.Owner})
		}
		return
	}
	if h.capture == nil {
		ctx, cancel := context.WithCancel(h.ctx)
		c := &hostBridge{ctx: ctx, cancel: cancel, target: h.captureTarget, slots: make(chan struct{}, 32)}
		for _, iface := range physicalLANInterfaces() {
			for _, ip := range interfaceIPv4(iface) {
				if len(c.tcp) >= 12 {
					break
				}
				l, e := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
				if e != nil {
					continue
				}
				c.tcp = append(c.tcp, l)
				c.lease.Addresses = append(c.lease.Addresses, "https://"+l.Addr().String())
				c.wg.Add(1)
				go func() {
					defer c.wg.Done()
					for {
						conn, e := l.Accept()
						if e != nil {
							return
						}
						select {
						case c.slots <- struct{}{}:
						default:
							conn.Close()
							continue
						}
						c.wg.Add(1)
						go func() { defer c.wg.Done(); defer func() { <-c.slots }(); c.forward(conn) }()
					}
				}()
			}
		}
		h.capture = c
		h.captureOwner = req.Owner
	}
	_ = bridgeWrite(h.root, "capture-host.json", CaptureLease{Owner: req.Owner, Expires: req.Expires, Addresses: h.capture.lease.Addresses})
}

// Release only the matching workbench lease, preserving other active owners.
func ReleaseCaptureLAN(dir, owner string) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return
	}
	defer root.Close()
	var req CaptureLease
	if bridgeRead(root, "capture-request.json", &req) == nil && req.Owner == owner {
		_ = bridgeWrite(root, "capture-request.json", CaptureLease{Owner: owner})
	}
}
