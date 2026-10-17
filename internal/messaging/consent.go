package messaging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Kind string

const (
	Contact Kind = "contact"
	Group   Kind = "group"
	Voice   Kind = "voice"
	File    Kind = "file"
)

type Invitation struct {
	RevocationAcknowledged bool      `json:"revocationAcknowledged,omitempty"`
	ID                     string    `json:"id"`
	Peer                   string    `json:"peer"` // Authenticated full fingerprint; never a discovery code.
	Kind                   Kind      `json:"kind"`
	Resource               string    `json:"resource"`
	Direction              string    `json:"direction"`
	Status                 string    `json:"status"`
	Expires                time.Time `json:"expires"`
}
type Relationship struct {
	Blocked        bool `json:"blocked"`
	LocalAccepted  bool `json:"localAccepted"`
	RemoteAccepted bool `json:"remoteAccepted"`
}
type State struct {
	Version       int                     `json:"version"`
	Relationships map[string]Relationship `json:"relationships"`
	Invitations   map[string]Invitation   `json:"invitations"`
}

// Save must atomically persist an encrypted snapshot before a mutation succeeds.
// Transport adapters must supply peer from authenticated channel identity, never
// from an unverified message field. This engine is not a network authentication layer.
type Save func(State) error
type Consent struct {
	mu    sync.Mutex
	state State
	save  Save
	now   func() time.Time
}

var ErrDenied = errors.New("consent required or peer blocked")

func NewConsent(state State, save Save) (*Consent, error) {
	if state.Version != 0 && state.Version != 1 {
		return nil, errors.New("unsupported consent version")
	}
	if save == nil {
		return nil, errors.New("durable consent storage required")
	}
	state.Version = 1
	if state.Relationships == nil {
		state.Relationships = map[string]Relationship{}
	}
	if state.Invitations == nil {
		state.Invitations = map[string]Invitation{}
	}
	if len(state.Invitations) > 4096 || len(state.Relationships) > 2048 {
		return nil, errors.New("consent storage capacity exceeded")
	}
	for key, inv := range state.Invitations {
		if key != inv.ID || !validFingerprint(inv.Peer) || !validKind(inv.Kind) || (inv.Direction != "incoming" && inv.Direction != "outgoing") || !validStatus(inv.Status) || len(inv.Resource) > 200 {
			return nil, errors.New("invalid persisted invitation")
		}
	}
	for peer := range state.Relationships {
		if !validFingerprint(peer) {
			return nil, errors.New("invalid persisted relationship")
		}
	}
	return &Consent{state: clone(state), save: save, now: time.Now}, nil
}
func validFingerprint(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func validKind(k Kind) bool { return k == Contact || k == Group || k == Voice || k == File }
func validStatus(s string) bool {
	return s == "pending" || s == "accepted" || s == "declined" || s == "revoked"
}
func clone(s State) State {
	out := State{Version: 1, Relationships: map[string]Relationship{}, Invitations: map[string]Invitation{}}
	for k, v := range s.Relationships {
		out.Relationships[k] = v
	}
	for k, v := range s.Invitations {
		out.Invitations[k] = v
	}
	return out
}
func (c *Consent) Snapshot() State { c.mu.Lock(); defer c.mu.Unlock(); return clone(c.state) }
func (c *Consent) commit(next State) error {
	if err := c.save(clone(next)); err != nil {
		return err
	}
	c.state = next
	return nil
}
func (c *Consent) allowed(s State, peer string) bool {
	r := s.Relationships[peer]
	return !r.Blocked && r.LocalAccepted && r.RemoteAccepted
}
func (c *Consent) CanChat(peer string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return validFingerprint(peer) && c.allowed(c.state, peer)
}

// Request records a specific authenticated invitation. Outgoing requests must
// be created locally with random IDs; incoming IDs are validated and cannot
// overwrite or revive a previously decided invitation.
func (c *Consent) Request(peer string, kind Kind, resource, direction, id string) (Invitation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !validFingerprint(peer) || !validKind(kind) || len(resource) > 200 || resource == "" || (direction != "incoming" && direction != "outgoing") {
		return Invitation{}, errors.New("invalid invitation")
	}
	next := clone(c.state)
	r := next.Relationships[peer]
	if r.Blocked || (kind != Contact && !c.allowed(next, peer)) {
		return Invitation{}, ErrDenied
	}
	if len(next.Relationships) >= 2048 {
		if _, ok := next.Relationships[peer]; !ok {
			return Invitation{}, errors.New("contact capacity exceeded")
		}
	}
	if id == "" && direction == "outgoing" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			return Invitation{}, e
		}
		id = hex.EncodeToString(b)
	}
	b, e := hex.DecodeString(id)
	if e != nil || len(b) != 16 || hex.EncodeToString(b) != id {
		return Invitation{}, errors.New("invalid invitation ID")
	}
	if old, ok := next.Invitations[id]; ok {
		if old.Peer == peer && old.Kind == kind && old.Resource == resource && old.Direction == direction {
			return old, nil
		}
		return Invitation{}, errors.New("invitation ID conflict")
	}
	if len(next.Invitations) >= 4096 {
		return Invitation{}, errors.New("invitation capacity exceeded")
	}
	totalPending := 0
	for _, v := range next.Invitations {
		if v.Status == "pending" && c.now().Before(v.Expires) {
			totalPending++
		}
	}
	if totalPending >= 128 {
		return Invitation{}, errors.New("pending invitation inbox full")
	}
	pending := 0
	for _, v := range next.Invitations {
		if direction == "incoming" && kind == Contact && v.Peer == peer && v.Kind == Contact && v.Status == "declined" && c.now().Before(v.Expires) {
			return Invitation{}, ErrDenied
		}
		if v.Peer == peer && v.Status == "pending" && c.now().Before(v.Expires) {
			pending++
			if v.Kind == kind && v.Resource == resource {
				return Invitation{}, errors.New("invitation already pending")
			}
		}
	}
	if pending >= 8 {
		return Invitation{}, errors.New("too many pending invitations")
	}
	expiry := c.now().Add(24 * time.Hour)
	if kind == Voice {
		expiry = c.now().Add(60 * time.Second)
	}
	inv := Invitation{ID: id, Peer: peer, Kind: kind, Resource: resource, Direction: direction, Status: "pending", Expires: expiry}
	next.Invitations[id] = inv
	next.Relationships[peer] = r
	if err := c.commit(next); err != nil {
		return Invitation{}, err
	}
	return inv, nil
}

