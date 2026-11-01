package server

import (
	"context"
	"errors"
)

// During this synchronous tool the main model is not making inference requests.
// Its slot can be used for actual work; ordinary asynchronous calls retain the
// one-slot reservation. No model is spawned to manage or poll the queue.
func (p *executionPool) runQueue(ctx context.Context, specs []executionAssignment, order string, merge bool, versions map[string]Change) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if order == "" {
		order = "random"
	}
	if order != "random" && order != "fifo" {
		return nil, errors.New("order must be random or fifo")
	}
	p.mu.Lock()
	if p.closed || p.ctx.Err() != nil {
		p.mu.Unlock()
		return nil, errors.New("environment pool closed")
	}
	if p.persistErr != nil {
		err := p.persistErr
		p.mu.Unlock()
		return nil, err
	}
	if p.draining {
		p.mu.Unlock()
		return nil, errors.New("queue already running with the main slot borrowed")
	}
	if cap(p.slots) == 0 {
		p.mu.Unlock()
		return nil, errors.New("1X uses the ordinary main environment")
	}
	previousOrder := p.randomReady
	if len(specs) > 0 {
		if _, err := p.submitBatchLocked(specs, true, order == "random"); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	} else {
		p.draining, p.randomReady = true, order == "random"
		p.dispatchReadyLocked()
		p.notifyQueueLocked()
		p.publishLocked()
	}
	p.mu.Unlock()
	defer func() {
		// Do not resume main inference while X workers survive an interrupted
		// drain. Closing cancels and joins every task-local worker first.
		if ctx.Err() != nil || p.ctx.Err() != nil {
			p.close()
		}
		p.mu.Lock()
		p.draining, p.randomReady = false, previousOrder
		p.notifyQueueLocked()
		p.publishLocked()
		p.mu.Unlock()
	}()
	seen, mergeErrors := map[string]bool{}, map[string]string{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		ids := []string{}
		unfinished := len(p.queue) > 0 || len(p.slots) > 0
		for _, j := range p.orderedJobsLocked() {
			r := j.result
			if r.Status != "queued" && r.Status != "running" && !r.Collected && !seen[r.ID] {
				ids = append(ids, r.ID)
				seen[r.ID] = true
			}
		}
		if p.changed == nil {
			p.changed = make(chan struct{})
		}
		changed := p.changed
		p.mu.Unlock()
		for _, id := range ids {
			if _, err := p.collect(ctx, id, false, merge, versions); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				mergeErrors[id] = err.Error() // Do not retry a conflict forever.
			}
		}
		if !unfinished {
			break
		}
		if len(ids) > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		case <-changed:
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := map[string]int{}
	rows, conflicts := []map[string]any{}, []map[string]any{}
	collected, unmerged := 0, 0
	for _, j := range p.orderedJobsLocked() {
		r := j.result
		counts[r.Status]++
		if r.Collected {
			collected++
		}
		if len(r.Changes) > 0 && !r.Merged {
			unmerged++
		}
		if message := mergeErrors[r.ID]; message != "" && len(conflicts) < 16 {
			conflicts = append(conflicts, map[string]any{"id": r.ID, "error": clip(message, 600)})
		}
		if len(rows) < 32 {
			paths := []string{}
			for _, c := range r.Changes {
				if len(paths) < 8 {
					paths = append(paths, clip(c.Path, 240))
				}
			}
			rows = append(rows, map[string]any{"id": r.ID, "key": r.Key, "status": r.Status, "error": clip(r.Error, 600), "collected": r.Collected, "mergedAsPending": r.Merged, "candidatePaths": paths, "candidatePathCount": len(r.Changes), "released": r.Released})
		}
	}
	return map[string]any{
		"totalJobs": len(p.jobs), "counts": counts, "collectedJobs": collected, "uncollectedJobs": len(p.jobs) - collected, "unmergedCandidateJobs": unmerged,
		"jobsPreview": rows, "previewTruncated": len(p.jobs) > len(rows), "collectionErrors": conflicts, "totalCollectionErrors": len(mergeErrors),
		"order": order, "workSlotCapacity": cap(p.slots), "verified": false,
		"notice": "Code ran and collected the queue while the main model was paused; no dedicated manager worker. The borrowed slot is returned before main inference resumes. Preview is bounded; environment_queue pages every retained ID. Worker claims are unverified; inspect selected environment_collect(detail=true) evidence and independently validate integration. mergedAsPending is an ordinary proposal, never applied code. Conflicts require explicit resolution; failed jobs are not replayed.",
	}, nil
}
