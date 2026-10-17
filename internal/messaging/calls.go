package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type CallState struct {
	ID         string                `json:"id"`
	Group      string                `json:"group"`
	Owner      string                `json:"owner"`
	Invitation string                `json:"invitation,omitempty"`
	Members    map[string]Membership `json:"members"`
	Revision   uint64                `json:"revision"`
	Expires    time.Time             `json:"expires"`
	Ended      bool                  `json:"ended"`
}

func (s *Service) StartGroupCall(ctx context.Context, groupID string) (CallState, error) {
	s.mu.Lock()
	g, ok := s.state.Groups[groupID]
	s.mu.Unlock()
	own := s.identity.Device().Fingerprint
	if !ok || g.Owner != own || len(g.Members) < 2 {
		return CallState{}, errors.New("group owner with joined members required")
	}
	id := randomID()
	call := CallState{ID: id, Group: groupID, Owner: own, Revision: 1, Members: map[string]Membership{own: s.membership(id, own, id)}, Expires: time.Now().Add(2 * time.Hour)}
	s.mu.Lock()
	s.state.Calls[id] = call
	err := s.persistLocked()
	if err != nil {
		delete(s.state.Calls, id)
	}
	s.mu.Unlock()
	if err != nil {
		return CallState{}, err
	}
	for peer := range g.Members {
		if peer != own {
			_, _ = s.Request(ctx, peer, Voice, id, nil)
		}
	}
	return call, nil
}
func (s *Service) receiveCallInvite(peer string, wire Wire, inv Invitation) (*Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.state.Groups[wire.Text]
	if !ok || g.Owner != peer || !s.localGroupAllowed(g) {
		return nil, ErrDenied
	}
	if _, ok = g.Members[s.identity.Device().Fingerprint]; !ok {
		return nil, ErrDenied
	}
	call, exists := s.state.Calls[wire.Resource]
	if exists && (call.Owner != peer || call.Invitation != inv.ID || call.Group != g.ID) {
		return nil, ErrDenied
	}
	if !exists {
		call = CallState{ID: wire.Resource, Group: g.ID, Owner: peer, Invitation: inv.ID, Members: map[string]Membership{}, Expires: time.Now().Add(2 * time.Hour)}
		s.state.Calls[call.ID] = call
		if err := s.persistLocked(); err != nil {
			delete(s.state.Calls, call.ID)
			return nil, err
		}
	}
	if inv.Status != "accepted" {
		return nil, nil
	}
	proof := s.membership(call.ID, peer, inv.ID)
	return &proof, nil
}
func (s *Service) addCallMember(ctx context.Context, inv Invitation, proof *Membership) error {
	own := s.identity.Device().Fingerprint
	if proof == nil || !proof.valid() || proof.Owner != own || proof.GroupID != inv.Resource || proof.Invitation != inv.ID || proof.Device.Fingerprint != inv.Peer {
		return ErrDenied
	}
	s.mu.Lock()
	call, ok := s.state.Calls[inv.Resource]
	g := s.state.Groups[call.Group]
	if !ok || call.Owner != own || call.Ended || time.Now().After(call.Expires) {
		s.mu.Unlock()
		return ErrDenied
	}
	if _, ok = g.Members[inv.Peer]; !ok {
		s.mu.Unlock()
		return ErrDenied
	}
	if _, ok = call.Members[inv.Peer]; ok {
		s.mu.Unlock()
		return s.syncCall(ctx, call)
	}
	old := call
	call.Members = copyMembers(call.Members)
	call.Members[inv.Peer] = *proof
	call.Revision++
	s.state.Calls[call.ID] = call
	if err := s.persistLocked(); err != nil {
		s.state.Calls[call.ID] = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return s.syncCall(ctx, call)
}
func (s *Service) syncCall(ctx context.Context, call CallState) error {
	var failures []error
	for peer := range call.Members {
		if peer != call.Owner {
			if _, err := s.push(ctx, peer, Wire{Type: "call-sync", ID: randomID(), Call: &call}); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (s *Service) receiveCallSync(peer string, wire Wire) (WireReply, error) {
	if wire.Call == nil {
		return WireReply{}, ErrDenied
	}
	in := *wire.Call
	s.mu.Lock()
	defer s.mu.Unlock()
	local, ok := s.state.Calls[in.ID]
	g := s.state.Groups[in.Group]
	if !ok || local.Owner != peer || g.Owner != peer || in.Owner != peer || local.Group != in.Group || in.Revision < local.Revision || len(in.Members) > MaxGroupMembers || in.Expires.After(local.Expires.Add(time.Minute)) || !s.consent.Authorized(peer, local.Invitation, Voice, in.ID) {
		return WireReply{}, ErrDenied
	}
	if in.Revision == local.Revision {
		left, _ := json.Marshal(in.Members)
		right, _ := json.Marshal(local.Members)
		if string(left) != string(right) || in.Ended != local.Ended {
			return WireReply{}, ErrDenied
		}
		return WireReply{Status: "synced"}, nil
	}
	own := s.identity.Device().Fingerprint
	proof, ok := in.Members[own]
	if !ok || proof.Invitation != local.Invitation {
		return WireReply{}, ErrDenied
	}
	for id, m := range in.Members {
		if id != m.Device.Fingerprint || m.Owner != peer || m.GroupID != in.ID || !m.valid() {
			return WireReply{}, ErrDenied
		}
		if _, ok = g.Members[id]; !ok {
			return WireReply{}, ErrDenied
		}
	}
	in.Invitation = local.Invitation
	s.state.Calls[in.ID] = in
	if err := s.persistLocked(); err != nil {
		s.state.Calls[in.ID] = local
		return WireReply{}, err
	}
	return WireReply{Status: "synced"}, nil
}
func (s *Service) callSignalAllowed(peer, group, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	call, ok := s.state.Calls[id]
	g := s.state.Groups[group]
	if !ok || call.Group != group || call.Ended || time.Now().After(call.Expires) || !s.localGroupAllowed(g) {
		return false
	}
	own := s.identity.Device().Fingerprint
	if _, ok = call.Members[peer]; !ok {
		return false
	}
	if _, ok = call.Members[own]; !ok {
		return false
	}
	if call.Owner == own {
		member := call.Members[peer]
		if !s.VoicePermission(peer, member.Invitation, id) {
			return false
		}
	}
	if call.Owner != own && !s.consent.Authorized(call.Owner, call.Invitation, Voice, id) {
		return false
	}
	return true
}
