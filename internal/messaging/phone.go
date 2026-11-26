package messaging

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// Display numbers derive from the existing device identity, including for old
// contacts. Keep them outside signed Device/Card payloads for wire compatibility.
func dialNumber(fingerprint string) string {
	if !validFingerprint(fingerprint) {
		return ""
	}
	digest, _ := hex.DecodeString(fingerprint)
	return fmt.Sprintf("%08d", binary.BigEndian.Uint64(digest[8:16])%100000000)
}

func resolveDialNumber(number string, relations map[string]Relationship) (string, error) {
	if len(number) != 8 {
		return "", errors.New("请输入 8 位成员编号")
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return "", errors.New("请输入 8 位成员编号")
		}
	}
	var match string
	for peer, rel := range relations {
		if rel.Blocked || !rel.LocalAccepted || !rel.RemoteAccepted || dialNumber(peer) != number {
			continue
		}
		if match != "" {
			return "", errors.New("成员编号重复，请在消息中选择联系人发起语音")
		}
		match = peer
	}
	if match == "" {
		return "", errors.New("未找到可呼叫的成员")
	}
	return match, nil
}

func (s *Service) ResolveDialNumber(number string) (string, error) {
	return resolveDialNumber(number, s.consent.Snapshot().Relationships)
}

// Phone calls only poll their own direct invitation and authorized signaling.
// The SIP host does not need the message history or other contacts' metadata.
func (s *Service) PhoneCallState(id string) (string, []Signal, error) {
	state := s.consent.Snapshot()
	inv, ok := state.Invitations[id]
	if !ok || inv.Direction != "outgoing" || inv.Kind != Voice || !s.consent.allowed(state, inv.Peer) {
		return "", nil, ErrDenied
	}
	if inv.Status != "accepted" {
		return inv.Status, nil, nil
	}
	if !s.VoiceSessionAllowed(inv.Peer, id, inv.Resource, "") {
		return "", nil, ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var signals []Signal
	for _, signal := range s.signals {
		if signal.Call == inv.Resource && signal.Peer == inv.Peer && signal.Invitation == id && signal.Group == "" {
			signals = append(signals, signal)
		}
	}
	return inv.Status, signals, nil
}
