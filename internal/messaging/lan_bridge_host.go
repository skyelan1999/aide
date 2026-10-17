package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// No private keys, credentials or application plaintext are held by this helper.
// Its TCP listeners forward opaque streams into the loopback-only Docker port.
type hostBridge struct {
	root   *os.Root
	lease  bridgeLease
	ctx    context.Context
	cancel context.CancelFunc
	target string
	tcp    []net.Listener
	udp    []*net.UDPConn
	wg     sync.WaitGroup
	slots  chan struct{}
	jobs   chan struct{}
	lookup func(context.Context, string) ([]Card, error)
}

func physicalLANInterfaces() []net.Interface {
	all, _ := net.Interfaces()
	out := []net.Interface{}
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		excluded := false
		for _, prefix := range []string{"utun", "tun", "tap", "wg", "tailscale", "docker", "veth", "br-", "virbr", "vmnet", "vbox", "awdl", "llw"} {
			if strings.HasPrefix(strings.ToLower(iface.Name), prefix) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, iface)
		}
	}
	return out
}
func interfaceIPv4(iface net.Interface) []net.IP {
	addresses, _ := iface.Addrs()
	out := []net.IP{}
	for _, address := range addresses {
		ip, _, e := net.ParseCIDR(address.String())
		if e == nil && ip.To4() != nil && !ip.IsLoopback() && ip.IsGlobalUnicast() {
			out = append(out, ip.To4())
		}
	}
	return out
}
func RunLANBridge(parent context.Context, dir, target string) error {
	// This is deliberately not a general-purpose forward proxy.
	host, port, e := net.SplitHostPort(target)
	ip := net.ParseIP(host)
	p, _ := strconv.Atoi(port)
	if e != nil || ip == nil || !ip.IsLoopback() || p < 1 || p > 65535 {
		return errors.New("bridge target must be a numeric loopback address and port")
	}
	root, e := bridgeOpen(dir)
	if e != nil {
		return e
	}
	if _, e = readBridgeLease(root); e == nil {
		root.Close()
		return errors.New("a LAN bridge already owns this directory")
	}
	ctx, cancel := context.WithCancel(parent)
	h := &hostBridge{root: root, ctx: ctx, cancel: cancel, target: target, lease: bridgeLease{Instance: randomID()}, slots: make(chan struct{}, 64), jobs: make(chan struct{}, 4)}
	defer h.close()
	interfaces := physicalLANInterfaces()
	for _, iface := range interfaces {
		for _, ip := range interfaceIPv4(iface) {
			if len(h.lease.Addresses) >= 12 {
				break
			}
			listener, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
			if err != nil {
				continue
			}
			h.tcp = append(h.tcp, listener)
			h.lease.Addresses = append(h.lease.Addresses, "https://"+listener.Addr().String())
			h.wg.Add(1)
			go func() { defer h.wg.Done(); h.serveTCP(listener) }()
		}
		conn, err := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{IP: net.ParseIP("239.255.76.33"), Port: 45839})
		if err != nil {
			continue
		}
		h.udp = append(h.udp, conn)
		h.wg.Add(1)
		go func() { defer h.wg.Done(); h.serveUDP(conn) }()
	}
	if len(h.lease.Addresses) == 0 || len(h.udp) == 0 {
		return errors.New("no usable physical LAN interface; check network access/firewall")
	}
	h.lookup = func(ctx context.Context, code string) ([]Card, error) { return discoverHostLAN(ctx, interfaces, code) }
	return h.run()
}
func (h *hostBridge) run() error {
	if err := h.heartbeat(); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	nextHeartbeat := time.Now().Add(time.Second)
	unavailable := time.Time{}
	for {
		select {
		case <-h.ctx.Done():
			return nil
		case <-ticker.C:
			var stop struct {
				Instance string `json:"instance"`
			}
			if bridgeRead(h.root, "stop.json", &stop) == nil && stop.Instance == h.lease.Instance {
				return nil
			}
			if !time.Now().Before(nextHeartbeat) {
				if err := h.heartbeat(); err != nil {
					return err
				}
				nextHeartbeat = time.Now().Add(time.Second)
				// Stop after the owning Docker mapping disappears, not when messaging is disabled.
				if _, err := h.announcement(); err == nil {
					unavailable = time.Time{}
				} else {
					c, err := net.DialTimeout("tcp", h.target, 150*time.Millisecond)
					if err == nil {
						c.Close()
						unavailable = time.Time{}
					} else if unavailable.IsZero() {
						unavailable = time.Now()
					} else if time.Since(unavailable) > 30*time.Second {
						return nil
					}
				}
			}
			h.scanJobs()
		}
	}
}
func (h *hostBridge) heartbeat() error {
	var current bridgeLease
	if bridgeRead(h.root, "host.json", &current) == nil && current.Instance != h.lease.Instance && current.Expires > time.Now().Unix() {
		return errors.New("LAN bridge directory ownership changed")
	}
	h.lease.Expires = time.Now().Add(5 * time.Second).Unix()
	return bridgeWrite(h.root, "host.json", h.lease)
}
func (h *hostBridge) announcement() (bridgeAnnouncement, error) {
	var a bridgeAnnouncement
	if err := bridgeRead(h.root, "announce.json", &a); err != nil {
		return a, err
	}
	now := time.Now()
	if a.Instance != h.lease.Instance || !validID(a.Owner) || a.Expires <= now.Unix() || a.Expires > now.Add(10*time.Second).Unix() {
		return a, errors.New("inactive device")
	}
	return a, a.Card.Validate(now)
}
func (h *hostBridge) serveTCP(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if _, err = h.announcement(); err != nil {
			conn.Close()
			continue
		}
		select {
		case h.slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		h.wg.Add(1)
		go func() { defer h.wg.Done(); defer func() { <-h.slots }(); h.forward(conn) }()
	}
}
func (h *hostBridge) forward(conn net.Conn) {
	defer conn.Close()
	stop := context.AfterFunc(h.ctx, func() { conn.Close() })
	defer stop()
	backend, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(h.ctx, "tcp", h.target)
	if err != nil {
		return
	}
	defer backend.Close()
	stopBackend := context.AfterFunc(h.ctx, func() { backend.Close() })
	defer stopBackend()
	copied := make(chan struct{})
	go func() { _, _ = io.Copy(backend, conn); backend.Close(); close(copied) }()
	_, _ = io.Copy(conn, backend)
	conn.Close()
	<-copied
}
func (h *hostBridge) serveUDP(conn *net.UDPConn) {
	buf := make([]byte, 513)
	last := map[string]time.Time{}
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n > 512 {
			continue
		}
		var q discoveryPacket
		if json.Unmarshal(buf[:n], &q) != nil || q.Card != nil || !ValidCode(q.Code) || !validID(q.Nonce) {
			continue
		}
		now := time.Now()
		if now.Sub(last[addr.IP.String()]) < time.Second {
			continue
		}
		if len(last) > 1024 {
			for key, at := range last {
				if now.Sub(at) > time.Minute {
					delete(last, key)
				}
			}
			if len(last) > 1024 {
				continue
			}
		}
		a, err := h.announcement()
		if err != nil || a.Card.Device.Code != q.Code {
			continue
		}
		last[addr.IP.String()] = now
		q.Card = &a.Card
		data, _ := json.Marshal(q)
		_, _ = conn.WriteToUDP(data, addr)
	}
}
func (h *hostBridge) scanJobs() {
	directory, err := h.root.Open(".")
	if err != nil {
		return
	}
	entries, err := directory.ReadDir(128)
	directory.Close()
	if err != nil && err != io.EOF {
		return
	}
	checked := 0
	for _, entry := range entries {
		if checked >= 64 {
			return
		}
		id := bridgeJobID(entry.Name(), "query-")
		if id == "" {
			if bridgeJobID(entry.Name(), "reply-") != "" {
				if info, e := entry.Info(); e == nil && time.Since(info.ModTime()) > 10*time.Second {
					_ = h.root.Remove(entry.Name())
				}
			}
			continue
		}
		checked++
		var query bridgeQuery
		name := entry.Name()
		now := time.Now().Unix()
		if bridgeRead(h.root, name, &query) != nil || query.Instance != h.lease.Instance || !ValidCode(query.Code) || query.Expires <= now || query.Expires > now+5 {
			_ = h.root.Remove(name)
			continue
		}
		select {
		case h.jobs <- struct{}{}:
		default:
			return
		}
		_ = h.root.Remove(name)
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer func() { <-h.jobs }()
			ctx, cancel := context.WithTimeout(h.ctx, 1200*time.Millisecond)
			defer cancel()
			cards, _ := h.lookup(ctx, query.Code)
			out := []Card{}
			for _, card := range cards {
				if len(out) < 16 && card.Device.Code == query.Code && card.Validate(time.Now()) == nil {
					out = append(out, card)
				}
			}
			_ = bridgeWrite(h.root, "reply-"+id+".json", bridgeReply{Instance: h.lease.Instance, Cards: out})
		}()
	}
}
func (h *hostBridge) close() {
	h.cancel()
	for _, l := range h.tcp {
		l.Close()
	}
	for _, c := range h.udp {
		c.Close()
	}
	h.wg.Wait()
	var lease bridgeLease
	if bridgeRead(h.root, "host.json", &lease) == nil && lease.Instance == h.lease.Instance {
		_ = h.root.Remove("host.json")
	}
	var stop struct {
		Instance string `json:"instance"`
	}
	if bridgeRead(h.root, "stop.json", &stop) == nil && stop.Instance == h.lease.Instance {
		_ = h.root.Remove("stop.json")
	}
	h.root.Close()
}
func StopLANBridge(dir string) error {
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	lease, err := readBridgeLease(root)
	if err != nil {
		return nil
	}
	if err = bridgeWrite(root, "stop.json", struct {
		Instance string `json:"instance"`
	}{lease.Instance}); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var current bridgeLease
		if bridgeRead(root, "host.json", &current) != nil || current.Instance != lease.Instance {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("LAN bridge did not stop within deadline")
}
func discoverHostLAN(ctx context.Context, interfaces []net.Interface, code string) ([]Card, error) {
	var mu sync.Mutex
	out := []Card{}
	var wg sync.WaitGroup
	for _, iface := range interfaces {
		for _, ip := range interfaceIPv4(iface) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
				if err != nil {
					return
				}
				defer conn.Close()
				if setDiscoveryInterface(conn, ip) != nil {
					return
				}
				cards, _ := discoverOnConn(ctx, conn, code, &net.UDPAddr{IP: net.ParseIP("239.255.76.33"), Port: 45839})
				mu.Lock()
				out = append(out, cards...)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	return out, nil
}
func discoverOnConn(ctx context.Context, conn *net.UDPConn, code string, target *net.UDPAddr) ([]Card, error) {
	deadline := time.Now().Add(1100 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	nonce := randomID()
	payload, _ := json.Marshal(discoveryPacket{Code: code, Nonce: nonce})
	if _, err := conn.WriteToUDP(payload, target); err != nil {
		return nil, err
	}
	cards := []Card{}
	buffer := make([]byte, 16<<10)
	for len(cards) < 16 {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			break
		}
		var p discoveryPacket
		if json.Unmarshal(buffer[:n], &p) == nil && p.Nonce == nonce && p.Code == code && p.Card != nil && p.Card.Validate(time.Now()) == nil {
			cards = append(cards, *p.Card)
		}
	}
	return cards, nil
}
