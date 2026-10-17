package messaging

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func testIdentity(t *testing.T, value byte) *Identity {
	t.Helper()
	i, e := FromSeed(bytes.Repeat([]byte{value}, 32))
	if e != nil {
		t.Fatal(e)
	}
	return i
}
func consentFixture(t *testing.T) (*Consent, string, *State) {
	t.Helper()
	saved := new(State)
	c, e := NewConsent(State{}, func(s State) error { *saved = s; return nil })
	if e != nil {
		t.Fatal(e)
	}
	return c, testIdentity(t, 2).Device().Fingerprint, saved
}
func addContact(t *testing.T, c *Consent, p string) {
	t.Helper()
	inv, e := c.Request(p, Contact, "contact", "outgoing", "")
	if e != nil {
		t.Fatal(e)
	}
	if c.CanChat(p) {
		t.Fatal("request granted chat")
	}
	if e = c.Reply(p, inv.ID, true); e != nil {
		t.Fatal(e)
	}
}
func TestDeviceStableSeedAndKeyBinding(t *testing.T) {
	a := testIdentity(t, 1)
	b := testIdentity(t, 1)
	other := testIdentity(t, 2)
	if a.Device().Fingerprint != b.Device().Fingerprint || a.Device().Code != b.Device().Code || a.Device().Fingerprint == other.Device().Fingerprint {
		t.Fatal("device identity not seed bound")
	}
	d := a.Device()
	msg := []byte("invite")
	if !Verify(d, msg, a.Sign(msg)) || Verify(other.Device(), msg, a.Sign(msg)) || Verify(d, []byte("changed"), a.Sign(msg)) {
		t.Fatal("signature binding failure")
	}
	d.Code = "000000"
	if Verify(d, msg, a.Sign(msg)) {
		t.Fatal("unbound short code")
	}
	blob, _ := json.Marshal(a)
	if string(blob) != "{}" {
		t.Fatal("identity leaked private fields")
	}
	if len(ResolveCode(a.Device().Code, []Device{a.Device(), a.Device(), other.Device(), d})) != 1 {
		t.Fatal("discovery validation/deduplication")
	}
	if _, e := FromSeed([]byte("short")); e == nil {
		t.Fatal("invalid seed")
	}
}
func TestContactsSeparateFromResourceConsentAndReplays(t *testing.T) {
	c, p, _ := consentFixture(t)
	if _, e := c.Request(p, File, "file1", "incoming", "11111111111111111111111111111111"); !errors.Is(e, ErrDenied) {
		t.Fatal("stranger file accepted", e)
	}
	addContact(t, c, p)
	for _, kind := range []Kind{Group, Voice, File} {
		inv, e := c.Request(p, kind, string(kind), "incoming", map[Kind]string{Group: "11111111111111111111111111111111", Voice: "22222222222222222222222222222222", File: "33333333333333333333333333333333"}[kind])
		if e != nil {
			t.Fatal(e)
		}
		if c.Authorized(p, inv.ID, kind, string(kind)) {
			t.Fatal("pending grant")
		}
		if e = c.Reply(p, inv.ID, true); !errors.Is(e, ErrDenied) {
			t.Fatal("remote decided incoming request")
		}
		if e = c.Decide(inv.ID, true); e != nil {
			t.Fatal(e)
		}
		if !c.Authorized(p, inv.ID, kind, string(kind)) || c.Authorized(p, inv.ID, kind, "another") {
			t.Fatal("resource binding")
		}
		if e = c.Decide(inv.ID, true); !errors.Is(e, ErrDenied) {
			t.Fatal("replay decision")
		}
		if e = c.Revoke(inv.ID); e != nil {
			t.Fatal(e)
		}
		if c.Authorized(p, inv.ID, kind, string(kind)) {
			t.Fatal("revoked grant")
		}
	}
}
func TestBlockRevokesGrantsAndUnblockDoesNotRestore(t *testing.T) {
	c, p, saved := consentFixture(t)
	addContact(t, c, p)
	inv, e := c.Request(p, File, "payload", "outgoing", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Reply(p, inv.ID, true); e != nil {
		t.Fatal(e)
	}
	if e = c.Block(p, true); e != nil {
		t.Fatal(e)
	}
	restored, e := NewConsent(*saved, func(State) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if restored.CanChat(p) || restored.Authorized(p, inv.ID, File, "payload") {
		t.Fatal("block lost after restart")
	}
	if e = restored.Block(p, false); e != nil {
		t.Fatal(e)
	}
	if restored.CanChat(p) || restored.Reply(p, inv.ID, true) == nil {
		t.Fatal("unblock restored authority")
	}
}
func TestFailedPersistenceDoesNotGrantAndSnapshotsAreCopies(t *testing.T) {
	c, p, _ := consentFixture(t)
	c.save = func(State) error { return errors.New("disk full") }
	if _, e := c.Request(p, Contact, "contact", "outgoing", ""); e == nil {
		t.Fatal("write failure hidden")
	}
	if len(c.Snapshot().Invitations) != 0 {
		t.Fatal("failed persistence mutated state")
	}
	c.save = func(State) error { return nil }
	addContact(t, c, p)
	snapshot := c.Snapshot()
	snapshot.Relationships[p] = Relationship{Blocked: true}
	if !c.CanChat(p) {
		t.Fatal("snapshot aliases state")
	}
}
func TestInviteExpiryAndAuthenticatedReply(t *testing.T) {
	c, p, _ := consentFixture(t)
	addContact(t, c, p)
	inv, e := c.Request(p, Voice, "call", "outgoing", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Reply(testIdentity(t, 3).Device().Fingerprint, inv.ID, true); !errors.Is(e, ErrDenied) {
		t.Fatal("wrong peer reply")
	}
	c.now = func() time.Time { return inv.Expires.Add(time.Second) }
	if e = c.Reply(p, inv.ID, true); !errors.Is(e, ErrDenied) {
		t.Fatal("expired invite")
	}
}
func TestConsentConcurrentBlockAndRead(t *testing.T) {
	c, p, _ := consentFixture(t)
	addContact(t, c, p)
	var wg sync.WaitGroup
	for n := 0; n < 30; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.CanChat(p)
			c.Snapshot()
			if e := c.Block(p, true); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if c.CanChat(p) {
		t.Fatal("blocked peer")
	}
}

func TestDeviceShortCodeCollisionPreservesBothFullKeys(t *testing.T) {
	keys := []string{"3af2cc844c0eeefc11aa907678be7347be1c1dc467e6bd7f4ba4e561a5909562", "0cba2fa7ffebce9bb31e312ee655e6c071e3e83dadeca2d5a75a67a0a9aa9421"}
	devices := []Device{}
	for _, key := range keys {
		raw, err := hex.DecodeString(key)
		if err != nil {
			t.Fatal(err)
		}
		device, err := PublicDevice(raw)
		if err != nil {
			t.Fatal(err)
		}
		devices = append(devices, device)
	}
	if devices[0].Code != devices[1].Code || devices[0].Fingerprint == devices[1].Fingerprint {
		t.Fatal("fixture must collide only on short code")
	}
	if got := ResolveCode(devices[0].Code, append(devices, devices[0])); len(got) != 2 {
		t.Fatalf("lost collision candidates: %d", len(got))
	}
}
