package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Delay before the bounded context is installed, after WithTimeout has chosen
// its deadline. This models a scheduling/admission delay without depending on
// whether a timer happens to fire just before or after another goroutine runs.
type delayedRootDeadlineContext struct {
	context.Context
	delay time.Duration
}

func (c delayedRootDeadlineContext) Deadline() (time.Time, bool) {
	time.Sleep(c.delay)
	return c.Context.Deadline()
}

func TestLocalRootOpenPendingUsesInitiatingDeadline(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	a.localRootOpens.open = func(string) (*os.Root, error) {
		calls.Add(1)
		<-release
		return nil, os.ErrNotExist
	}
	ctx := delayedRootDeadlineContext{Context: context.Background(), delay: localRootOpenTimeout / 3}
	if root, err := a.openLocalSourceRoot(ctx, "blocked-root"); root != nil || !errors.Is(err, errLocalRootTimeout) {
		t.Fatalf("initial open: root=%v err=%v", root, err)
	}
	if root, err := a.openLocalSourceRoot(context.Background(), "blocked-root"); root != nil || !errors.Is(err, errLocalRootPending) {
		t.Fatalf("retry after initiating deadline must be pending: root=%v err=%v", root, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("pending retry started another opener: %d", calls.Load())
	}
}

func TestLocalRootOpenPendingUsesShortCallerDeadline(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	a.localRootOpens.open = func(string) (*os.Root, error) {
		<-release
		return nil, os.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(context.Background(), localRootOpenTimeout/4)
	defer cancel()
	if _, err := a.openLocalSourceRoot(ctx, "blocked-root"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("initial caller deadline was not preserved: %v", err)
	}
	if _, err := a.openLocalSourceRoot(context.Background(), "blocked-root"); !errors.Is(err, errLocalRootPending) {
		t.Fatalf("retry after caller deadline must be pending: %v", err)
	}
}

func TestLocalRootOpenTimeoutDeduplicatesAndClosesLateHandle(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	path := t.TempDir()
	var calls atomic.Int32
	opened := make(chan *os.Root, 2)
	a.localRootOpens.open = func(p string) (*os.Root, error) {
		calls.Add(1)
		<-release
		r, e := os.OpenRoot(p)
		if r != nil {
			opened <- r
		}
		return r, e
	}
	if r, err := a.openLocalSourceRoot(context.Background(), path); !errors.Is(err, errLocalRootTimeout) || r != nil {
		t.Fatal("blocked open did not time out", err)
	}
	a.localRootOpens.mu.Lock()
	entry := a.localRootOpens.pending[path]
	a.localRootOpens.mu.Unlock()
	started := time.Now()
	for n := 0; n < 20; n++ {
		if _, err := a.openLocalSourceRoot(context.Background(), path); !errors.Is(err, errLocalRootPending) {
			t.Fatal("pending open retried", err)
		}
	}
	if calls.Load() != 1 || time.Since(started) > 100*time.Millisecond {
		t.Fatal("retries spawned operations or waited again", calls.Load())
	}
	once.Do(func() { close(release) })
	select {
	case <-entry.done:
	case <-time.After(time.Second):
		t.Fatal("late open was not reaped")
	}
	late := <-opened
	if _, err := late.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatal("abandoned root handle leaked", err)
	}
	root, err := a.openLocalSourceRoot(context.Background(), path)
	if err != nil {
		t.Fatal("completed slot not reusable", err)
	}
	root.Close()
	if calls.Load() != 2 {
		t.Fatal("fresh open missing")
	}
}

func TestLocalRootOpenAdmissionAndShutdownAreBounded(t *testing.T) {
	a := testApp(t)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	entered := make(chan struct{}, localRootOpenLimit)
	a.localRootOpens.open = func(string) (*os.Root, error) { entered <- struct{}{}; <-release; return nil, os.ErrNotExist }
	results := make(chan error, localRootOpenLimit)
	for i := 0; i < localRootOpenLimit; i++ {
		go func(i int) {
			_, err := a.openLocalSourceRoot(context.Background(), filepath.Join(t.TempDir(), "root"))
			results <- err
		}(i)
	}
	for i := 0; i < localRootOpenLimit; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("open admission stalled")
		}
	}
	if _, err := a.openLocalSourceRoot(context.Background(), "another-root"); !errors.Is(err, errLocalRootCapacity) {
		t.Fatal("missing global admission cap", err)
	}
	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for blocked operating-system calls")
	}
	for i := 0; i < localRootOpenLimit; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("shutdown did not cancel caller", err)
			}
		case <-time.After(time.Second):
			t.Fatal("open caller survived shutdown")
		}
	}
	once.Do(func() { close(release) })
}

func TestLocalRootOpenConcurrentReadersKeepIndependentHandles(t *testing.T) {
	a := &App{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	errorsOut := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			r, err := a.openLocalSourceRoot(context.Background(), dir)
			if err == nil {
				_, err = r.ReadFile("a.txt")
				r.Close()
			}
			errorsOut <- err
		}()
	}
	for i := 0; i < 20; i++ {
		if err := <-errorsOut; err != nil {
			t.Fatal("normal concurrent source open failed", err)
		}
	}
}
