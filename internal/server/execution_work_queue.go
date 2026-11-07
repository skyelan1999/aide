package server

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"
)

// Jobs are metadata until admitted. Queued jobs have no scratch workspace,
// provider request or plugin process. X limits active runtimes, not task count.
type executionAssignment struct {
	Key        string   `json:"key,omitempty"`
	Assignment string   `json:"assignment"`
	DependsOn  []string `json:"dependsOn,omitempty"`
}

type executionQueueJob struct {
	ID        string   `json:"id"`
	Key       string   `json:"key,omitempty"`
	Status    string   `json:"status"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

func (p *executionPool) notifyQueueLocked() {
	if p.changed != nil {
		close(p.changed)
	}
	p.changed = make(chan struct{})
}

// Validate the whole graph before accepting any work. Batch-local keys may
// refer forwards; existing runtime IDs remain supported by the single API.
func (p *executionPool) submitBatch(specs []executionAssignment) ([]executionQueueJob, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.submitBatchLocked(specs, false, false)
}

func (p *executionPool) submitBatchLocked(specs []executionAssignment, drain, random bool) ([]executionQueueJob, error) {
	if p.persistErr != nil {
		return nil, p.persistErr
	}
	if p.closed || p.ctx.Err() != nil {
		return nil, errors.New("environment pool closed")
	}
	if cap(p.slots) == 0 {
		return nil, errors.New("1X has only the main environment; execute this assignment with main tools")
	}
	if len(specs) == 0 {
		return nil, errors.New("jobs is empty")
	}
	keys, ids := map[string]string{}, make([]string, len(specs))
	for i, spec := range specs {
		if strings.TrimSpace(spec.Assignment) == "" {
			return nil, fmt.Errorf("job %d assignment is empty", i)
		}
		ids[i] = newID()
		if spec.Key != "" {
			if _, ok := keys[spec.Key]; ok || p.jobs[spec.Key] != nil {
				return nil, fmt.Errorf("duplicate or ambiguous job key %q", spec.Key)
			}
			keys[spec.Key] = ids[i]
		}
	}
	deps := map[string][]string{}
	for i, spec := range specs {
		for _, ref := range spec.DependsOn {
			id := keys[ref]
			if id == "" {
				id = ref
				if p.jobs[id] == nil {
					return nil, fmt.Errorf("unknown dependency %q", ref)
				}
			}
			deps[ids[i]] = append(deps[ids[i]], id)
		}
	}
	marks := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		if marks[id] == 1 {
			return errors.New("dependency cycle in batch")
		}
		if marks[id] == 2 || p.jobs[id] != nil {
			return nil
		}
		marks[id] = 1
		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		marks[id] = 2
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	if p.root == "" {
		var err error
		p.root, err = os.MkdirTemp("", "aide-environments-")
		if err != nil {
			return nil, err
		}
	}
	oldQueue := len(p.queue)
	accepted := make([]executionQueueJob, 0, len(specs))
	for i, spec := range specs {
		ctx, cancel := context.WithCancel(p.ctx)
		r := ExecutionEnvironmentResult{ID: ids[i], Key: spec.Key, Assignment: spec.Assignment, DependsOn: deps[ids[i]], Status: "queued", Released: true, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		p.jobs[r.ID] = &executionJob{ctx: ctx, cancel: cancel, result: r, done: make(chan struct{}), dependencies: r.DependsOn}
		p.queue = append(p.queue, r.ID)
		accepted = append(accepted, executionQueueJob{ID: r.ID, Key: r.Key, Status: r.Status, DependsOn: r.DependsOn})
	}
	p.publishLocked()
	if p.persistErr != nil {
		for _, id := range ids {
			p.jobs[id].cancel()
			delete(p.jobs, id)
		}
		p.queue = p.queue[:oldQueue]
		// The failed checkpoint is not an accepted or executable batch.
		p.publishLocked()
		return nil, p.persistErr
	}
	p.wg.Add(len(ids))
	if drain {
		p.draining, p.randomReady = true, random
	}
	p.notifyQueueLocked()
	p.dispatchReadyLocked()
	p.publishLocked()
	return accepted, nil
}

func (p *executionPool) workerLimitLocked() int {
	if cap(p.slots) == 0 {
		return 0
	}
	if p.draining {
		return cap(p.slots)
	}
	return cap(p.slots) - 1 // The main model may still be computing asynchronously.
}

func (p *executionPool) finishQueuedLocked(j *executionJob, err error) {
	j.cancel()
	j.result.Status, j.result.Error = "failed", err.Error()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		j.result.Status = "cancelled"
	}
	j.result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	close(j.done)
	p.wg.Done()
	p.notifyQueueLocked()
}

// FIFO among ready jobs, skipping blocked dependencies. Called on admission,
// completion and cancellation, independently of the main model's next turn.
func (p *executionPool) dispatchReadyLocked() {
	for {
		if p.randomReady {
			// Shuffle candidates, then skip blocked/failed dependencies below.
			// Every ready job has the same chance of the next free slot.
			for i := len(p.queue) - 1; i > 0; i-- {
				j := rand.IntN(i + 1)
				p.queue[i], p.queue[j] = p.queue[j], p.queue[i]
			}
		}
		removed := false
		remaining := p.queue[:0]
		for _, id := range p.queue {
			j := p.jobs[id]
			err, blocked := j.ctx.Err(), false
			if err == nil {
				err = p.ctx.Err()
			}
			if err == nil && p.persistErr != nil {
				err = p.persistErr
			}
			for _, depID := range j.dependencies {
				dep := p.jobs[depID]
				if dep.result.Status == "queued" || dep.result.Status == "running" {
					blocked = true
				} else if dep.result.Status != "completed" {
					err = fmt.Errorf("dependency %s did not complete", depID)
				}
			}
			if err != nil {
				p.finishQueuedLocked(j, err)
				removed = true
				continue
			}
			if blocked || len(p.slots) >= p.workerLimitLocked() {
				remaining = append(remaining, id)
				continue
			}
			p.slots <- struct{}{}
			j.result.Status = "running"
			started := time.Now()
			j.result.StartedAt = started.UTC().Format(time.RFC3339Nano)
			p.parent.mu.Lock()
			if queuedAt, err := time.Parse(time.RFC3339Nano, j.result.CreatedAt); err == nil {
				p.parent.appendOrchestrationEventLocked(p.task, "queue_wait", "environment", queuedAt, started, "admitted")
			}
			p.task.Environments.Active++
			p.task.Environments.Peak = max(p.task.Environments.Peak, p.task.Environments.Active)
			p.parent.mu.Unlock()
			p.notifyQueueLocked()
			go func() { defer p.wg.Done(); p.run(j) }()
		}
		p.queue = remaining
		// A failed forward dependency may precede its dependant in this batch.
		if !removed || len(p.queue) == 0 {
			return
		}
	}
}

func (p *executionPool) cancelJob(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	j := p.jobs[id]
	if j == nil {
		return errors.New("unknown environment")
	}
	j.cancel()
	p.dispatchReadyLocked()
	p.publishLocked()
	return nil
}

func (p *executionPool) orderedJobsLocked() []*executionJob {
	jobs := make([]*executionJob, 0, len(p.jobs))
	for _, j := range p.jobs {
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].result.CreatedAt != jobs[j].result.CreatedAt {
			return jobs[i].result.CreatedAt < jobs[j].result.CreatedAt
		}
		return jobs[i].result.ID < jobs[j].result.ID
	})
	return jobs
}

// Pagination limits presentation only, never accepted jobs or execution.
func (p *executionPool) queueStatus(after string, limit int) (map[string]any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if limit <= 0 {
		limit = 32
	}
	counts, rows := map[string]int{}, []map[string]any{}
	passed := after == ""
	more, last := false, ""
	for _, j := range p.orderedJobsLocked() {
		r := j.result
		counts[r.Status]++
		if !passed {
			passed = r.ID == after
			continue
		}
		if len(rows) == limit {
			more = true
			continue
		}
		rows = append(rows, map[string]any{"id": r.ID, "key": r.Key, "assignmentReference": clip(r.Assignment, 360), "status": r.Status, "dependsOn": r.DependsOn, "collected": r.Collected, "mergedAsPending": r.Merged})
		last = r.ID
	}
	if !passed {
		return nil, errors.New("unknown queue cursor")
	}
	if !more {
		last = ""
	}
	return map[string]any{"total": len(p.jobs), "counts": counts, "jobs": rows, "nextAfter": last, "workerCapacity": p.workerLimitLocked(), "active": len(p.slots), "mainPaused": p.draining, "randomReady": p.randomReady}, nil
}

// Wait for ANY completion, not a particular slow job. Conflicts stay explicit;
// one rejected merge must not discard successful results from other jobs.
func (p *executionPool) collectReady(ctx context.Context, wait, merge bool, limit int, versions map[string]Change) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 8
	}
	for {
		p.mu.Lock()
		ids, unfinished := []string{}, false
		for _, j := range p.orderedJobsLocked() {
			if j.result.Status == "running" || j.result.Status == "queued" {
				unfinished = true
			} else if !j.result.Collected && len(ids) < limit {
				ids = append(ids, j.result.ID)
			}
		}
		if p.changed == nil {
			p.changed = make(chan struct{})
		}
		changed := p.changed
		p.mu.Unlock()
		if len(ids) > 0 || !wait || !unfinished {
			out := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				r, err := p.collect(ctx, id, false, merge, versions)
				row := map[string]any{"result": compactExecutionResult(r)}
				if err != nil {
					row["error"] = err.Error()
				}
				out = append(out, row)
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		case <-changed:
		}
	}
}
