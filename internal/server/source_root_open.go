package server

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

const localRootOpenTimeout = 350 * time.Millisecond
const localRootOpenLimit = maxSources + 4

var (
	errLocalRootPending  = errors.New("本机目录访问仍在等待，请检查目录或本机访问权限")
	errLocalRootTimeout  = errors.New("本机目录访问超时，请检查目录或本机访问权限")
	errLocalRootCapacity = errors.New("等待访问的本机目录过多，请先处理不可用来源")
)

type localRootOpening struct {
	started time.Time
	done    chan struct{}
}
type localRootOpenState struct {
	mu      sync.Mutex
	pending map[string]*localRootOpening
	open    func(string) (*os.Root, error) // Optional isolated test opener; immutable after first use.
}
type localRootResult struct {
	root *os.Root
	err  error
}

// A host-share open can wait inside the kernel after its request is cancelled.
// Only one such operation per path may remain, with a hard per-App admission
// limit. Do not add these workers to bgWg: Close cannot join an uncancellable OS
// call. Late handles are closed by their owner; no source binding is substituted.
func (a *App) openLocalSourceRoot(ctx context.Context, candidate string) (*os.Root, error) {
	bounded, cancel := context.WithTimeout(ctx, localRootOpenTimeout)
	defer cancel()
	var stopped <-chan struct{}
	if a.bgCtx != nil {
		stopped = a.bgCtx.Done()
	}
	state := &a.localRootOpens
	for {
		select {
		case <-stopped:
			return nil, context.Canceled
		default:
		}
		if err := bounded.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errLocalRootTimeout
		}
		state.mu.Lock()
		if state.pending == nil {
			state.pending = make(map[string]*localRootOpening)
		}
		if prior := state.pending[candidate]; prior != nil {
			state.mu.Unlock()
			if time.Since(prior.started) >= localRootOpenTimeout {
				return nil, errLocalRootPending
			}
			select {
			case <-prior.done:
				continue
			case <-stopped:
				return nil, context.Canceled
			case <-bounded.Done():
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errLocalRootTimeout
			}
		}
		if len(state.pending) >= localRootOpenLimit {
			state.mu.Unlock()
			return nil, errLocalRootCapacity
		}
		entry := &localRootOpening{started: time.Now(), done: make(chan struct{})}
		state.pending[candidate] = entry
		opener := state.open
		if opener == nil {
			opener = os.OpenRoot
		}
		state.mu.Unlock()
		result := make(chan localRootResult)
		go func() {
			defer func() { state.mu.Lock(); delete(state.pending, candidate); close(entry.done); state.mu.Unlock() }()
			root, err := opener(candidate)
			select {
			case result <- localRootResult{root, err}:
			case <-bounded.Done():
				if root != nil {
					root.Close()
				}
			case <-stopped:
				if root != nil {
					root.Close()
				}
			}
		}()
		select {
		case value := <-result:
			if bounded.Err() != nil {
				if value.root != nil {
					value.root.Close()
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errLocalRootTimeout
			}
			select {
			case <-stopped:
				if value.root != nil {
					value.root.Close()
				}
				return nil, context.Canceled
			default:
			}
			return value.root, value.err
		case <-stopped:
			return nil, context.Canceled
		case <-bounded.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errLocalRootTimeout
		}
	}
}

func localRootUnavailableMessage(err error) string {
	for _, known := range []error{errLocalRootPending, errLocalRootTimeout, errLocalRootCapacity} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "本地目录不可读取"
}
