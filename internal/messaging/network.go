package messaging

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

type NetworkConfig struct {
	// Launcher-owned private mailbox, never accepted from browser configuration.
	BridgeDir string   `json:"-"`
	Listen    string   `json:"listen"`
	Advertise []string `json:"advertise"`
	LAN       bool     `json:"lan"`
	Relays    []string `json:"relays"`
}
type Network struct {
	bridgeOwner string
	service     *Service
	config      NetworkConfig
	ctx         context.Context
	cancel      context.CancelFunc
	listener    net.Listener
	tunnel      *channelListener
	servers     []*http.Server
	relays      []*RelayClient
	udp         *net.UDPConn
	wg          sync.WaitGroup
	mu          sync.Mutex
	relayStatus map[string]string
}

func StartNetwork(parent context.Context, s *Service, config NetworkConfig) (*Network, error) {
	if len(config.Relays) > 4 || len(config.Advertise) > 12 {
		return nil, errors.New("too many relay or advertised addresses")
	}
	if config.Listen == "" {
		config.Listen = ":45840"
	}
	if err := ValidateListen(config.Listen); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	n := &Network{bridgeOwner: randomID(), service: s, config: config, ctx: ctx, cancel: cancel, listener: listener, relayStatus: map[string]string{}}
	n.tunnel = &channelListener{connections: make(chan net.Conn, 16), done: make(chan struct{})}
	if err = n.refreshAddresses(); err != nil {
		listener.Close()
		cancel()
		return nil, err
	}
	for _, endpoint := range config.Relays {
		relay, e := NewRelayClient(endpoint, s.identity, s.Card, nil)
		if e != nil {
			n.Close()
			return nil, e
		}
		n.relays = append(n.relays, relay)
	}
	s.mu.Lock()
	s.dial = func(ctx context.Context, peer string, known Card) (net.Conn, error) {
		var last error = errors.New("no configured relay available")
		if config.LAN {
			cards, _ := n.discover(ctx, known.Device.Code)
			for _, card := range cards {
				if card.Device.Fingerprint != peer || card.Validate(time.Now()) != nil {
					continue
				}
				_ = s.Remember(card)
				for _, address := range card.Addresses {
					u, e := ParseAddress(address)
					if e != nil {
						continue
					}
					host := u.Host
					if u.Port() == "" {
						host = net.JoinHostPort(u.Hostname(), "443")
					}
					conn, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", host)
					if e == nil {
						return conn, nil
					}
					last = e
				}
			}
		}
		for _, relay := range n.relays {
			conn, err := relay.OpenTunnel(ctx, peer)
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	s.mu.Unlock()
	for _, ln := range []net.Listener{listener, n.tunnel} {
		server := &http.Server{Handler: s.rateLimitedHandler(), TLSConfig: ServerTLS(s.cert), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 45 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		n.servers = append(n.servers, server)
		n.wg.Add(1)
		go func(server *http.Server, ln net.Listener) {
			defer n.wg.Done()
			_ = server.Serve(tls.NewListener(newBoundedListener(ln, 64), server.TLSConfig))
		}(server, ln)
	}
	if config.LAN && config.BridgeDir == "" {
		n.udp, err = net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("239.255.76.33"), Port: 45839})
		if err != nil {
			n.Close()
			return nil, err
		}
		n.wg.Add(1)
		go func() { defer n.wg.Done(); n.serveDiscovery() }()
	}
	for _, relay := range n.relays {
		n.wg.Add(1)
		go func(relay *RelayClient) { defer n.wg.Done(); n.maintainRelay(relay) }(relay)
	}
	if config.LAN && config.BridgeDir != "" {
		// Approval retries may take many seconds; keep the host lease alive independently.
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-n.ctx.Done():
					return
				case <-ticker.C:
					_ = n.refreshAddresses()
				}
			}
		}()
	}
	n.wg.Add(1)
	go func() { defer n.wg.Done(); n.refreshPending() }()
	return n, nil
}
func (n *Network) refreshAddresses() error {
	if n.publishBridge() {
		return nil
	}
	if len(n.config.Advertise) > 0 {
		return n.service.SetAddresses(n.config.Advertise)
	}
	_, port, err := net.SplitHostPort(n.listener.Addr().String())
	if err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(n.config.Listen)
	if parsed := net.ParseIP(host); parsed != nil && !parsed.IsUnspecified() {
		return n.service.SetAddresses([]string{"https://" + net.JoinHostPort(host, port)})
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	out := []string{}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, "https://"+net.JoinHostPort(ip.String(), port))
		if len(out) >= 12 {
			break
		}
	}
	return n.service.SetAddresses(out)
}
func (n *Network) Close() {
	n.cancel()
	if n.listener != nil {
		n.listener.Close()
	}
	if n.udp != nil {
		n.udp.Close()
	}
	if n.tunnel != nil {
		n.tunnel.Close()
	}
	for _, server := range n.servers {
		server.Close()
	}
	for _, relay := range n.relays {
		relay.Close()
	}
	n.wg.Wait()
	n.clearBridge()
}
func (n *Network) Status() map[string]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := map[string]string{}
	for k, v := range n.relayStatus {
		out[k] = v
	}
	return out
}
func (n *Network) maintainRelay(relay *RelayClient) {
	delay := time.Second
	for n.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		inbox, err := relay.Inbox(ctx)
		cancel()
		n.mu.Lock()
		if err != nil {
			n.relayStatus[relay.URL] = "offline"
		} else {
			n.relayStatus[relay.URL] = "online"
		}
		n.mu.Unlock()
		if err == nil {
			delay = 2 * time.Second
			for _, offer := range inbox {
				if !validID(offer["id"]) || !validFingerprint(offer["from"]) || offer["from"] == n.service.identity.Device().Fingerprint {
					continue
				}
				if n.service.consent.Snapshot().Relationships[offer["from"]].Blocked {
					continue
				}
				ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
				conn, e := relay.AcceptTunnel(ctx, offer["id"])
				cancel()
				if e != nil {
					continue
				}
				select {
				case n.tunnel.connections <- conn:
				case <-n.ctx.Done():
					conn.Close()
					return
				default:
					conn.Close()
				}
			}
		} else {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-n.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (n *Network) refreshPending() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var offset, rounds int
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if !n.config.LAN || n.config.BridgeDir == "" {
				_ = n.refreshAddresses()
			}
			rounds++
			if rounds%15 == 0 {
				ctx, cancel := context.WithTimeout(n.ctx, 8*time.Second)
				n.service.retryRoster(ctx, rounds/15-1)
				cancel()
			}
			pending := []string{}
			consentState := n.service.consent.Snapshot()
			for id, inv := range consentState.Invitations {
				if (inv.Direction == "outgoing" && inv.Status == "pending" && time.Now().Before(inv.Expires)) || (inv.Status == "revoked" && !inv.RevocationAcknowledged && !consentState.Relationships[inv.Peer].Blocked) {
					pending = append(pending, id)
				}
			}
			sort.Strings(pending)
			if len(pending) == 0 {
				continue
			}
			offset %= len(pending)
			for i := 0; i < len(pending) && i < 8; i++ {
				ctx, cancel := context.WithTimeout(n.ctx, 3*time.Second)
				_, _ = n.service.RefreshInvitation(ctx, pending[(offset+i)%len(pending)])
				cancel()
				if n.ctx.Err() != nil {
					return
				}
			}
			offset += 8
		}
	}
}
func (n *Network) Discover(ctx context.Context, code string) ([]Card, error) {
	if !ValidCode(code) {
		return nil, errors.New("enter a six-character device code")
	}
	if n.config.LAN && n.config.BridgeDir != "" && len(n.relays) == 0 && !LANBridgeReady(n.config.BridgeDir) {
		return nil, errors.New("Docker 局域网桥未就绪，请用更新后的启动器启动；检查 .aide-data/message-lan/bridge.log")
	}
	result := []Card{}
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	appendCards := func(cards []Card) {
		mu.Lock()
		defer mu.Unlock()
		for _, card := range cards {
			peer := card.Device.Fingerprint
			if peer == n.service.identity.Device().Fingerprint || seen[peer] || card.Validate(time.Now()) != nil || card.Device.Code != code {
				continue
			}
			seen[peer] = true
			result = append(result, card)
		}
	}
	for _, relay := range n.relays {
		wg.Add(1)
		go func(relay *RelayClient) {
			defer wg.Done()
			cards, err := relay.Find(ctx, code)
			if err == nil {
				appendCards(cards)
			}
		}(relay)
	}
	if n.config.LAN {
		wg.Add(1)
		go func() { defer wg.Done(); cards, _ := n.discover(ctx, code); appendCards(cards) }()
	}
	wg.Wait()
	sort.Slice(result, func(i, j int) bool { return result[i].Device.Fingerprint < result[j].Device.Fingerprint })
	return result, nil
}

