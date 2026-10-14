package server

import (
	"context"
	"sync"
)

// FIFO per workspace/plugin. No settings, credentials or argument data is kept.
// Unknown plugin effects are serialized because a separate node host is not a
// transaction boundary for shared plugin-data files.
type pluginCallWaiter struct{ ready chan struct{} }
type pluginCallQueues struct {
	mu     sync.Mutex
	queues map[string][]*pluginCallWaiter
}

func (q *pluginCallQueues) acquire(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &pluginCallWaiter{ready: make(chan struct{})}
	q.mu.Lock()
	if q.queues == nil {
		q.queues = make(map[string][]*pluginCallWaiter)
	}
	q.queues[key] = append(q.queues[key], w)
	if len(q.queues[key]) == 1 {
		close(w.ready)
	}
	q.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			items := q.queues[key]
			for i, item := range items {
				if item == w {
					items = append(items[:i], items[i+1:]...)
					if len(items) == 0 {
						delete(q.queues, key)
					} else {
						q.queues[key] = items
						if i == 0 {
							close(items[0].ready)
						}
					}
					break
				}
			}
		})
	}
	select {
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
}
