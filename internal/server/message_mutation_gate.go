package server

import (
	"context"
	"errors"
	"sync"
)

// Lock transitions cancel admitted message requests, then wait for their
// handlers to finish before publishing a new generation. The application
// mutex is never held while waiting for network operations to exit.
type messageMutationGate struct {
	mu      sync.Mutex
	writer  sync.Mutex
	barrier sync.RWMutex
	closing bool
	active  map[*messageMutationLease]context.CancelFunc
}

type messageMutationLease struct{ reserved byte }

func (a *App) beginMessageMutation(parent context.Context, generation int64) (context.Context, func(), error) {
	g := &a.messageMutations
	g.mu.Lock()
	if g.closing || !g.barrier.TryRLock() {
		g.mu.Unlock()
		return nil, nil, errors.New("锁屏状态已变化，请先解锁工作台")
	}
	ctx, cancel := context.WithCancel(parent)
	lease := &messageMutationLease{}
	if g.active == nil {
		g.active = make(map[*messageMutationLease]context.CancelFunc)
	}
	g.active[lease] = cancel
	g.mu.Unlock()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			g.mu.Lock()
			delete(g.active, lease)
			g.barrier.RUnlock()
			g.mu.Unlock()
		})
	}
	current := a.readLockState()
	if current.Locked || current.Gen != generation || ctx.Err() != nil {
		finish()
		return nil, nil, errors.New("锁屏状态已变化，请先解锁工作台")
	}
	return ctx, finish, nil
}

func (g *messageMutationGate) beginTransition() func() {
	g.writer.Lock()
	g.mu.Lock()
	g.closing = true
	for _, cancel := range g.active {
		cancel()
	}
	g.mu.Unlock()
	g.barrier.Lock()
	return func() {
		g.mu.Lock()
		g.barrier.Unlock()
		g.closing = false
		g.mu.Unlock()
		g.writer.Unlock()
	}
}
