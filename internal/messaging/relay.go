package messaging

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type relayAuth struct {
	Card      Card   `json:"card"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	Tunnel    string `json:"tunnel,omitempty"`
	Nonce     string `json:"nonce"`
	At        int64  `json:"at"`
	Signature []byte `json:"signature"`
}

func (a relayAuth) payload() []byte { a.Signature = nil; raw, _ := json.Marshal(a); return raw }

type relayTunnel struct {
	id, from, to string
	created      time.Time
	left         net.Conn
	right        net.Conn
}
type relayWindow struct {
	at    time.Time
	count int
}

// Relay is an optional self-hosted rendezvous and opaque byte-forwarding service.
// It never receives workbench access tokens or device private/storage keys.
// Operators must serve this handler over ordinary publicly trusted HTTPS (or
// explicitly configured private CA) with server connection/header timeouts.
type Relay struct {
	mu      sync.Mutex
	cards   map[string]Card
	nonces  map[string]time.Time
	windows map[string]relayWindow
	tunnels map[string]*relayTunnel
}

func NewRelay() *Relay {
	return &Relay{cards: map[string]Card{}, nonces: map[string]time.Time{}, windows: map[string]relayWindow{}, tunnels: map[string]*relayTunnel{}}
}
func (r *Relay) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, t := range r.tunnels {
		t.left.Close()
		if t.right != nil {
			t.right.Close()
		}
		delete(r.tunnels, id)
	}
}
func (r *Relay) authorize(w http.ResponseWriter, request *http.Request) (relayAuth, error) {
	var auth relayAuth
	if request.TLS == nil {
		return auth, errors.New("HTTPS required")
	}
	request.Body = http.MaxBytesReader(w, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&auth); err != nil {
		return auth, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return auth, errors.New("trailing request data")
	}
	now := time.Now()
	age := now.Unix() - auth.At
	if age < -30 || age > 30 || !validID(auth.Nonce) || auth.Card.Validate(now) != nil || !Verify(auth.Card.Device, auth.payload(), auth.Signature) {
		return auth, ErrDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for nonce, expiry := range r.nonces {
		if now.After(expiry) {
			delete(r.nonces, nonce)
		}
	}
	for id, card := range r.cards {
		if card.Expires < now.Unix() {
			delete(r.cards, id)
		}
	}
	for id, t := range r.tunnels {
		if t.right == nil && now.Sub(t.created) > 30*time.Second {
			t.left.Close()
			delete(r.tunnels, id)
		}
	}
	for key, window := range r.windows {
		if now.Sub(window.at) > time.Minute {
			delete(r.windows, key)
		}
	}
	if len(r.nonces) >= 262144 || len(r.windows) >= 8192 {
		return auth, errors.New("relay capacity reached")
	}
	if _, exists := r.cards[auth.Card.Device.Fingerprint]; !exists && len(r.cards) >= 4096 {
		return auth, errors.New("relay device capacity reached")
	}
	nonceKey := auth.Card.Device.Fingerprint + ":" + auth.Nonce
	if _, seen := r.nonces[nonceKey]; seen {
		return auth, errors.New("relay request replay")
	}
	host, _, _ := net.SplitHostPort(request.RemoteAddr)
	for key, limit := range map[string]int{"device:" + auth.Card.Device.Fingerprint: 120, "network:" + host: 10000} {
		window := r.windows[key]
		if now.Sub(window.at) > time.Minute {
			window = relayWindow{at: now}
		}
		if window.count >= limit {
			return auth, errors.New("relay rate exceeded")
		}
		window.count++
		r.windows[key] = window
	}
	r.nonces[nonceKey] = now.Add(time.Minute)
	r.cards[auth.Card.Device.Fingerprint] = auth.Card
	return auth, nil
}
func (r *Relay) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != "POST" || request.URL.Path != "/v1/relay" {
		http.NotFound(w, request)
		return
	}
	auth, err := r.authorize(w, request)
	if err != nil {
		http.Error(w, "relay request rejected", 403)
		return
	}
	own := auth.Card.Device.Fingerprint
	switch auth.Action {
	case "find":
		if !ValidCode(auth.Target) {
			http.Error(w, "invalid code", 400)
			return
		}
		cards := []Card{}
		r.mu.Lock()
		for _, card := range r.cards {
			if card.Device.Code == auth.Target && card.Device.Fingerprint != own {
				cards = append(cards, card)
				if len(cards) >= 16 {
					break
				}
			}
		}
		r.mu.Unlock()
		relayJSON(w, cards)
	case "inbox":
		inbox := []map[string]string{}
		r.mu.Lock()
		for _, t := range r.tunnels {
			if t.to == own && t.right == nil {
				inbox = append(inbox, map[string]string{"id": t.id, "from": t.from})
			}
		}
		r.mu.Unlock()
		relayJSON(w, inbox)
	case "advertise":
		relayJSON(w, map[string]string{"status": "registered"})
	case "open", "accept":
		r.tunnel(w, request, auth)
	default:
		http.Error(w, "unsupported relay operation", 400)
	}
}
func relayJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (r *Relay) tunnel(w http.ResponseWriter, request *http.Request, auth relayAuth) {
	own := auth.Card.Device.Fingerprint
	r.mu.Lock()
	var tunnel *relayTunnel
	if auth.Action == "open" {
		if !validFingerprint(auth.Target) || auth.Target == own || len(r.tunnels) >= 256 {
			r.mu.Unlock()
			http.Error(w, "tunnel unavailable", 409)
			return
		}
		outstanding := 0
		for _, t := range r.tunnels {
			if t.from == own || t.to == auth.Target {
				outstanding++
			}
		}
		if outstanding >= 8 {
			r.mu.Unlock()
			http.Error(w, "tunnel limit", 429)
			return
		}
		if _, ok := r.cards[auth.Target]; !ok {
			r.mu.Unlock()
			http.Error(w, "device unavailable", 404)
			return
		}
		tunnel = &relayTunnel{id: randomID(), from: own, to: auth.Target, created: time.Now()}
	} else {
		tunnel = r.tunnels[auth.Tunnel]
		if tunnel == nil || tunnel.to != own || tunnel.right != nil {
			r.mu.Unlock()
			http.Error(w, "tunnel unavailable", 404)
			return
		}
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		r.mu.Unlock()
		http.Error(w, "HTTP/1.1 tunnel required", 505)
		return
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		r.mu.Unlock()
		return
	}
	// Bound tunnel lifetime independently from HTTP request context; leaving a
	// handler must not cancel the encrypted device session it just established.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Hour))
	if _, err = fmt.Fprintf(buffer, "HTTP/1.1 200 Connection Established\r\nX-Aide-Tunnel: %s\r\n\r\n", tunnel.id); err == nil {
		err = buffer.Flush()
	}
	if err != nil {
		conn.Close()
		r.mu.Unlock()
		return
	}
	wrapped := &bufferedConn{Conn: conn, reader: buffer.Reader}
	if auth.Action == "open" {
		tunnel.left = wrapped
		r.tunnels[tunnel.id] = tunnel
		r.mu.Unlock()
		return
	}
	tunnel.right = wrapped
	r.mu.Unlock()
	go func() {
		defer func() {
			tunnel.left.Close()
			tunnel.right.Close()
			r.mu.Lock()
			delete(r.tunnels, tunnel.id)
			r.mu.Unlock()
		}()
		done := make(chan struct{}, 1)
		go func() { _, _ = io.Copy(tunnel.left, tunnel.right); done <- struct{}{} }()
		_, _ = io.Copy(tunnel.right, tunnel.left)
		tunnel.left.Close()
		tunnel.right.Close()
		<-done
	}()
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

type RelayClient struct {
	URL      string
	identity *Identity
	card     func() (Card, error)
	http     *http.Client
	tls      *tls.Config
}

// NewRelayClient takes only public relay URL and optional explicit CA trust.
// Inner peer TLS always runs above OpenTunnel and is independent of this trust.
func NewRelayClient(endpoint string, identity *Identity, card func() (Card, error), trust *tls.Config) (*RelayClient, error) {
	u, err := ParseAddress(endpoint)
	if err != nil {
		return nil, err
	}
	if trust == nil {
		trust = &tls.Config{MinVersion: tls.VersionTLS13}
	} else {
		trust = trust.Clone()
	}
	if trust.InsecureSkipVerify {
		return nil, errors.New("relay certificate verification cannot be disabled")
	}
	trust.MinVersion = tls.VersionTLS13
	trust.ServerName = u.Hostname()
	trust.NextProtos = []string{"http/1.1"}
	transport := &http.Transport{TLSClientConfig: trust, Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, MaxConnsPerHost: 4}
	return &RelayClient{URL: u.Scheme + "://" + u.Host, identity: identity, card: card, tls: trust, http: &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("relay redirects forbidden") }}}, nil
}
func (c *RelayClient) auth(action, target, tunnel string) ([]byte, error) {
	card, err := c.card()
	if err != nil {
		return nil, err
	}
	auth := relayAuth{Card: card, Action: action, Target: target, Tunnel: tunnel, Nonce: randomID(), At: time.Now().Unix()}
	auth.Signature = c.identity.Sign(auth.payload())
	return json.Marshal(auth)
}
func (c *RelayClient) request(ctx context.Context, action, target string, out any) error {
	payload, err := c.auth(action, target, "")
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/v1/relay", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("relay unavailable (%d)", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(out)
}
func (c *RelayClient) Advertise(ctx context.Context) error {
	var out map[string]string
	return c.request(ctx, "advertise", "", &out)
}
func (c *RelayClient) Find(ctx context.Context, code string) ([]Card, error) {
	cards := []Card{}
	if !ValidCode(code) {
		return cards, errors.New("invalid code")
	}
	err := c.request(ctx, "find", code, &cards)
	if err != nil {
		return nil, err
	}
	valid := []Card{}
	for _, card := range cards {
		if card.Device.Code == code && card.Validate(time.Now()) == nil {
			valid = append(valid, card)
		}
	}
	return valid, nil
}
func (c *RelayClient) Inbox(ctx context.Context) ([]map[string]string, error) {
	var out []map[string]string
	err := c.request(ctx, "inbox", "", &out)
	return out, err
}
func (c *RelayClient) OpenTunnel(ctx context.Context, peer string) (net.Conn, error) {
	return c.tunnel(ctx, "open", peer, "")
}
func (c *RelayClient) AcceptTunnel(ctx context.Context, id string) (net.Conn, error) {
	return c.tunnel(ctx, "accept", "", id)
}
func (c *RelayClient) tunnel(ctx context.Context, action, target, id string) (net.Conn, error) {
	payload, err := c.auth(action, target, id)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(c.URL)
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	conn, err := (&tls.Dialer{Config: c.tls, NetDialer: &net.Dialer{Timeout: 10 * time.Second}}).DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	request, _ := http.NewRequest("POST", c.URL+"/v1/relay", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	if err = request.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 16<<10)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		conn.Close()
		return nil, errors.New("relay tunnel rejected")
	}
	_ = conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, reader: reader}, nil
}
func (c *RelayClient) Close() { c.http.CloseIdleConnections() }
