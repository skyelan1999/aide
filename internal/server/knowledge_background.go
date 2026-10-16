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
			view, err := a.knowledgeAcquireViewLocked(batch, code, false)
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
