package messaging

// Optional opaque TLS relay for password-protected LAN workbench access. Its fixed loopback
// destination is launcher supplied, never read from the writable mailbox.
import (
	"context"
	"errors"
	"net"
	"os"
	"time"
)

type RemoteAccessLease struct {
	Owner     string   `json:"owner"`
	Expires   int64    `json:"expires"`
	Addresses []string `json:"addresses,omitempty"`
}

func RemoteAccessLAN(ctx context.Context, dir, owner string, stop bool) (RemoteAccessLease, error) {
	if !validID(owner) {
		return RemoteAccessLease{}, errors.New("invalid remote access owner")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return RemoteAccessLease{}, e
	}
	defer root.Close()
	if _, e = readBridgeLease(root); e != nil {
		return RemoteAccessLease{}, e
	}
	var old RemoteAccessLease
	if bridgeRead(root, "remote-request.json", &old) == nil && old.Expires > time.Now().Unix() && old.Owner != owner {
		return RemoteAccessLease{}, errors.New("another owner holds remote access")
	}
	expires := time.Now().Add(45 * time.Second).Unix()
	if stop {
		expires = 0
	}
	if e = bridgeWrite(root, "remote-request.json", RemoteAccessLease{Owner: owner, Expires: expires}); e != nil {
		return RemoteAccessLease{}, e
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var info RemoteAccessLease
		if bridgeRead(root, "remote-host.json", &info) == nil && info.Owner == owner && (stop && info.Expires == 0 || !stop && info.Expires > time.Now().Unix() && len(info.Addresses) > 0) {
			return info, nil
		}
		select {
		case <-ctx.Done():
			return RemoteAccessLease{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func (h *hostBridge) scanRemoteAccess() {
	if h.remoteTarget == "" {
		return
	}
	var req RemoteAccessLease
	valid := bridgeRead(h.root, "remote-request.json", &req) == nil && validID(req.Owner) && req.Expires > time.Now().Unix() && req.Expires <= time.Now().Add(50*time.Second).Unix()
	if h.remote != nil && (!valid || h.remoteOwner != req.Owner || len(h.remote.tcp) == 0) {
		h.remote.close()
		h.remote = nil
	}
	if !valid {
		if validID(req.Owner) {
			_ = bridgeWrite(h.root, "remote-host.json", RemoteAccessLease{Owner: req.Owner})
		}
		return
	}
	if h.remote == nil {
		ctx, cancel := context.WithCancel(h.ctx)
		c := &hostBridge{ctx: ctx, cancel: cancel, target: h.remoteTarget, slots: make(chan struct{}, 32)}
		for _, iface := range physicalLANInterfaces() {
			for _, ip := range interfaceIPv4(iface) {
				if !ip.IsPrivate() {
					continue
				}
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
		h.remote = c
		h.remoteOwner = req.Owner
	}
	_ = bridgeWrite(h.root, "remote-host.json", RemoteAccessLease{Owner: req.Owner, Expires: req.Expires, Addresses: h.remote.lease.Addresses})
}

// Release only the matching authorization lease, preserving other active owners.
func ReleaseRemoteAccessLAN(dir, owner string) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return
	}
	defer root.Close()
	var req RemoteAccessLease
	if bridgeRead(root, "remote-request.json", &req) == nil && req.Owner == owner {
		_ = bridgeWrite(root, "remote-request.json", RemoteAccessLease{Owner: owner})
	}
}
