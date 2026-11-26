package messaging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMemberDialNumberIsStableAndRejectsAmbiguity(t *testing.T) {
	a := strings.Repeat("0", 31) + "1" + strings.Repeat("a", 32)
	b := a[:63] + "b"
	accepted := Relationship{LocalAccepted: true, RemoteAccepted: true}
	relations := map[string]Relationship{a: accepted}
	if dialNumber(a) != "00000001" {
		t.Fatal(dialNumber(a))
	}
	if peer, err := resolveDialNumber("00000001", relations); err != nil || peer != a {
		t.Fatal(peer, err)
	}
	relations[b] = accepted
	if _, err := resolveDialNumber("00000001", relations); err == nil {
		t.Fatal("ambiguous dial")
	}
	for _, rel := range []Relationship{{LocalAccepted: true}, {RemoteAccepted: true}, {LocalAccepted: true, RemoteAccepted: true, Blocked: true}} {
		if _, err := resolveDialNumber("00000001", map[string]Relationship{a: rel}); err == nil {
			t.Fatal("unauthorized peer")
		}
	}
	for _, n := range []string{"1000", "1234567", "123456789", "1234567a", "00000002"} {
		if _, err := resolveDialNumber(n, relations); err == nil {
			t.Fatalf("accepted %q", n)
		}
	}
}

func TestMemberDialUsesExistingVoiceConsentAndSignals(t *testing.T) {
	a, b := newTestNode(t, 61), newTestNode(t, 62)
	connectTestNodes(t, a, b)
	ctx := context.Background()
	number := b.s.Snapshot()["dialNumber"].(string)
	peer, err := a.s.ResolveDialNumber(number)
	if err != nil || peer != b.card.Device.Fingerprint {
		t.Fatal(peer, err)
	}
	inv, err := a.s.Request(ctx, peer, Voice, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state, signals, err := a.s.PhoneCallState(inv.ID); state != "pending" || len(signals) != 0 || err != nil {
		t.Fatal(state, signals, err)
	}
	if err = b.s.Decide(inv.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = a.s.RefreshInvitation(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err = b.s.SendSignal(ctx, a.card.Device.Fingerprint, inv.ID, inv.Resource, "", json.RawMessage(`{"candidate":"fixture"}`)); err != nil {
		t.Fatal(err)
	}
	if status, signals, err := a.s.PhoneCallState(inv.ID); status != "accepted" || len(signals) != 1 || err != nil {
		t.Fatal(status, signals, err)
	}
	if err = b.s.Cancel(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if state, signals, _ := a.s.PhoneCallState(inv.ID); state == "accepted" || len(signals) > 0 {
		t.Fatal(state)
	}
}