func (n *Network) discover(ctx context.Context, code string) ([]Card, error) {
	if n.config.BridgeDir != "" {
		return bridgeDiscover(ctx, n.config.BridgeDir, code)
	}
	return discoverLAN(ctx, code)
}

type discoveryPacket struct {
	Code  string `json:"code"`
	Nonce string `json:"nonce"`
	Card  *Card  `json:"card,omitempty"`
}

func (n *Network) serveDiscovery() {
	buffer := make([]byte, 512)
	last := map[string]time.Time{}
	for {
		size, address, err := n.udp.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var query discoveryPacket
		if json.Unmarshal(buffer[:size], &query) != nil || query.Card != nil || query.Code != n.service.identity.Device().Code || !validID(query.Nonce) {
			continue
		}
		now := time.Now()
		if now.Sub(last[address.IP.String()]) < 5*time.Second {
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
		last[address.IP.String()] = now
		card, err := n.service.Card()
		if err != nil {
			continue
		}
		query.Card = &card
		data, _ := json.Marshal(query)
		_, _ = n.udp.WriteToUDP(data, address)
	}
}
func discoverLAN(ctx context.Context, code string) ([]Card, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(1200 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	nonce := randomID()
	payload, _ := json.Marshal(discoveryPacket{Code: code, Nonce: nonce})
	if _, err = conn.WriteToUDP(payload, &net.UDPAddr{IP: net.ParseIP("239.255.76.33"), Port: 45839}); err != nil {
		return nil, err
	}
	buffer := make([]byte, 16<<10)
	cards := []Card{}
	for len(cards) < 16 {
		size, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			break
		}
		var response discoveryPacket
		if json.Unmarshal(buffer[:size], &response) == nil && response.Nonce == nonce && response.Code == code && response.Card != nil {
			cards = append(cards, *response.Card)
		}
	}
	return cards, nil
}

type channelListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case c := <-l.connections:
		return c, nil
	}
}
func (l *channelListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		for {
			select {
			case c := <-l.connections:
				c.Close()
			default:
				return
			}
		}
	})
	return nil
}
func (l *channelListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0} }

type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newBoundedListener(l net.Listener, count int) *boundedListener {
	return &boundedListener{Listener: l, slots: make(chan struct{}, count), done: make(chan struct{})}
}
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &countedConn{Conn: conn, release: func() { <-l.slots }}, nil
}
func (l *boundedListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }

// Keep parsing centralized for native-host and container configurations.
func ValidateListen(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host != "" && net.ParseIP(host) == nil {
		return errors.New("listen host must be an IP literal")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 0 || n > 65535 {
		return errors.New("invalid message port")
	}
	return nil
}
