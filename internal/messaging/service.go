package messaging

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

const ChunkSize = 1 << 20
const MaxFileSize int64 = 1 << 30
const maxReserved int64 = 4 << 30

type Message struct {
	ID       string    `json:"id"`
	Peer     string    `json:"peer"`
	Group    string    `json:"group,omitempty"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
	Incoming bool      `json:"incoming"`
}
type Manifest struct {
	Name   string   `json:"name"`
	Size   int64    `json:"size"`
	Hashes []string `json:"hashes"`
}
type Transfer struct {
	CleanupError string       `json:"cleanupError,omitempty"`
	ID           string       `json:"id"`
	Peer         string       `json:"peer"`
	Incoming     bool         `json:"incoming"`
	Manifest     Manifest     `json:"manifest"`
	Received     map[int]bool `json:"received"`
	Cancelled    bool         `json:"cancelled"`
	Created      time.Time    `json:"created"`
}
type Signal struct {
	ID         string          `json:"id"`
	Peer       string          `json:"peer"`
	Call       string          `json:"call"`
	Invitation string          `json:"invitation"`
	Group      string          `json:"group,omitempty"`
	Data       json.RawMessage `json:"data"`
	At         time.Time       `json:"at"`
}
type Wire struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Source   Card            `json:"source"`
	Kind     Kind            `json:"kind,omitempty"`
	Resource string          `json:"resource,omitempty"`
	Text     string          `json:"text,omitempty"`
	Manifest *Manifest       `json:"manifest,omitempty"`
	Signal   json.RawMessage `json:"signal,omitempty"`
	Group    *GroupState     `json:"group,omitempty"`
	Call     *CallState      `json:"call,omitempty"`
}
type WireReply struct {
	Status     string      `json:"status"`
	Received   []int       `json:"received,omitempty"`
	Membership *Membership `json:"membership,omitempty"`
}
type serviceState struct {
	Version   int                   `json:"version"`
	Peers     map[string]Card       `json:"peers"`
	Messages  []Message             `json:"messages"`
	Transfers map[string]Transfer   `json:"transfers"`
	Groups    map[string]GroupState `json:"groups"`
	Calls     map[string]CallState  `json:"calls"`
	Read      map[string]int64      `json:"read"`
}
type Service struct {
	mu           sync.Mutex
	identity     *Identity
	cert         tls.Certificate
	store        *Store
	consent      *Consent
	state        serviceState
	name         string
	addresses    []string
	signals      []Signal
	clients      map[string]*http.Client
	clientRoutes map[string]string
	dial         DialPeer
}

// DialPeer optionally supplies an opaque tunnel to a configured relay. The TLS
// handshake above it always terminates at and authenticates the selected device.
type DialPeer func(context.Context, string, Card) (net.Conn, error)

func NewService(identity *Identity, store *Store, name string, addresses []string, dial DialPeer) (*Service, error) {
	cert, err := identity.Certificate()
	if err != nil {
		return nil, err
	}
	if _, err = identity.Card(cert, name, addresses); err != nil {
		return nil, err
	}
	s := &Service{identity: identity, cert: cert, store: store, name: name, addresses: addresses, clients: map[string]*http.Client{}, clientRoutes: map[string]string{}, dial: dial, state: serviceState{Version: 1, Peers: map[string]Card{}, Messages: []Message{}, Transfers: map[string]Transfer{}, Groups: map[string]GroupState{}, Calls: map[string]CallState{}}}
	raw, err := store.Get("state", 4<<20)
	if err == nil {
		if err = json.Unmarshal(raw, &s.state); err != nil {
			return nil, err
		}
		if s.state.Version != 1 || s.state.Peers == nil || s.state.Transfers == nil || s.state.Groups == nil {
			return nil, errors.New("invalid message state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var state State
	raw, err = store.Get("consent", 4<<20)
	if err == nil {
		if err = json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.consent, err = NewConsent(state, func(state State) error {
		raw, e := json.Marshal(state)
		if e != nil {
			return e
		}
		return store.Put("consent", raw)
	})
	if err != nil {
		return nil, err
	}
	if s.state.Calls == nil {
		s.state.Calls = map[string]CallState{}
	}
	return s, nil
}
func (s *Service) Certificate() tls.Certificate { return s.cert }
func (s *Service) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}
func (s *Service) Card() (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identity.Card(s.cert, s.name, append([]string(nil), s.addresses...))
}
func (s *Service) SetAddresses(addresses []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.identity.Card(s.cert, s.name, addresses); err != nil {
		return err
	}
	s.addresses = append([]string(nil), addresses...)
	return nil
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.CloseIdleConnections()
	}
	s.clients = map[string]*http.Client{}
}
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func validID(id string) bool {
	b, e := hex.DecodeString(id)
	return e == nil && len(b) == 16 && hex.EncodeToString(b) == id
}
func (s *Service) persistLocked() error {
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	return s.store.Put("state", raw)
}
func (s *Service) Remember(card Card) error {
	if err := card.Validate(time.Now()); err != nil {
		return err
	}
	if card.Device.Fingerprint == s.identity.Device().Fingerprint {
		return errors.New("cannot add this device to itself")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := card.Device.Fingerprint
	if len(s.state.Peers) >= 2048 {
		if _, ok := s.state.Peers[id]; !ok {
			return errors.New("device cache full")
		}
	}
	old, existed := s.state.Peers[id]
	s.state.Peers[id] = card
	if err := s.persistLocked(); err != nil {
		if existed {
			s.state.Peers[id] = old
		} else {
			delete(s.state.Peers, id)
		}
		return err
	}
	return nil
}
func (s *Service) peer(peer string) (Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	card, ok := s.state.Peers[peer]
	if !ok {
		return Card{}, errors.New("device not discovered")
	}
	return card, nil
}
func (s *Service) client(peer string, card Card) (*http.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, _ := json.Marshal(struct {
		Certificate []byte
		Addresses   []string
	}{card.Certificate, card.Addresses})
	key := string(route)
	if client := s.clients[peer]; client != nil && s.clientRoutes[peer] == key {
		return client, nil
	}
	if old := s.clients[peer]; old != nil {
		old.CloseIdleConnections()
	}
	client, err := s.peerHTTPClient(peer, card)
	if err != nil {
		return nil, err
	}
	s.clients[peer] = client
	s.clientRoutes[peer] = key
	return client, nil
}
func (s *Service) push(ctx context.Context, peer string, wire Wire) (WireReply, error) {
	card, err := s.peer(peer)
	if err != nil {
		return WireReply{}, err
	}
	source, err := s.Card()
	if err != nil {
		return WireReply{}, err
	}
	wire.Source = source
	body, err := json.Marshal(wire)
	if err != nil {
		return WireReply{}, err
	}
	client, err := s.client(peer, card)
	if err != nil {
		return WireReply{}, err
	}
	endpoint := "https://" + peerName
	if len(card.Addresses) > 0 {
		endpoint = card.Addresses[0]
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/action", bytes.NewReader(body))
	if err != nil {
		return WireReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return WireReply{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return WireReply{}, fmt.Errorf("peer rejected request (%d)", res.StatusCode)
	}
	var out WireReply
	err = json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&out)
	return out, err
}
func validateManifest(m Manifest) error {
	if m.Name == "" || len(m.Name) > 255 || m.Size < 0 || m.Size > MaxFileSize || len(m.Hashes) != int((m.Size+ChunkSize-1)/ChunkSize) {
		return errors.New("invalid file manifest")
	}
	for _, h := range m.Hashes {
		if !validFingerprint(h) {
			return errors.New("invalid chunk digest")
		}
	}
	return nil
}
func (s *Service) Request(ctx context.Context, peer string, kind Kind, resource string, manifest *Manifest) (Invitation, error) {
	if _, err := s.peer(peer); err != nil {
		return Invitation{}, err
	}
	if kind == File {
		if manifest == nil {
			return Invitation{}, errors.New("file manifest required")
		}
		if err := validateManifest(*manifest); err != nil {
			return Invitation{}, err
		}
		s.mu.Lock()
		full := len(s.state.Transfers) >= 128
		s.mu.Unlock()
		if full {
			return Invitation{}, errors.New("file transfer capacity reached")
		}
	}
	if kind == Group {
		s.mu.Lock()
		g, exists := s.state.Groups[resource]
		s.mu.Unlock()
		if !exists || g.Owner != s.identity.Device().Fingerprint {
			return Invitation{}, ErrDenied
		}
	}
	if kind == Contact {
		resource = "contact"
	}
	if resource == "" {
		resource = randomID()
	}
	inv, err := s.consent.Request(peer, kind, resource, "outgoing", "")
	if err != nil {
		return inv, err
	}
	if kind == File {
		if manifest == nil {
			_ = s.consent.Revoke(inv.ID)
			return inv, errors.New("file manifest required")
		}
		if err = validateManifest(*manifest); err != nil {
			_ = s.consent.Revoke(inv.ID)
			return inv, err
		}
		s.mu.Lock()
		s.state.Transfers[inv.ID] = Transfer{ID: inv.ID, Peer: peer, Manifest: *manifest, Received: map[int]bool{}, Created: time.Now()}
		err = s.persistLocked()
		if err != nil {
			delete(s.state.Transfers, inv.ID)
		}
		s.mu.Unlock()
		if err != nil {
			_ = s.consent.Revoke(inv.ID)
			return inv, err
		}
	}
	// Persist pending consent even when offline. Refresh retries this exact ID;
	// a retry never creates a second grant or sends any file content.
	_, err = s.RefreshInvitation(ctx, inv.ID)
	return inv, err
}
func (s *Service) RefreshInvitation(ctx context.Context, id string) (Invitation, error) {
	snapshot := s.consent.Snapshot()
	inv, ok := snapshot.Invitations[id]
	if ok && inv.Status == "revoked" {
		reply, err := s.push(ctx, inv.Peer, Wire{Type: "revoke", ID: id})
		if err == nil && reply.Status == "revoked" {
			err = s.consent.AcknowledgeRevocation(id)
		}
		return inv, err
	}
	if !ok || inv.Direction != "outgoing" {
		return inv, ErrDenied
	}
	wire := Wire{Type: "request", ID: inv.ID, Kind: inv.Kind, Resource: inv.Resource}
	if inv.Kind == File {
		s.mu.Lock()
		f := s.state.Transfers[id]
		s.mu.Unlock()
		wire.Manifest = &f.Manifest
	}
	if inv.Kind == Group {
		s.mu.Lock()
		g := s.state.Groups[inv.Resource]
		s.mu.Unlock()
		wire.Group = &g
	}
	if inv.Kind == Voice {
		s.mu.Lock()
		call := s.state.Calls[inv.Resource]
		s.mu.Unlock()
		wire.Text = call.Group
	}
	reply, err := s.push(ctx, inv.Peer, wire)
	if err != nil {
		return inv, err
	}
	if inv.Status == "pending" && (reply.Status == "accepted" || reply.Status == "declined") {
		if err = s.consent.Reply(inv.Peer, id, reply.Status == "accepted"); err != nil {
			return inv, err
		}
	}
	if reply.Status == "revoked" {
		if err = s.consent.Revoke(id); err != nil {
			return inv, err
		}
	}
	inv = s.consent.Snapshot().Invitations[id]
	if inv.Kind == File {
		s.mu.Lock()
		f := s.state.Transfers[id]
		for _, n := range reply.Received {
			if n >= 0 && n < len(f.Manifest.Hashes) {
				f.Received[n] = true
			}
		}
		s.state.Transfers[id] = f
		err = s.persistLocked()
		s.mu.Unlock()
	}
	if inv.Kind == Group && inv.Status == "accepted" && reply.Status == "accepted" {
		err = s.addGroupMember(ctx, inv, reply.Membership)
	}
	if inv.Kind == Voice && inv.Status == "accepted" && wire.Text != "" && reply.Status == "accepted" {
		err = s.addCallMember(ctx, inv, reply.Membership)
	}
	return inv, err
}
func (s *Service) Decide(id string, accept bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.consent.Snapshot().Invitations[id]
	if !ok {
		return ErrDenied
	}
	if inv.Kind == File && accept {
		f, exists := s.state.Transfers[id]
		if !exists || f.Cancelled {
			return ErrDenied
		}
		reserved := int64(0)
		state := s.consent.Snapshot()
		for key, item := range s.state.Transfers {
			if item.Incoming && ((!item.Cancelled && state.Invitations[key].Status == "accepted") || len(item.Received) > 0) {
				reserved += item.Manifest.Size
			}
		}
		if reserved+f.Manifest.Size > maxReserved {
			return errors.New("receive quota exceeded")
		}
	}
	if err := s.consent.Decide(id, accept); err != nil {
		return err
	}
	if inv.Kind == File && !accept {
		err := s.cleanupTransferLocked(id)
		if persist := s.persistLocked(); persist != nil {
			return persist
		}
		return err
	}
	return nil
}
func (s *Service) Block(peer string, block bool) error {
	before := s.consent.Snapshot()
	if err := s.consent.Block(peer, block); err != nil {
		return err
	}
	s.mu.Lock()
	var cleanup error
	for id, f := range s.state.Transfers {
		if f.Peer == peer {
			cleanup = errors.Join(cleanup, s.cleanupTransferLocked(id))
		}
	}
	err := s.persistLocked()
	s.mu.Unlock()
	if block {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, inv := range before.Invitations {
			if inv.Peer == peer && (inv.Kind == Voice || inv.Kind == Group) {
				s.afterRevoke(ctx, inv)
			}
		}
	}
	return errors.Join(err, cleanup)
}
func (s *Service) SendText(ctx context.Context, peer, group, text string) error {
	if len(text) == 0 || len(text) > 16000 {
		return errors.New("message length invalid")
	}
	if group != "" {
		return s.sendGroupText(ctx, group, text)
	}
	if !s.consent.CanChat(peer) {
		return ErrDenied
	}
	id := randomID()
	_, err := s.push(ctx, peer, Wire{Type: "text", ID: id, Text: text})
	if err != nil {
		return err
	}
	return s.appendMessage(Message{ID: id, Peer: peer, Text: text, At: time.Now()})
}
func (s *Service) appendMessage(m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Incoming && (s.consent.Snapshot().Relationships[m.Peer].Blocked || (m.Group == "" && !s.consent.CanChat(m.Peer))) {
		return ErrDenied
	}
	for _, old := range s.state.Messages {
		if old.ID == m.ID && old.Peer == m.Peer {
			return nil
		}
	}
	old := s.state.Messages
	s.state.Messages = append(append([]Message(nil), old...), m)
	if len(s.state.Messages) > 10000 {
		s.state.Messages = s.state.Messages[len(s.state.Messages)-10000:]
	}
	total := 0
	for n := len(s.state.Messages) - 1; n >= 0; n-- {
		total += len(s.state.Messages[n].Text) + 256
		if total > 2<<20 {
			s.state.Messages = s.state.Messages[n+1:]
			break
		}
	}
	if err := s.persistLocked(); err != nil {
		s.state.Messages = old
		return err
	}
	return nil
}
func (s *Service) SendSignal(ctx context.Context, peer, invitation, call, group string, data json.RawMessage) error {
	if len(data) > 96000 || !json.Valid(data) {
		return errors.New("invalid voice signal")
	}
	if !s.signalAllowed(peer, invitation, call, group) {
		return ErrDenied
	}
	_, err := s.push(ctx, peer, Wire{Type: "signal", ID: invitation, Resource: call, Text: group, Signal: data})
	return err
}
func (s *Service) signalAllowed(peer, invitation, call, group string) bool {
	if s.consent.Snapshot().Relationships[peer].Blocked {
		return false
	}
	if group != "" {
		return s.groupSignalAllowed(peer, group, call)
	}
	return s.VoicePermission(peer, invitation, call)
}
func (s *Service) VoiceSessionAllowed(peer, invitation, call, group string) bool {
	return s.signalAllowed(peer, invitation, call, group)
}
func (s *Service) Snapshot() map[string]any {
	consent := s.consent.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var state serviceState
	_ = json.Unmarshal(raw, &state)
	peers := map[string]any{}
	for id, card := range state.Peers {
		peers[id] = map[string]any{"device": card.Device, "name": card.Name}
	}
	if len(state.Messages) > 500 {
		state.Messages = state.Messages[len(state.Messages)-500:]
	}
	return map[string]any{"device": s.identity.Device(), "name": s.name, "consent": consent, "peers": peers, "messages": state.Messages, "transfers": state.Transfers, "groups": state.Groups, "calls": state.Calls, "read": state.Read, "signals": append([]Signal(nil), s.signals...)}
}
func (s *Service) receive(w http.ResponseWriter, r *http.Request) {
	peer, err := AuthenticatedPeer(r)
	if err != nil {
		http.Error(w, "device authentication required", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var wire Wire
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&wire); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if decoder.Decode(new(any)) != io.EOF || !validID(wire.ID) || wire.Source.Device.Fingerprint != peer.Fingerprint {
		http.Error(w, "invalid request identity", 400)
		return
	}
	relationship := s.consent.Snapshot().Relationships[peer.Fingerprint]
	if relationship.Blocked {
		http.Error(w, "unavailable", 403)
		return
	}
	if !s.knownPeer(peer.Fingerprint) && (wire.Type != "request" || wire.Kind != Contact) {
		http.Error(w, "contact required", 403)
		return
	}
	if err = wire.Source.Validate(time.Now()); err != nil {
		http.Error(w, "invalid device card", 400)
		return
	}
	if err = s.Remember(wire.Source); err != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	reply, err := s.receiveWire(peer.Fingerprint, wire)
	if err != nil {
		http.Error(w, "request not authorized or invalid", 403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}
func (s *Service) receiveWire(peer string, wire Wire) (WireReply, error) {
	switch wire.Type {
	case "request":
		if wire.Kind == Contact && wire.Resource != "contact" {
			return WireReply{}, ErrDenied
		}
		if wire.Kind == File {
			if wire.Manifest == nil || validateManifest(*wire.Manifest) != nil {
				return WireReply{}, errors.New("invalid file offer")
			}
		}
		if wire.Kind == Group {
			if err := s.validateGroupInvite(peer, wire); err != nil {
				return WireReply{}, err
			}
		}
		inv, err := s.consent.Request(peer, wire.Kind, wire.Resource, "incoming", wire.ID)
		if err != nil {
			return WireReply{}, err
		}
		reply := WireReply{Status: inv.Status}
		if wire.Kind == File {
			if err = s.receiveFileOffer(inv, *wire.Manifest); err != nil {
				return WireReply{}, err
			}
			s.mu.Lock()
			f := s.state.Transfers[inv.ID]
			for n, ok := range f.Received {
				if ok {
					reply.Received = append(reply.Received, n)
				}
			}
			s.mu.Unlock()
			sort.Ints(reply.Received)
		}
		if wire.Kind == Group {
			reply.Membership, err = s.receiveGroupInvite(inv, *wire.Group)
		}
		if wire.Kind == Voice && wire.Text != "" {
			reply.Membership, err = s.receiveCallInvite(peer, wire, inv)
		}
		return reply, err
	case "text":
		if len(wire.Text) == 0 || len(wire.Text) > 16000 || !s.consent.CanChat(peer) {
			return WireReply{}, ErrDenied
		}
		return WireReply{Status: "received"}, s.appendMessage(Message{ID: wire.ID, Peer: peer, Text: wire.Text, At: time.Now(), Incoming: true})
	case "signal":
		if !s.signalAllowed(peer, wire.ID, wire.Resource, wire.Text) || len(wire.Signal) > 96000 || !json.Valid(wire.Signal) {
			return WireReply{}, ErrDenied
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.signals = append(s.signals, Signal{ID: randomID(), Peer: peer, Call: wire.Resource, Invitation: wire.ID, Group: wire.Text, Data: append(json.RawMessage(nil), wire.Signal...), At: time.Now()})
		if len(s.signals) > 256 {
			s.signals = s.signals[len(s.signals)-256:]
		}
		return WireReply{Status: "received"}, nil
	case "call-sync":
		return s.receiveCallSync(peer, wire)
	case "group-sync":
		return s.receiveGroupSync(peer, wire)
	case "group-text":
		return s.receiveGroupText(peer, wire)
	case "revoke":
		inv, ok := s.consent.Snapshot().Invitations[wire.ID]
		if !ok || inv.Peer != peer {
			return WireReply{}, ErrDenied
		}
		if err := s.consent.Revoke(inv.ID); err != nil {
			return WireReply{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.afterRevoke(ctx, inv)
		if inv.Kind == File {
			s.mu.Lock()
			cleanup := s.cleanupTransferLocked(inv.ID)
			persist := s.persistLocked()
			s.mu.Unlock()
			if cleanup != nil {
				return WireReply{}, cleanup
			}
			if persist != nil {
				return WireReply{}, persist
			}
		}
		return WireReply{Status: "revoked"}, s.consent.AcknowledgeRevocation(inv.ID)
	default:
		return WireReply{}, errors.New("unsupported action")
	}
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/action", s.receive)
	mux.HandleFunc("PUT /v1/files/{id}/chunks/{index}", s.receiveChunk)
	return mux
}
func (s *Service) receiveFileOffer(inv Invitation, m Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.state.Transfers[inv.ID]; ok {
		if old.Peer == inv.Peer && old.Cancelled {
			return nil
		}
		a, _ := json.Marshal(old.Manifest)
		b, _ := json.Marshal(m)
		if old.Peer != inv.Peer || !bytes.Equal(a, b) {
			return errors.New("file offer changed")
		}
		return nil
	}
	if len(s.state.Transfers) >= 128 {
		return errors.New("file offer capacity exceeded")
	}

	s.state.Transfers[inv.ID] = Transfer{ID: inv.ID, Peer: inv.Peer, Incoming: true, Manifest: m, Received: map[int]bool{}, Created: time.Now()}
	if err := s.persistLocked(); err != nil {
		delete(s.state.Transfers, inv.ID)
		return err
	}
	return nil
}
func (s *Service) receiveChunk(w http.ResponseWriter, r *http.Request) {
	peer, err := AuthenticatedPeer(r)
	if err != nil {
		http.Error(w, "unauthorized", 403)
		return
	}
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || !validID(id) {
		http.Error(w, "invalid chunk", 400)
		return
	}
	inv, exists := s.consent.Snapshot().Invitations[id]
	if !exists || !s.consent.Authorized(peer.Fingerprint, id, File, inv.Resource) {
		w.Header().Set("Connection", "close")
		http.Error(w, "consent required", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, ChunkSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid chunk", 400)
		return
	}
	if err = s.PutReceivedChunk(peer.Fingerprint, id, index, data); err != nil {
		http.Error(w, "chunk rejected", 403)
		return
	}
	w.WriteHeader(204)
}
func (s *Service) PutReceivedChunk(peer, id string, index int, data []byte) error {
	inv, ok := s.consent.Snapshot().Invitations[id]
	if !ok || !s.consent.Authorized(peer, id, File, inv.Resource) {
		return ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.state.Transfers[id]
	if !s.consent.Authorized(peer, id, File, inv.Resource) {
		return ErrDenied
	}
	if !ok || !f.Incoming || f.Peer != peer || f.Cancelled || index < 0 || index >= len(f.Manifest.Hashes) {
		return ErrDenied
	}
	expected := ChunkSize
	if index == len(f.Manifest.Hashes)-1 {
		expected = int(f.Manifest.Size - int64(index)*ChunkSize)
	}
	hash := sha256.Sum256(data)
	if len(data) != expected || hex.EncodeToString(hash[:]) != f.Manifest.Hashes[index] {
		return errors.New("chunk integrity mismatch")
	}
	if err := s.store.Put(fmt.Sprintf("chunk-%s-%d", id, index), data); err != nil {
		return err
	}
	old := f.Received[index]
	f.Received[index] = true
	s.state.Transfers[id] = f
	if err := s.persistLocked(); err != nil {
		f.Received[index] = old
		s.state.Transfers[id] = f
		return err
	}
	return nil
}
