package messaging

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUnreadArrivalBoundarySurvivesClockSkewAndRestart(t *testing.T) {
	a, b := newTestNode(t, 81), newTestNode(t, 82)
	connectTestNodes(t, a, b)
	peer := b.card.Device.Fingerprint
	first := Message{ID: randomID(), Peer: peer, Incoming: true, Text: "first", At: time.Now()}
	if err := a.s.appendMessage(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = randomID()
	second.At = first.At.Add(-time.Hour)
	if err := a.s.appendMessage(second); err != nil {
		t.Fatal(err)
	}
	if err := a.s.MarkReadThrough(peer, "", first.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.s.Snapshot()["unread"].(map[string]int)[peer]; got != 1 {
		t.Fatalf("delayed read cleared later arrival: %d", got)
	}
	if err := a.s.MarkReadThrough(peer, "", randomID()); err == nil {
		t.Fatal("unknown boundary accepted")
	}
	if err := a.s.MarkReadThrough(peer, "", second.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.s.MarkReadThrough(peer, "", first.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.s.Snapshot()["unread"].(map[string]int)[peer]; got != 0 {
		t.Fatalf("read regressed: %d", got)
	}
	reopened, err := NewService(a.s.identity, a.store, "restarted", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot()["unread"].(map[string]int)[peer] != 0 {
		t.Fatal("read state lost across restart")
	}
	third := second
	third.ID = randomID()
	if err := reopened.appendMessage(third); err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot()["unread"].(map[string]int)[peer] != 1 {
		t.Fatal("old remote clock swallowed new arrival")
	}
}

func TestUnreadSummaryUsesAllRetainedMessagesAndAtomicBoundaries(t *testing.T) {
	a, b := newTestNode(t, 83), newTestNode(t, 84)
	connectTestNodes(t, a, b)
	peer := b.card.Device.Fingerprint
	a.s.mu.Lock()
	for i := 0; i < 650; i++ {
		a.s.state.NextSequence++
		a.s.state.Messages = append(a.s.state.Messages, Message{ID: randomID(), Peer: peer, Text: "unread", Incoming: true, At: time.Now(), Sequence: a.s.state.NextSequence})
	}
	last := a.s.state.Messages[649].ID
	a.s.mu.Unlock()
	snapshot := a.s.Snapshot()
	if len(snapshot["messages"].([]Message)) != 500 || snapshot["unread"].(map[string]int)[peer] != 650 {
		t.Fatal("pagination truncated count")
	}
	if snapshot["readThrough"].(map[string]string)[peer] != last {
		t.Fatal("wrong explicit boundary")
	}
	if err := a.s.MarkReadBatch(map[string]string{peer: last, "invalid": randomID()}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if a.s.Snapshot()["unread"].(map[string]int)[peer] != 650 {
		t.Fatal("invalid batch partially applied")
	}
	if err := a.s.MarkReadBatch(map[string]string{peer: last}); err != nil {
		t.Fatal(err)
	}
	if a.s.Snapshot()["unread"].(map[string]int)[peer] != 0 {
		t.Fatal("mark all failed")
	}
}

func TestUnreadLegacyMigrationAndFailedWriteRollback(t *testing.T) {
	a, b := newTestNode(t, 85), newTestNode(t, 86)
	connectTestNodes(t, a, b)
	peer := b.card.Device.Fingerprint
	now := time.Now()
	a.s.state.ReadSequence = nil
	a.s.state.NextSequence = 0
	a.s.state.Read = map[string]int64{peer: now.UnixNano()}
	a.s.state.Messages = []Message{{ID: randomID(), Peer: peer, Text: "already read", Incoming: true, At: now}, {ID: randomID(), Peer: peer, Text: "unread", Incoming: true, At: now.Add(time.Second)}}
	raw, err := json.Marshal(a.s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.store.Put("state", raw); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(a.s.identity, a.store, "legacy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot()["unread"].(map[string]int)[peer] != 1 {
		t.Fatal("legacy read state changed")
	}
	a.store.Close()
	if err = reopened.MarkRead(peer, ""); err == nil {
		t.Fatal("closed store did not fail")
	}
	if reopened.Snapshot()["unread"].(map[string]int)[peer] != 1 {
		t.Fatal("failed write advanced read cursor")
	}
}
