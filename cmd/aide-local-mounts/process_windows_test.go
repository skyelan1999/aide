package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNativeLifecycleLockExclusionAndRelease(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockNativeLifecycle(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, err := lockNativeLifecycle(ctx, root); err == nil {
		release()
		t.Fatal("second mutation entered an owned lifecycle lock")
	}
	unlock()
	unlock() // Deferred/repeated cleanup must not close a reused OS handle.
	release, err := lockNativeLifecycle(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestNativeProcessLifetimeWithoutTasklist(t *testing.T) {
	alive, err := platformProcessAlive(os.Getpid())
	if err != nil || !alive {
		t.Fatalf("current process: alive=%v err=%v", alive, err)
	}
	// PID zero is reserved and OpenProcess must not identify a live owner.
	alive, err = platformProcessAlive(0)
	if err != nil || alive {
		t.Fatalf("invalid process: alive=%v err=%v", alive, err)
	}
}

func TestNativeComposeBoundaryKeepsLiteralDollars(t *testing.T) {
	source := `C:\Users\cost$$money\$${USER}\$$$$literal`
	want := `C:\Users\cost$money\${USER}\$$literal`
	if got := platformComposePath(source); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
