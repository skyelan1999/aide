//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestClearDeadLockOwnerUnlinksPIDAfterFirstRead(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^$")
	if err = child.Run(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	if alive, err := processAlive(pid); err != nil || alive {
		t.Fatalf("owned child did not exit: %v %v", alive, err)
	}
	path := filepath.Join(t.TempDir(), "serve.pid")
	if err = syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		file, e := os.OpenFile(path, os.O_WRONLY, 0)
		if e != nil {
			written <- e
			return
		}
		if _, e = file.WriteString(strconv.Itoa(pid)); e == nil {
			e = os.Remove(path)
		}
		closeErr := file.Close()
		if e == nil {
			e = closeErr
		}
		written <- e
	}()
	// The FIFO returns the old PID, but its directory entry is already absent
	// before EOF. This deterministically reproduces an exiting owner's cleanup
	// racing with the second read; no user process or timing gamble is involved.
	cleared, err := clearDeadLock(path)
	if writeErr := <-written; writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil || !cleared {
		t.Fatalf("owner already removed PID: cleared=%v err=%v", cleared, err)
	}
}
