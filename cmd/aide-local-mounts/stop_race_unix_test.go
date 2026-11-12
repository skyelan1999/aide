//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestStalePIDReuseDoesNotOwnHelperLock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "serve.pid")
	// An unrelated live process with the recorded PID must not retain stale ownership.
	if err := os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cleared, err := clearDeadLock(p); err != nil || cleared {
				t.Errorf("removed replacement owner's receipt: %v %v", cleared, err)
			}
		}()
	}
	wg.Wait()
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	release()
	cleared, err := clearDeadLock(p)
	if err != nil || !cleared {
		t.Fatalf("kernel lock was not released: %v %v", cleared, err)
	}
	// The persistent guard must survive cleanup: unlinking it would split ownership.
	if _, err := os.Stat(filepath.Join(dir, "serve.guard")); err != nil {
		t.Fatal(err)
	}
}
