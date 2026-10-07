package server

import (
	"context"
	"sync"
	"time"
)

// A task budget counts active execution, excluding human clarification/approval.
// Per-call model, shell and plugin timeouts remain independently bounded.
type taskBudgetKey struct{}
type taskBudget struct {
	mu         sync.Mutex
	remaining  time.Duration
	started    time.Time
	timer      *time.Timer
	generation uint64
	paused     int
	stopped    bool
	cancel     context.CancelCauseFunc
}
type taskBudgetContext struct{ context.Context }

func (c taskBudgetContext) Err() error {
	if c.Context.Err() != nil && context.Cause(c.Context) == context.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

// A wall-clock deadline would incorrectly include human waiting. Done enforces
// the active budget; downstream operations retain their own wall-clock limits.
func (c taskBudgetContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func withTaskRunBudget(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	base, cancel := context.WithCancelCause(parent)
	b := &taskBudget{remaining: duration, cancel: cancel}
	b.mu.Lock()
	b.armLocked()
	b.mu.Unlock()
	ctx := taskBudgetContext{context.WithValue(base, taskBudgetKey{}, b)}
	return ctx, func() {
		b.mu.Lock()
		b.stopped = true
		b.generation++
		if b.timer != nil {
			b.timer.Stop()
		}
		b.mu.Unlock()
		cancel(context.Canceled)
	}
}
func (b *taskBudget) armLocked() {
	b.started = time.Now()
	b.generation++
	gen := b.generation
	b.timer = time.AfterFunc(b.remaining, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.stopped || b.paused > 0 || gen != b.generation {
			return
		}
		b.stopped = true
		b.cancel(context.DeadlineExceeded)
	})
}
func pauseTaskRunBudget(ctx context.Context) func() {
	b, ok := ctx.Value(taskBudgetKey{}).(*taskBudget)
	if !ok {
		return func() {}
	}
	b.mu.Lock()
	if b.stopped || ctx.Err() != nil {
		b.mu.Unlock()
		return func() {}
	}
	if b.paused == 0 {
		b.remaining -= time.Since(b.started)
		b.generation++
		b.timer.Stop()
		if b.remaining <= 0 {
			b.stopped = true
			b.cancel(context.DeadlineExceeded)
			b.mu.Unlock()
			return func() {}
		}
	}
	b.paused++
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.paused > 0 {
				b.paused--
			}
			if !b.stopped && b.paused == 0 {
				b.armLocked()
			}
		})
	}
}
