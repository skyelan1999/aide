package messaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

type testNode struct {
	s      *Service
	store  *Store
	server *http.Server
	card   Card
}

func newTestNode(t *testing.T, value byte) *testNode {
	t.Helper()
	identity := testIdentity(t, value)
	store, e := OpenStore(t.TempDir(), bytes.Repeat([]byte{value}, 32))
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewService(identity, store, "test", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	n := &testNode{s: s, store: store}
	n.listen(t)
	t.Cleanup(func() { n.server.Close(); n.s.Close(); n.store.Close() })
	return n
}
func (n *testNode) listen(t *testing.T) {
	t.Helper()
	ln, e := tls.Listen("tcp", "127.0.0.1:0", ServerTLS(n.s.Certificate()))
	if e != nil {
		t.Fatal(e)
	}
	if e = n.s.SetAddresses([]string{"https://" + ln.Addr().String()}); e != nil {
		t.Fatal(e)
	}
	n.server = &http.Server{Handler: n.s.Handler(), ReadHeaderTimeout: time.Second}
	go n.server.Serve(ln)
	n.card, e = n.s.Card()
	if e != nil {
		t.Fatal(e)
	}
}
func connectTestNodes(t *testing.T, a, b *testNode) {
	t.Helper()
	if e := a.s.Remember(b.card); e != nil {
		t.Fatal(e)
	}
	if e := b.s.Remember(a.card); e != nil {
		t.Fatal(e)
	}
	inv, e := a.s.Request(context.Background(), b.card.Device.Fingerprint, Contact, "contact", nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.s.Decide(inv.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = a.s.RefreshInvitation(context.Background(), inv.ID); e != nil {
		t.Fatal(e)
	}
}

func seedIssue66Transfers(t *testing.T, s *Service, peer, status string, incoming bool, count int) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consent.mu.Lock()
	defer s.consent.mu.Unlock()
	data := []byte("history")
	digest := sha256.Sum256(data)
	first := ""
	for n := 1; n <= count; n++ {
		id := fmt.Sprintf("%032x", n)
		if first == "" {
			first = id
		}
		inv := Invitation{ID: id, Peer: peer, Kind: File, Resource: id, Direction: "outgoing", Status: "accepted", Expires: time.Now().Add(time.Hour)}
		if incoming {
			inv.Direction = "incoming"
		}
		f := Transfer{ID: id, Peer: peer, Incoming: incoming, Manifest: Manifest{Name: "history.bin", Size: int64(len(data)), Hashes: []string{hex.EncodeToString(digest[:])}}, Received: map[int]bool{}, Created: time.Now()}
		switch status {
		case "cancelled", "cleanup-failed":
			f.Cancelled = true
			inv.Status = "revoked"
			if status == "cleanup-failed" {
				f.CleanupError = "fixture cleanup still needs retry"
			}
		case "declined":
			inv.Status = "declined"
		case "expired":
			inv.Status = "pending"
			inv.Expires = time.Now().Add(-time.Hour)
		case "completed":
			f.Received[0] = true
			if incoming {
				if err := s.store.Put("chunk-"+id+"-0", data); err != nil {
					t.Fatal(err)
				}
			}
		}
		s.state.Transfers[id] = f
		s.consent.state.Invitations[id] = inv
	}
	if err := s.persistLocked(); err != nil {
		t.Fatal(err)
	}
	if err := s.consent.save(clone(s.consent.state)); err != nil {
		t.Fatal(err)
	}
	return first
}

func TestIssue66HistoricalTransfersDoNotExhaustActiveCapacity(t *testing.T) {
	for _, status := range []string{"cancelled", "declined", "expired", "completed"} {
		t.Run(status, func(t *testing.T) {
			a, b := newTestNode(t, 71), newTestNode(t, 72)
			connectTestNodes(t, a, b)
			seedIssue66Transfers(t, a.s, b.card.Device.Fingerprint, status, false, 128)
			first := seedIssue66Transfers(t, b.s, a.card.Device.Fingerprint, status, true, 128)
			manifest := Manifest{Name: "next.bin", Size: 0, Hashes: []string{}}
			inv, err := a.s.Request(context.Background(), b.card.Device.Fingerprint, File, "next", &manifest)
			if err != nil {
				t.Errorf("history blocked a new outgoing transfer: %v", err)
			} else if _, ok := b.s.consent.Snapshot().Invitations[inv.ID]; !ok {
				t.Error("new offer was not received by the real peer")
			}
			// Exercise the receiving admission path independently, even when the
			// sender's preflight rejected the request above.
			incoming, err := b.s.consent.Request(a.card.Device.Fingerprint, File, "next-direct", "incoming", randomID())
			if err != nil {
				t.Fatal(err)
			}
			if err := b.s.receiveFileOffer(incoming, manifest); err != nil {
				t.Errorf("history blocked a new incoming offer: %v", err)
			}
			if status == "completed" {
				var content bytes.Buffer
				if err := b.s.Download(context.Background(), first, &content); err != nil || content.String() != "history" {
					t.Fatalf("new admission lost previously received content: %q %v", content.String(), err)
				}
			}
		})
	}
}

func TestIssue66ActiveTransfersAndFailedCleanupRemainBounded(t *testing.T) {
	for _, status := range []string{"active", "cleanup-failed"} {
		t.Run(status, func(t *testing.T) {
			a, b := newTestNode(t, 73), newTestNode(t, 74)
			connectTestNodes(t, a, b)
			seedIssue66Transfers(t, a.s, b.card.Device.Fingerprint, status, false, 128)
			seedIssue66Transfers(t, b.s, a.card.Device.Fingerprint, status, true, 128)
			manifest := Manifest{Name: "next.bin", Hashes: []string{}}
			if _, err := a.s.Request(context.Background(), b.card.Device.Fingerprint, File, "next", &manifest); err == nil {
				t.Error("admitted more than 128 active transfers")
			}
			incoming, err := b.s.consent.Request(a.card.Device.Fingerprint, File, "next-direct", "incoming", randomID())
			if err != nil {
				t.Fatal(err)
			}
			if err := b.s.receiveFileOffer(incoming, manifest); err == nil {
				t.Error("received more than 128 active transfers")
			}
		})
	}
}

func TestIssue66ConcurrentRequestsCannotExceedActiveLimit(t *testing.T) {
	a, b := newTestNode(t, 75), newTestNode(t, 76)
	connectTestNodes(t, a, b)
	seedIssue66Transfers(t, a.s, b.card.Device.Fingerprint, "active", false, 127)
	seedIssue66Transfers(t, b.s, a.card.Device.Fingerprint, "active", true, 127)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, err := a.s.Request(ctx, b.card.Device.Fingerprint, File, fmt.Sprintf("concurrent-%d", n), &Manifest{Name: "next.bin", Hashes: []string{}})
			results <- err
		}(n)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("127 active transfers should admit exactly one request; admitted %d", successes)
	}
	for _, node := range []*testNode{a, b} {
		node.s.mu.Lock()
		count := len(node.s.state.Transfers)
		node.s.mu.Unlock()
		if count != 128 {
			t.Fatalf("concurrent admission retained %d active transfers", count)
		}
	}
}

