package server

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// outputTokenMeter converts generated UTF-16 text units with the same local
// heuristic used by context estimates. Cumulative state avoids rounding every
// small provider chunk independently and makes the result chunk invariant.
type outputTokenMeter struct {
	characters int
	tokens     int
}

func (m *outputTokenMeter) add(delta string) int {
	if delta == "" {
		return 0
	}
	m.characters += utf16CodeUnits(delta)
	next := estimateContextTextTokens(m.characters)
	increment := next - m.tokens
	m.tokens = next
	return increment
}

// sessionOutputRate is attached to the execution context. Every provider call
// derived from that execution registers its own stream, so visible answers,
// hidden reasoning, tool arguments, planning, title generation, compaction and
// approval calls all feed the same current-session rate instead of relying on
// the primary answer's delta callback.
type sessionOutputRate struct {
	app       *App
	sessionID string
	taskID    string
	next      atomic.Uint64
}

type sessionOutputRateKey struct{}

func (a *App) withSessionOutputRate(ctx context.Context, sessionID, taskID string) context.Context {
	if ctx == nil || sessionID == "" || taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionOutputRateKey{}, &sessionOutputRate{app: a, sessionID: sessionID, taskID: taskID})
}

const (
	sessionOutputRateFlushInterval = 75 * time.Millisecond
	sessionOutputRateMaxPending    = 64
)

type sessionRequestOutputMeter struct {
	session     *sessionOutputRate
	streamID    string
	meter       outputTokenMeter
	pending     int
	lastPublish time.Time
	now         func() time.Time
}

func newSessionRequestOutputMeter(ctx context.Context) *sessionRequestOutputMeter {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(sessionOutputRateKey{}).(*sessionOutputRate)
	if session == nil || session.app == nil || session.sessionID == "" || session.taskID == "" {
		return nil
	}
	id := session.next.Add(1)
	return &sessionRequestOutputMeter{session: session, streamID: fmt.Sprintf("session/request/%d", id), now: time.Now}
}

func (m *sessionRequestOutputMeter) flush() {
	if m == nil || m.pending == 0 {
		return
	}
	tokens := m.pending
	m.pending = 0
	m.lastPublish = m.now()
	m.session.app.publishSessionOutputRate(m.session.sessionID, m.session.taskID, streamEvent{Event: "output-rate", StreamID: m.streamID, OutputTokens: tokens})
}

func (m *sessionRequestOutputMeter) add(delta string) {
	if m == nil {
		return
	}
	m.pending += m.meter.add(delta)
	if m.pending == 0 {
		return
	}
	pending := m.pending
	if pending < 0 {
		pending = -pending
	}
	if m.lastPublish.IsZero() || m.now().Sub(m.lastPublish) >= sessionOutputRateFlushInterval || pending >= sessionOutputRateMaxPending {
		m.flush()
	}
}

func (m *sessionRequestOutputMeter) addMessage(content string, calls []ToolCall) {
	if m == nil {
		return
	}
	m.add(content)
	for _, call := range calls {
		m.add(call.Function.Name)
		m.add(call.Function.Arguments)
	}
}

// finish reconciles the live text estimate to the provider's complete output
// usage (including hidden reasoning and tool calls). A signed correction keeps
// the session total accurate even when the provider counts fewer tokens than
// the local estimate; callers pass the existing estimated usage when upstream
// omitted usage, preserving the repository's established fallback semantics.
func (m *sessionRequestOutputMeter) finish(actualOutputTokens int) {
	if m == nil {
		return
	}
	m.pending += actualOutputTokens - m.meter.tokens
	m.meter.tokens = actualOutputTokens
	m.flush()
}
