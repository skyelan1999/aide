//go:build unix

package server

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestKnowledgeStartupUnavailableFIFOIsNeverOpened(t *testing.T) {
	a, work, _ := recoveredKnowledgeApp(t)
	if err := os.MkdirAll(filepath.Join(work, "missing"), 0700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(work, "missing", "docs")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := a.knowledgeCurrentGraph(context.Background(), false); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		// Unblock a regressed implementation before failing; do not leave a FIFO
		// reader or test goroutine behind for the rest of the full suite.
		fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("scan did not exit after FIFO was unblocked")
		}
		t.Fatal("unavailable descendant blocked first graph publication")
	}
}
