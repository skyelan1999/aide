package server

import (
	"context"
	"time"
)

type knowledgeContinuation struct {
	workspace string
	epoch     uint64
	policy    string
}

type knowledgeContinuationContext struct{}

func knowledgeFinishingCycle(ctx context.Context) bool {
	value, _ := ctx.Value(knowledgeContinuationContext{}).(bool)
	return value
}

// A cancelled first response can already own useful discovery/material cursors.
// Resume those too; waiting for a successful first publication would recreate
// the dependency on browser retries for large or slow initial catalogues.
func (a *App) knowledgeContinueFailed(code bool) {
	if a.bgCtx == nil || a.bgCtx.Err() != nil {
		return
	}
	state := &a.knowledgeUpdates
	mode := 0
	if code {
		mode = 1
	}
	workspace, epoch, policy := a.knowledgeUpdatesIdentity()
	if !state.starting[mode].CompareAndSwap(false, true) {
		return
	}
	a.background(func() {
		defer state.starting[mode].Store(false)
		ctx, cancel := context.WithTimeout(a.bgCtx, 30*time.Second)
		defer cancel()
		select {
		case <-ctx.Done():
			return
		case state.gate <- struct{}{}:
		}
		defer func() { <-state.gate }()
		currentWorkspace, currentEpoch, currentPolicy := a.knowledgeUpdatesIdentity()
		if ctx.Err() != nil || workspace != currentWorkspace || epoch != currentEpoch || policy != currentPolicy {
			return
		}
		// A first code/knowledge request can arrive while the other mode
		// owns the gate, before this mode has any saved view at all.
		batch, stop := context.WithTimeout(ctx, 10*time.Second)
		view, err := a.knowledgeAcquireViewLocked(batch, code, false)
		stop()
		if err != nil {
			view = state.views[mode]
		}
		if view != nil && (view.failed && view.cache != nil || !view.failed && knowledgeGraphScanning(view.graph)) {
			a.knowledgeContinueLocked(code)
		}
	})
}

// Called with the scan gate held. Finish an explicitly requested progressive
// catalogue independently of HTTP cancellation, Safari timers or paint frames.
// This is a finite job, not a perpetual filesystem watcher.
func (a *App) knowledgeContinueLocked(code bool) {
	if a.bgCtx == nil || a.bgCtx.Err() != nil {
		return
	}
	state := &a.knowledgeUpdates
	mode := 0
	if code {
		mode = 1
	}
	job := &knowledgeContinuation{state.workspace, state.epoch, state.policy}
	if old := state.continuing[mode]; old != nil && *old == *job {
		return
	}
	state.continuing[mode] = job
	a.background(func() {
		defer func() {
			state.gate <- struct{}{}
			if state.continuing[mode] == job {
				state.continuing[mode] = nil
			}
			<-state.gate
		}()
		ctx, cancel := context.WithTimeout(a.bgCtx, 30*time.Minute)
		defer cancel()
		timer := time.NewTimer(knowledgeProgressiveInterval)
		defer timer.Stop()
		failures, unchanged, revision := 0, 0, ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			select {
			case <-ctx.Done():
				return
			case state.gate <- struct{}{}:
			}
			workspace, epoch, policy := a.knowledgeUpdatesIdentity()
			if ctx.Err() != nil || state.continuing[mode] != job || workspace != job.workspace || epoch != job.epoch || policy != job.policy {
				<-state.gate
				return
			}
			batch, stop := context.WithTimeout(ctx, 10*time.Second)
			// The job already paces batches. A failed first view has no
			// published coverage yet, so the ordinary 2s refresh throttle
			// must not turn three 350ms retries into three cached failures.
			view, err := a.knowledgeAcquireViewLocked(batch, code, true)
			stop()
			done := false
			if err != nil {
				failures++
			} else {
				failures = 0
				if revision == view.revision {
					unchanged++
				} else {
					unchanged, revision = 0, view.revision
				}
				done = !knowledgeGraphScanning(view.graph)
			}
			<-state.gate
			// Unavailable/limited sources must not spin forever or repeatedly
			// access a broken host. A later live request can retry the job.
			if done || failures >= 3 || unchanged >= 8 {
				return
			}
			timer.Reset(knowledgeProgressiveInterval)
		}
	})
}