// Decide only acts on incoming requests. A remote party cannot call this
// operation on the recipient's behalf; authenticated remote replies use Reply.
func (c *Consent) Decide(id string, accept bool) error      { return c.decide(id, "", accept, false) }
func (c *Consent) Reply(peer, id string, accept bool) error { return c.decide(id, peer, accept, true) }
func (c *Consent) decide(id, peer string, accept, remote bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := clone(c.state)
	inv, ok := next.Invitations[id]
	if !ok {
		return errors.New("invitation not found")
	}
	expected := "incoming"
	if remote {
		expected = "outgoing"
	}
	if inv.Direction != expected || (remote && inv.Peer != peer) {
		return ErrDenied
	}
	if inv.Status != "pending" || !c.now().Before(inv.Expires) || next.Relationships[inv.Peer].Blocked {
		return ErrDenied
	}
	if inv.Kind != Contact && !c.allowed(next, inv.Peer) {
		return ErrDenied
	}
	inv.Status = "declined"
	if accept {
		inv.Status = "accepted"
	}
	next.Invitations[id] = inv
	if inv.Kind == Contact {
		r := next.Relationships[inv.Peer]
		// A locally initiated request expresses local consent; the signed reply is
		// the remote consent. An incoming request expresses the remote consent.
		r.LocalAccepted = accept
		r.RemoteAccepted = accept
		next.Relationships[inv.Peer] = r
		if !accept {
			invalidate(&next, inv.Peer)
		}
	}
	return c.commit(next)
}
func invalidate(s *State, peer string) {
	for id, v := range s.Invitations {
		if v.Peer == peer && (v.Status == "pending" || v.Status == "accepted") {
			v.Status = "revoked"
			s.Invitations[id] = v
		}
	}
}

// Block and Remove revoke all resource grants. Unblocking never re-adds a contact.
func (c *Consent) Block(peer string, blocked bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !validFingerprint(peer) {
		return ErrDenied
	}
	next := clone(c.state)
	r := next.Relationships[peer]
	r.Blocked = blocked
	r.LocalAccepted = false
	r.RemoteAccepted = false
	next.Relationships[peer] = r
	invalidate(&next, peer)
	return c.commit(next)
}
func (c *Consent) Remove(peer string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !validFingerprint(peer) {
		return ErrDenied
	}
	next := clone(c.state)
	r := next.Relationships[peer]
	r.LocalAccepted = false
	r.RemoteAccepted = false
	next.Relationships[peer] = r
	invalidate(&next, peer)
	return c.commit(next)
}
func (c *Consent) Revoke(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := clone(c.state)
	inv, ok := next.Invitations[id]
	if !ok {
		return fmt.Errorf("invitation not found")
	}
	inv.Status = "revoked"
	next.Invitations[id] = inv
	if inv.Kind == Contact {
		r := next.Relationships[inv.Peer]
		r.LocalAccepted = false
		r.RemoteAccepted = false
		next.Relationships[inv.Peer] = r
		invalidate(&next, inv.Peer)
	}
	return c.commit(next)
}

// Authorized is checked for every file chunk and every voice/signaling action,
// not merely when opening a panel. Expiry is the invitation's acceptance deadline;
// resource lifetime and group membership are enforced by the transport/session.
func (c *Consent) Authorized(peer, id string, kind Kind, resource string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	inv, ok := c.state.Invitations[id]
	return ok && c.allowed(c.state, peer) && inv.Peer == peer && inv.Kind == kind && inv.Resource == resource && inv.Status == "accepted"
}

// Acknowledgement is durable so an offline peer is retried without restoring
// the revoked grant. The transport must authenticate the matching peer first.
func (c *Consent) AcknowledgeRevocation(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	inv, ok := c.state.Invitations[id]
	if !ok || inv.Status != "revoked" {
		return ErrDenied
	}
	if inv.RevocationAcknowledged {
		return nil
	}
	next := clone(c.state)
	inv.RevocationAcknowledged = true
	next.Invitations[id] = inv
	return c.commit(next)
}