func TestRealNodesContactChatAndBlock(t *testing.T) {
	a, b := newTestNode(t, 11), newTestNode(t, 12)
	ctx := context.Background()
	if e := a.s.Remember(b.card); e != nil {
		t.Fatal(e)
	}
	if e := a.s.SendText(ctx, b.card.Device.Fingerprint, "", "not authorized"); e == nil {
		t.Fatal("stranger chat permitted")
	}
	connectTestNodes(t, a, b)
	if e := a.s.SendText(ctx, b.card.Device.Fingerprint, "", "hello encrypted"); e != nil {
		t.Fatal(e)
	}
	b.s.mu.Lock()
	messages := append([]Message(nil), b.s.state.Messages...)
	b.s.mu.Unlock()
	if len(messages) != 1 || messages[0].Text != "hello encrypted" {
		t.Fatal("missing received message")
	}
	if e := b.s.Block(a.card.Device.Fingerprint, true); e != nil {
		t.Fatal(e)
	}
	if e := a.s.SendText(ctx, b.card.Device.Fingerprint, "", "blocked"); e == nil {
		t.Fatal("blocked chat delivered")
	}
}
func TestRealFileConsentResumeTamperAndRevocation(t *testing.T) {
	a, b := newTestNode(t, 13), newTestNode(t, 14)
	connectTestNodes(t, a, b)
	ctx := context.Background()
	data := bytes.Repeat([]byte{42}, ChunkSize+19)
	first := sha256.Sum256(data[:ChunkSize])
	second := sha256.Sum256(data[ChunkSize:])
	manifest := Manifest{Name: "../do-not-execute.html", Size: int64(len(data)), Hashes: []string{hex.EncodeToString(first[:]), hex.EncodeToString(second[:])}}
	inv, e := a.s.Request(ctx, b.card.Device.Fingerprint, File, "", &manifest)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.s.UploadChunk(ctx, inv.ID, 0, data[:ChunkSize]); e == nil {
		t.Fatal("sent file before consent")
	}
	if e = b.s.Decide(inv.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = a.s.RefreshInvitation(ctx, inv.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.s.UploadChunk(ctx, inv.ID, 1, data[ChunkSize:]); e != nil {
		t.Fatal(e)
	}
	restored, e := NewService(b.s.identity, b.store, "restarted", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	b.server.Close()
	b.s.Close()
	b.s = restored
	b.listen(t)
	if e = a.s.Remember(b.card); e != nil {
		t.Fatal(e)
	}
	if _, e = a.s.RefreshInvitation(ctx, inv.ID); e != nil {
		t.Fatal(e)
	}
	a.s.mu.Lock()
	received := a.s.state.Transfers[inv.ID].Received[1]
	a.s.mu.Unlock()
	if !received {
		t.Fatal("resume bitmap lost after restart")
	}
	altered := append([]byte(nil), data[:ChunkSize]...)
	altered[0] ^= 1
	if e = a.s.UploadChunk(ctx, inv.ID, 0, altered); e == nil {
		t.Fatal("changed source accepted")
	}
	if e = a.s.UploadChunk(ctx, inv.ID, 0, data[:ChunkSize]); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = b.s.Download(ctx, inv.ID, &out); e != nil || !bytes.Equal(out.Bytes(), data) {
		t.Fatal("file reconstruction failed", e)
	}
	if e = b.s.Block(a.card.Device.Fingerprint, true); e != nil {
		t.Fatal(e)
	}
	if e = a.s.UploadChunk(ctx, inv.ID, 0, data[:ChunkSize]); e == nil {
		t.Fatal("blocked transfer resumed")
	}
}
func TestGroupRequiresEachSignedAcceptance(t *testing.T) {
	a, b, c := newTestNode(t, 21), newTestNode(t, 22), newTestNode(t, 23)
	connectTestNodes(t, a, b)
	connectTestNodes(t, a, c)
	ctx := context.Background()
	g, e := a.s.CreateGroup(ctx, "team", []string{b.card.Device.Fingerprint, c.card.Device.Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	invitations := a.s.consent.Snapshot().Invitations
	for _, inv := range invitations {
		if inv.Kind != Group {
			continue
		}
		node := b
		if inv.Peer == c.card.Device.Fingerprint {
			node = c
		}
		if e = node.s.Decide(inv.ID, true); e != nil {
			t.Fatal(e)
		}
		if _, e = a.s.RefreshInvitation(ctx, inv.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e = b.s.SendText(ctx, "", g.ID, "group message"); e != nil {
		t.Fatal(e)
	}
	c.s.mu.Lock()
	messages := c.s.state.Messages
	c.s.mu.Unlock()
	if len(messages) != 1 || messages[0].Text != "group message" {
		t.Fatal("group message missing")
	}
	call, e := a.s.StartGroupCall(ctx, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	if a.s.callSignalAllowed(b.card.Device.Fingerprint, g.ID, call.ID) {
		t.Fatal("joining group also accepted call")
	}
	for _, inv := range a.s.consent.Snapshot().Invitations {
		if inv.Kind != Voice {
			continue
		}
		node := b
		if inv.Peer == c.card.Device.Fingerprint {
			node = c
		}
		if e = node.s.Decide(inv.ID, true); e != nil {
			t.Fatal(e)
		}
		if _, e = a.s.RefreshInvitation(ctx, inv.ID); e != nil {
			t.Fatal(e)
		}
	}
	if !b.s.callSignalAllowed(c.card.Device.Fingerprint, g.ID, call.ID) {
		t.Fatal("accepted group call roster not synchronized")
	}
}

func TestGroupRosterRetryAfterPersistedAcceptance(t *testing.T) {
	a, b := newTestNode(t, 31), newTestNode(t, 32)
	connectTestNodes(t, a, b)
	ctx := context.Background()
	g, err := a.s.CreateGroup(ctx, "retry", []string{b.card.Device.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	var inv Invitation
	for _, candidate := range a.s.consent.Snapshot().Invitations {
		if candidate.Kind == Group {
			inv = candidate
		}
	}
	if err = b.s.Decide(inv.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = a.s.consent.Reply(inv.Peer, inv.ID, true); err != nil {
		t.Fatal(err)
	}
	proof := b.s.membership(g.ID, a.card.Device.Fingerprint, inv.ID)
	b.server.Close()
	if err = a.s.addGroupMember(ctx, a.s.consent.Snapshot().Invitations[inv.ID], &proof); err == nil {
		t.Fatal("offline roster sync should report delivery failure")
	}
	b.listen(t)
	if err = a.s.Remember(b.card); err != nil {
		t.Fatal(err)
	}
	a.s.retryRoster(ctx, 0)
	b.s.mu.Lock()
	received := b.s.state.Groups[g.ID]
	b.s.mu.Unlock()
	if len(received.Members) != 2 {
		t.Fatal("persisted membership was not retried")
	}
	a.s.retryRoster(ctx, 0) // Same signed revision is safely idempotent.
	if err = b.s.SendText(ctx, "", g.ID, "retry works"); err != nil {
		t.Fatal(err)
	}
	// Leaving while the owner is offline persists the revoke for later delivery.
	a.server.Close()
	if err = b.s.Cancel(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if b.s.consent.Snapshot().Invitations[inv.ID].RevocationAcknowledged {
		t.Fatal("offline revoke acknowledged")
	}
	a.listen(t)
	if err = b.s.Remember(a.card); err != nil {
		t.Fatal(err)
	}
	if _, err = b.s.RefreshInvitation(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if !b.s.consent.Snapshot().Invitations[inv.ID].RevocationAcknowledged {
		t.Fatal("revoke acknowledgement not persisted")
	}
	a.s.mu.Lock()
	remaining := len(a.s.state.Groups[g.ID].Members)
	a.s.mu.Unlock()
	if remaining != 1 {
		t.Fatal("owner retained revoked group membership")
	}
	if err = b.s.SendText(ctx, "", g.ID, "after leaving"); err == nil {
		t.Fatal("left group still usable")
	}
}

func TestRealFileQuotaReservedBeforeContentAndReleasedOnCancel(t *testing.T) {
	a, b := newTestNode(t, 41), newTestNode(t, 42)
	connectTestNodes(t, a, b)
	ctx := context.Background()
	hashes := make([]string, int(MaxFileSize/ChunkSize))
	digest := sha256.Sum256(nil)
	for index := range hashes {
		hashes[index] = hex.EncodeToString(digest[:])
	}
	ids := []string{}
	for index := 0; index < 5; index++ {
		inv, err := a.s.Request(ctx, b.card.Device.Fingerprint, File, "", &Manifest{Name: "quota-fixture.bin", Size: MaxFileSize, Hashes: hashes})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, inv.ID)
		if index < 4 {
			if err = b.s.Decide(inv.ID, true); err != nil {
				t.Fatal(err)
			}
		} else if err = b.s.Decide(inv.ID, true); err == nil {
			t.Fatal("accepted more than receive quota")
		}
	}
	if err := b.s.Cancel(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := b.s.Decide(ids[4], true); err != nil {
		t.Fatal("cancel did not release unreceived reservation", err)
	}
}
