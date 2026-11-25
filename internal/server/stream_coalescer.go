package server

import (
	"sync"
	"time"
)

const (
	// The first chunk is never delayed. Later chunks share a short network and
	// rendering frame, with a byte cap that bounds both latency and copy work.
	streamDeltaCoalesceWindow = 4 * time.Millisecond
	streamDeltaCoalesceMax    = 16 << 10
)

// streamDeltaCoalescer preserves callback order while combining adjacent deltas
// of the same kind. A model round owns one coalescer and closes it before the
// request outcome is published, so success, cancellation and errors all flush.
type streamDeltaCoalescer struct {
	app    *App
	taskID string
	round  int

	mu          sync.Mutex
	pending     *streamEvent
	timer       *time.Timer
	lastPublish time.Time
	closed      bool
}

func newStreamDeltaCoalescer(app *App, taskID string, round int) *streamDeltaCoalescer {
	return &streamDeltaCoalescer{app: app, taskID: taskID, round: round}
}

func (c *streamDeltaCoalescer) delta(text string)     { c.add("delta", text) }
func (c *streamDeltaCoalescer) reasoning(text string) { c.add("reasoning", text) }

func (c *streamDeltaCoalescer) add(kind, text string) {
	if c == nil || text == "" {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	events := make([]streamEvent, 0, 2)
	if c.pending != nil && c.pending.Event != kind {
		events = append(events, c.takeLocked())
	}
	now := time.Now()
	if c.pending == nil {
		event := streamEvent{Event: kind, Text: text, Reasoning: text, Round: c.round}
		if kind == "delta" {
			event.Reasoning = ""
		} else {
			event.Text = ""
		}
		if c.lastPublish.IsZero() || now.Sub(c.lastPublish) >= streamDeltaCoalesceWindow {
			c.lastPublish = now
			events = append(events, event)
		} else {
			c.pending = &event
			delay := streamDeltaCoalesceWindow - now.Sub(c.lastPublish)
			c.timer = time.AfterFunc(delay, c.flush)
		}
	} else if kind == "delta" {
		c.pending.Text += text
	} else {
		c.pending.Reasoning += text
	}
	if c.pending != nil && len(c.pending.Text)+len(c.pending.Reasoning) >= streamDeltaCoalesceMax {
		events = append(events, c.takeLocked())
	}
	// Publishing under the same request-local lock keeps timer and callback
	// flushes ordered. Downstream publication is non-blocking and never calls
	// back into this coalescer.
	for _, event := range events {
		c.publish(event)
	}
	c.mu.Unlock()
}

// takeLocked returns and clears the pending event. The caller holds c.mu and
// publishes it before unlocking so timer and callback flushes stay ordered.
func (c *streamDeltaCoalescer) takeLocked() streamEvent {
	event := *c.pending
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.lastPublish = time.Now()
	return event
}

func (c *streamDeltaCoalescer) flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return
	}
	event := c.takeLocked()
	c.publish(event)
	c.mu.Unlock()
}

func (c *streamDeltaCoalescer) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.pending == nil {
		c.mu.Unlock()
		return
	}
	event := c.takeLocked()
	c.publish(event)
	c.mu.Unlock()
}

func (c *streamDeltaCoalescer) publish(event streamEvent) {
	c.app.publishStream(c.taskID, event)
	if event.Event == "delta" {
		c.app.publishLiveDelta(c.taskID, event.Text)
	}
}
