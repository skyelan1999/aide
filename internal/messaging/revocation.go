package messaging

import (
	"context"
	"time"
)

func (s *Service) afterRevoke(ctx context.Context, inv Invitation) {
	if inv.Kind == Contact {
		for _, child := range s.consent.Snapshot().Invitations {
			if child.Peer == inv.Peer && (child.Kind == Group || child.Kind == Voice) && child.Status == "revoked" {
				s.afterRevoke(ctx, child)
			}
		}
		return
	}
	if inv.Kind == Group {
		s.revokeGroupMembership(ctx, inv)
		return
	}
	if inv.Kind != Voice {
		return
	}
	s.mu.Lock()
	call, ok := s.state.Calls[inv.Resource]
	own := s.identity.Device().Fingerprint
	if !ok {
		s.mu.Unlock()
		return
	}
	if call.Owner == own {
		if member, exists := call.Members[inv.Peer]; exists && member.Invitation == inv.ID {
			call.Members = copyMembers(call.Members)
			delete(call.Members, inv.Peer)
			call.Revision++
		}
	} else if call.Invitation == inv.ID {
		call.Ended = true
	}
	s.state.Calls[call.ID] = call
	_ = s.persistLocked()
	s.mu.Unlock()
	if call.Owner == own {
		for peer := range call.Members {
			if peer != own {
				_, _ = s.push(ctx, peer, Wire{Type: "call-sync", ID: randomID(), Call: &call})
			}
		}
	}
}
func (s *Service) MarkRead(peer, group string) error {
	key := peer
	if group != "" {
		key = "group:" + group
	}
	if (group == "" && !validFingerprint(peer)) || (group != "" && !validID(group)) {
		return ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Read == nil {
		s.state.Read = map[string]int64{}
	}
	latest := int64(0)
	for _, message := range s.state.Messages {
		if message.Incoming && ((group != "" && message.Group == group) || (group == "" && message.Group == "" && message.Peer == peer)) && message.At.UnixNano() > latest {
			latest = message.At.UnixNano()
		}
	}
	old := s.state.Read[key]
	if latest <= old {
		return nil
	}
	s.state.Read[key] = latest
	if err := s.persistLocked(); err != nil {
		s.state.Read[key] = old
		return err
	}
	return nil
}
func (s *Service) VoicePermission(peer, id, call string) bool {
	inv, ok := s.consent.Snapshot().Invitations[id]
	return ok && time.Now().Before(inv.Expires.Add(2*time.Hour)) && s.consent.Authorized(peer, id, Voice, call)
}

func (s *Service) revokeGroupMembership(ctx context.Context, inv Invitation) {
	s.mu.Lock()
	g, ok := s.state.Groups[inv.Resource]
	own := s.identity.Device().Fingerprint
	if !ok {
		s.mu.Unlock()
		return
	}
	if g.Owner == own {
		if member, exists := g.Members[inv.Peer]; exists && member.Invitation == inv.ID {
			g.Members = copyMembers(g.Members)
			delete(g.Members, inv.Peer)
			g.Revision++
			peers := map[string]Card{}
			for peer, card := range g.Peers {
				if peer != inv.Peer {
					peers[peer] = card
				}
			}
			g.Peers = peers
			s.state.Groups[g.ID] = g
		}
	}
	for id, call := range s.state.Calls {
		if call.Group == g.ID && !call.Ended {
			if call.Owner != own {
				call.Ended = true
			} else {
				call.Members = copyMembers(call.Members)
				delete(call.Members, inv.Peer)
				call.Revision++
			}
			s.state.Calls[id] = call
		}
	}
	_ = s.persistLocked()
	s.mu.Unlock()
	if g.Owner == own {
		_ = s.syncGroup(ctx, g)
	}
}
