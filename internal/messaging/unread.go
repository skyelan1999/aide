package messaging

import "strings"

func conversationKey(m Message) string {
	if m.Group != "" {
		return "group:" + m.Group
	}
	return m.Peer
}

func validConversationKey(key string) bool {
	if strings.HasPrefix(key, "group:") {
		return validID(strings.TrimPrefix(key, "group:"))
	}
	return validFingerprint(key)
}

// Migration retains legacy read state. Arrival sequences, unlike remote wall
// clocks, remain ordered when peers have clock skew or identical timestamps.
func (s *Service) migrateReadSequences() {
	legacy := s.state.ReadSequence == nil
	if legacy {
		s.state.ReadSequence = map[string]uint64{}
	}
	for i := range s.state.Messages {
		m := &s.state.Messages[i]
		if m.Sequence == 0 {
			s.state.NextSequence++
			m.Sequence = s.state.NextSequence
		}
		if m.Sequence > s.state.NextSequence {
			s.state.NextSequence = m.Sequence
		}
		key := conversationKey(*m)
		if legacy && m.Incoming && m.At.UnixNano() <= s.state.Read[key] && m.Sequence > s.state.ReadSequence[key] {
			s.state.ReadSequence[key] = m.Sequence
		}
	}
}

func (s *Service) unreadLocked(consent State) (map[string]int, map[string]string) {
	counts := map[string]int{}
	through := map[string]string{}
	own := s.identity.Device().Fingerprint
	for _, m := range s.state.Messages {
		if !m.Incoming || consent.Relationships[m.Peer].Blocked {
			continue
		}
		if m.Group == "" {
			r := consent.Relationships[m.Peer]
			if !r.LocalAccepted || !r.RemoteAccepted {
				continue
			}
		} else {
			g, ok := s.state.Groups[m.Group]
			_, member := g.Members[own]
			if !ok || (g.Owner != own && (!member || consent.Invitations[g.Invitation].Status != "accepted" || consent.Relationships[g.Owner].Blocked)) {
				continue
			}
		}
		key := conversationKey(m)
		through[key] = m.ID
		if m.Sequence > s.state.ReadSequence[key] {
			counts[key]++
		}
	}
	return counts, through
}
