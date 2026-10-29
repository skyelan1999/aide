package server

import (
	"container/list"
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	daemonMaxEventBytes     = 32 << 10
	daemonProcessEventBytes = 4 << 20
	daemonTotalEventBytes   = 32 << 20
	daemonEventOverhead     = 512
)

// Pool order contains only retained events, not tombstones. This bounds both
// string payloads and the bookkeeping across all processes of one manager.
type daemonEventPool struct {
	mu    sync.Mutex
	order list.List
	bytes int
}

func (p *daemonProc) eventMutex() *sync.Mutex {
	if p.eventPool != nil {
		return &p.eventPool.mu
	}
	return &p.eventsMu
}

// Count JSON bytes, including HTML/control escaping, while retaining only a
// copied prefix at a UTF-8 boundary. A sliced giant input must not stay alive.
func clipDaemonText(text string, budget int) (string, bool) {
	changed := !utf8.ValidString(text)
	if changed {
		text = strings.ToValidUTF8(text, "\ufffd")
	}
	used := 0
	for i, r := range text {
		cost := utf8.RuneLen(r)
		switch {
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			cost = 6
		case r == '"' || r == '\\':
			cost = 2
		}
		if used+cost > budget {
			return strings.Clone(text[:i]), true
		}
		used += cost
	}
	return strings.Clone(text), changed
}

func boundedDaemonEvent(ev daemonEvent) daemonEvent {
	clip := func(s string, n int) string {
		value, cut := clipDaemonText(s, n)
		ev.PayloadTruncated = ev.PayloadTruncated || cut
		return value
	}
	ev.Type = clip(ev.Type, 128)
	ev.Time = clip(ev.Time, 128)
	ev.Direction = clip(ev.Direction, 128)
	ev.Subtype = clip(ev.Subtype, 256)
	ev.Level = clip(ev.Level, 64)
	// Leave ample space for field names, numbers, quotes and bounded metadata.
	ev.Message = clip(ev.Message, daemonMaxEventBytes-2048)
	return ev
}

func (p *daemonProc) dropOldestEventLocked() {
	weight := p.eventWeights[0]
	p.eventBytes -= weight
	if p.eventPool != nil {
		p.eventPool.bytes -= weight
		p.eventPool.order.Remove(p.eventRefs[0])
		p.eventRefs[0] = nil
		p.eventRefs = p.eventRefs[1:]
	}
	// Clear evicted values so backing arrays cannot retain their strings.
	p.events[0] = daemonEvent{}
	p.eventWeights[0] = 0
	p.events = p.events[1:]
	p.eventWeights = p.eventWeights[1:]
	p.eventDrops++
	if len(p.events) == 0 {
		p.events, p.eventWeights, p.eventRefs = nil, nil, nil
		p.eventDrops = 0
	} else if p.eventDrops >= len(p.events) {
		// Global eviction can leave a few events in many processes. Compact
		// regularly rather than retaining a formerly large backing array.
		p.events = append([]daemonEvent(nil), p.events...)
		p.eventWeights = append([]int(nil), p.eventWeights...)
		p.eventRefs = append([]*list.Element(nil), p.eventRefs...)
		p.eventDrops = 0
	}
}

func (p *daemonProc) retainEvent(ev daemonEvent) {
	ev = boundedDaemonEvent(ev)
	encoded, _ := json.Marshal(ev)
	weight := len(encoded) + daemonEventOverhead
	mu := p.eventMutex()
	mu.Lock()
	defer mu.Unlock()
	p.events = append(p.events, ev)
	p.eventWeights = append(p.eventWeights, weight)
	p.eventBytes += weight
	if p.eventPool != nil {
		p.eventPool.bytes += weight
		p.eventRefs = append(p.eventRefs, p.eventPool.order.PushBack(p))
	}
	for len(p.events) > daemonEventCap || p.eventBytes > daemonProcessEventBytes {
		p.dropOldestEventLocked()
	}
	if pool := p.eventPool; pool != nil {
		for pool.bytes > daemonTotalEventBytes {
			pool.order.Front().Value.(*daemonProc).dropOldestEventLocked()
		}
	}
}
