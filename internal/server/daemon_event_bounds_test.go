package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestIssue86DaemonEventsBoundEncodedPayloadAndHistory(t *testing.T) {
	p := &daemonProc{}
	large := strings.Repeat("界\n<&", 20000)
	for i := 0; i < 180; i++ {
		p.appendEvent(daemonEvent{Type: "log", Message: large, Subtype: large, Direction: large, Level: large, Time: large})
	}
	total := 0
	for _, event := range p.events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		total += len(encoded)
		if len(encoded) > 32<<10 {
			t.Errorf("event retained %d encoded bytes", len(encoded))
			break
		}
		if !event.PayloadTruncated || !utf8.ValidString(event.Message) {
			t.Error("truncated event lost marker or UTF-8 boundary")
		}
	}
	encoded, err := json.Marshal(p.events)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 4<<20 || total > 4<<20 {
		t.Errorf("daemon history exceeds4MiB: %d bytes", len(encoded))
	}
}

func TestIssue86DaemonEventCountAndOrdinaryPayload(t *testing.T) {
	p := &daemonProc{}
	for i := 0; i < 1100; i++ {
		p.appendEvent(daemonEvent{Type: "event", Subtype: "owned", Message: "ordinary payload"})
	}
	if len(p.events) != 1000 || p.events[0].Message != "ordinary payload" || p.events[0].PayloadTruncated {
		t.Fatal("count cap/ordinary event changed")
	}
}

func TestIssue86ManagerGlobalBudgetEvictsOldestAndBoundsAPI(t *testing.T) {
	m := newDaemonManager(testApp(t))
	for n := 0; n < 24; n++ {
		id := fmt.Sprintf("owned-%02d", n)
		p := &daemonProc{id: id, eventPool: m.eventPool}
		m.procs[id] = p
		for i := 0; i < 180; i++ {
			p.appendEvent(daemonEvent{Type: "event", Message: id + strings.Repeat("x", 32<<10)})
		}
	}
	total := 0
	for id, p := range m.procs {
		events := m.Events(id)
		encoded, err := json.Marshal(events)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > 4<<20 {
			t.Fatalf("API history exceeded budget: %d", len(encoded))
		}
		total += p.eventBytes
		if len(p.events) == 0 && (cap(p.events) != 0 || cap(p.eventRefs) != 0) {
			t.Fatal("empty process retained backing arrays")
		}
	}
	if total > 32<<20 || total != m.eventPool.bytes {
		t.Fatalf("global budget mismatch/overflow: %d %d", total, m.eventPool.bytes)
	}
	if len(m.Events("owned-00")) != 0 || len(m.Events("owned-23")) == 0 {
		t.Fatal("global eviction did not preserve newest events")
	}
}
func TestIssue86ConcurrentProducersAndReaders(t *testing.T) {
	m := newDaemonManager(testApp(t))
	for n := 0; n < 12; n++ {
		id := fmt.Sprintf("p%d", n)
		m.procs[id] = &daemonProc{id: id, eventPool: m.eventPool}
	}
	var wg sync.WaitGroup
	for id, p := range m.procs {
		wg.Add(1)
		go func(id string, p *daemonProc) {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				p.appendEvent(daemonEvent{Type: "log", Message: strings.Repeat("\x00界", 6000)})
				if i%20 == 0 {
					_ = m.Events(id)
				}
			}
		}(id, p)
	}
	wg.Wait()
	if m.eventPool.bytes > 32<<20 {
		t.Fatal("concurrent producers exceeded global budget")
	}
}
