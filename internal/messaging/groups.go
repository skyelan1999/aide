package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const MaxGroupMembers = 8

// Membership is a device-signed acceptance of one specific group invitation.
// A group owner cannot enroll another device by merely listing its public key.
type Membership struct {
	GroupID    string `json:"groupId"`
	Owner      string `json:"owner"`
	Device     Device `json:"device"`
	Invitation string `json:"invitation"`
	Signature  []byte `json:"signature"`
}

func (m Membership) payload() []byte { m.Signature = nil; raw, _ := json.Marshal(m); return raw }
func (m Membership) valid() bool {
	return validID(m.GroupID) && validFingerprint(m.Owner) && validID(m.Invitation) && Verify(m.Device, m.payload(), m.Signature)
}

type GroupState struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Owner      string                `json:"owner"`
	Revision   uint64                `json:"revision"`
	Members    map[string]Membership `json:"members,omitempty"`
	Peers      map[string]Card       `json:"peers,omitempty"`
	Invitation string                `json:"invitation,omitempty"`
}

func (s *Service) membership(group, owner, invitation string) Membership {
	m := Membership{GroupID: group, Owner: owner, Device: s.identity.Device(), Invitation: invitation}
	m.Signature = s.identity.Sign(m.payload())
	return m
}
func (s *Service) CreateGroup(ctx context.Context, name string, peers []string) (GroupState, error) {
	if name == "" || len(name) > 100 || len(peers) < 1 || len(peers) >= MaxGroupMembers {
		return GroupState{}, errors.New("groups require a name and 1–7 invited contacts")
	}
	unique := map[string]bool{}
	for _, peer := range peers {
		if unique[peer] || !s.consent.CanChat(peer) {
			return GroupState{}, ErrDenied
		}
		unique[peer] = true
	}
	own := s.identity.Device().Fingerprint
	id := randomID()
	m := s.membership(id, own, id)
	group := GroupState{ID: id, Name: name, Owner: own, Revision: 1, Members: map[string]Membership{own: m}, Peers: map[string]Card{}}
	s.mu.Lock()
	s.state.Groups[id] = group
	err := s.persistLocked()
	if err != nil {
		delete(s.state.Groups, id)
	}
	s.mu.Unlock()
	if err != nil {
		return GroupState{}, err
	}
	for _, peer := range peers {
		_, _ = s.Request(ctx, peer, Group, id, nil)
	}
	return group, nil
}
func (s *Service) validateGroupInvite(peer string, wire Wire) error {
	g := wire.Group
	if g == nil || g.ID != wire.Resource || g.Owner != peer || !validID(g.ID) || g.Name == "" || len(g.Name) > 100 {
		return ErrDenied
	}
	return nil
}
func (s *Service) receiveGroupInvite(inv Invitation, in GroupState) (*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, exists := s.state.Groups[in.ID]
	if exists && (g.Owner != inv.Peer || g.Invitation != inv.ID) {
		return nil, ErrDenied
	}
	if !exists {
		g = GroupState{ID: in.ID, Name: in.Name, Owner: inv.Peer, Invitation: inv.ID, Members: map[string]Membership{}, Peers: map[string]Card{}}
		s.state.Groups[g.ID] = g
		if err := s.persistLocked(); err != nil {
			delete(s.state.Groups, g.ID)
			return nil, err
		}
	}
	if inv.Status != "accepted" {
		return nil, nil
	}
	proof := s.membership(g.ID, g.Owner, inv.ID)
	return &proof, nil
}
func (s *Service) addGroupMember(ctx context.Context, inv Invitation, proof *Membership) error {
	if proof == nil || !proof.valid() || proof.GroupID != inv.Resource || proof.Owner != s.identity.Device().Fingerprint || proof.Device.Fingerprint != inv.Peer || proof.Invitation != inv.ID {
		return ErrDenied
	}
	s.mu.Lock()
	g, ok := s.state.Groups[inv.Resource]
	if !ok || g.Owner != s.identity.Device().Fingerprint {
		s.mu.Unlock()
		return ErrDenied
	}
	if _, ok = g.Members[inv.Peer]; ok {
		s.mu.Unlock()
		return s.syncGroup(ctx, g)
	}
	if len(g.Members) >= MaxGroupMembers {
		s.mu.Unlock()
		return errors.New("group full")
	}
	old := g
	g.Members = copyMembers(g.Members)
	g.Members[inv.Peer] = *proof
	g.Revision++
	g.Peers = map[string]Card{}
	for peer := range g.Members {
		if peer != g.Owner {
			g.Peers[peer] = s.state.Peers[peer]
		}
	}
	ownCard, err := s.identity.Card(s.cert, s.name, s.addresses)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	g.Peers[g.Owner] = ownCard
	s.state.Groups[g.ID] = g
	if err = s.persistLocked(); err != nil {
		s.state.Groups[g.ID] = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return s.syncGroup(ctx, g)
}
func (s *Service) syncGroup(ctx context.Context, g GroupState) error {
	var failures []error
	for peer := range g.Members {
		if peer != g.Owner {
			if _, err := s.push(ctx, peer, Wire{Type: "group-sync", ID: randomID(), Group: &g}); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

// retryRoster repairs delivery lost after an acceptance was persisted. Each
// periodic attempt selects one bounded roster and retains the signed revision.
func (s *Service) retryRoster(ctx context.Context, index int) {
	own := s.identity.Device().Fingerprint
	s.mu.Lock()
	var groups []GroupState
	for _, g := range s.state.Groups {
		if g.Owner == own && len(g.Members) > 1 {
			groups = append(groups, g)
		}
	}
	var calls []CallState
	for _, call := range s.state.Calls {
		if call.Owner == own && !call.Ended && time.Now().Before(call.Expires) && len(call.Members) > 1 {
			calls = append(calls, call)
		}
	}
	s.mu.Unlock()
	sort.Slice(calls, func(i, j int) bool { return calls[i].ID < calls[j].ID })
	if len(calls) > 0 {
		_ = s.syncCall(ctx, calls[index%len(calls)])
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	if len(groups) > 0 {
		_ = s.syncGroup(ctx, groups[index%len(groups)])
	}
}

func copyMembers(m map[string]Membership) map[string]Membership {
	out := map[string]Membership{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func (s *Service) localGroupAllowed(g GroupState) bool {
	own := s.identity.Device().Fingerprint
	if _, ok := g.Members[own]; !ok {
		return false
	}
	if g.Owner == own {
		return true
	}
	return s.consent.Authorized(g.Owner, g.Invitation, Group, g.ID)
}
func (s *Service) receiveGroupSync(peer string, wire Wire) (WireReply, error) {
	if wire.Group == nil {
		return WireReply{}, ErrDenied
	}
	in := *wire.Group
	s.mu.Lock()
	defer s.mu.Unlock()
	local, ok := s.state.Groups[in.ID]
	if !ok || local.Owner != peer || in.Owner != peer || in.Revision < local.Revision || len(in.Members) > MaxGroupMembers || len(in.Members) < 2 || !s.consent.Authorized(peer, local.Invitation, Group, in.ID) {
		return WireReply{}, ErrDenied
	}
	if in.Revision == local.Revision {
		left, _ := json.Marshal(in.Members)
		right, _ := json.Marshal(local.Members)
		if string(left) != string(right) {
			return WireReply{}, ErrDenied
		}
		return WireReply{Status: "synced"}, nil
	}
	own := s.identity.Device().Fingerprint
	ownMember, ok := in.Members[own]
	if !ok || ownMember.Invitation != local.Invitation {
		return WireReply{}, ErrDenied
	}
	for id, m := range in.Members {
		if id != m.Device.Fingerprint || m.GroupID != in.ID || m.Owner != peer || !m.valid() {
			return WireReply{}, ErrDenied
		}
	}
	for id, card := range in.Peers {
		if _, ok := in.Members[id]; !ok || card.Device.Fingerprint != id || card.Validate(timeFromCard(card)) != nil {
			return WireReply{}, ErrDenied
		}
	}
	in.Invitation = local.Invitation
	s.state.Groups[in.ID] = in
	for id, card := range in.Peers {
		if id != own {
			s.state.Peers[id] = card
		}
	}
	if err := s.persistLocked(); err != nil {
		s.state.Groups[in.ID] = local
		return WireReply{}, err
	}
	return WireReply{Status: "synced"}, nil
}
func (s *Service) sendGroupText(ctx context.Context, id, text string) error {
	s.mu.Lock()
	g, ok := s.state.Groups[id]
	s.mu.Unlock()
	if !ok || !s.localGroupAllowed(g) {
		return ErrDenied
	}
	own := s.identity.Device().Fingerprint
	messageID := randomID()
	failed := false
	for peer := range g.Members {
		if peer != own {
			if s.consent.Snapshot().Relationships[peer].Blocked || (g.Owner == own && !s.consent.Authorized(peer, g.Members[peer].Invitation, Group, g.ID)) {
				failed = true
				continue
			}
			if _, err := s.push(ctx, peer, Wire{Type: "group-text", ID: messageID, Resource: id, Text: text}); err != nil {
				failed = true
			}
		}
	}
	if err := s.appendMessage(Message{ID: messageID, Peer: own, Group: id, Text: text, At: time.Now()}); err != nil {
		return err
	}
	if failed {
		return errors.New("some group members are offline; delivery incomplete")
	}
	return nil
}
func (s *Service) receiveGroupText(peer string, wire Wire) (WireReply, error) {
	if len(wire.Text) == 0 || len(wire.Text) > 16000 {
		return WireReply{}, ErrDenied
	}
	s.mu.Lock()
	g, ok := s.state.Groups[wire.Resource]
	s.mu.Unlock()
	if !ok || !s.localGroupAllowed(g) {
		return WireReply{}, ErrDenied
	}
	if _, ok = g.Members[peer]; !ok || (g.Owner == s.identity.Device().Fingerprint && !s.consent.Authorized(peer, g.Members[peer].Invitation, Group, g.ID)) {
		return WireReply{}, ErrDenied
	}
	return WireReply{Status: "received"}, s.appendMessage(Message{ID: wire.ID, Peer: peer, Group: g.ID, Text: wire.Text, At: time.Now(), Incoming: true})
}
func timeFromCard(card Card) time.Time { return time.Unix(card.Expires-1, 0) }

// Group voice invitations and call rosters are implemented separately from
// group membership: agreeing to join a group does not open a microphone.
func (s *Service) groupSignalAllowed(peer, group, call string) bool {
	return s.callSignalAllowed(peer, group, call)
}
