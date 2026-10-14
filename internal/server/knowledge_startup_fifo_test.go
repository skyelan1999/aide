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

func TestKnowledgeIndependentBlockedSourceDoesNotBlockOtherSources(t *testing.T) {
	a := testApp(t)
	fifo := filepath.Join(a.workPath, ".blocked-root")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "visible.md"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "blocked", Name: "blocked", Type: "local", Enabled: true, Config: SourceConfig{Path: ".blocked-root"}})
	defer func() {
		fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		defer syscall.Close(fd)
		a.localRootOpens.mu.Lock()
		entry := a.localRootOpens.pending[fifo]
		a.localRootOpens.mu.Unlock()
		if entry != nil {
			select {
			case <-entry.done:
			case <-time.After(time.Second):
				t.Error("blocked FIFO worker not reaped")
			}
		}
	}()
	start := time.Now()
	g, err := a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("blocked source prevented prompt first publication")
	}
	visible, unavailable := false, false
	for _, n := range g.Nodes {
		if n.Root == "workspace" && n.Path == "visible.md" {
			visible = true
		}
	}
	for _, s := range g.Sources {
		if s.ID == "blocked" {
			unavailable = s.State == "unavailable" && s.Message == errLocalRootTimeout.Error()
		}
	}
	if !visible || !unavailable {
		t.Fatalf("independent source isolation failed: visible=%v unavailable=%v", visible, unavailable)
	}
	start = time.Now()
	_, err = a.knowledgeCurrentGraph(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("pending source waited again instead of retaining one bounded operation")
	}
}
