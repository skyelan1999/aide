package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIssue92ReplacedMasterPinsReapOnlyDeadListeners(t *testing.T) {
	root := t.TempDir()
	socket := filepath.Join(root, "master.sock")
	old, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	livePin, err := pinTerminalSSHMaster(root, socket)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(livePin)
	t.Cleanup(func() { os.RemoveAll(dir) }) // only this test's hash-owned temp folder
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := pinTerminalSSHMaster(root, socket)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Errorf("restart %d retained %d entries, want live old and current only", i, len(entries))
		}
		if _, err := os.Lstat(livePin); err != nil {
			t.Fatal("live old pin removed", err)
		}
		conn, err := net.Dial("unix", livePin)
		if err != nil {
			t.Fatal("live old master lost", err)
		}
		conn.Close()
		listener.Close()
		if _, err := os.Lstat(pin); err != nil {
			t.Fatal("test needs stale linked socket", err)
		}
	}
}

func TestIssue92AppOwnsPinsAndShutdownPreservesOtherOwners(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "master.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a, b := testApp(t), testApp(t)
	first, err := a.pinTerminalMaster(socket)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.pinTerminalMaster(socket)
	if err != nil || again != first {
		t.Fatalf("same inode not reused: %q %q %v", first, again, err)
	}
	other, err := b.pinTerminalMaster(socket)
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("apps shared pin ownership")
	}
	a.Close()
	if _, err := os.Lstat(filepath.Dir(first)); !os.IsNotExist(err) {
		t.Fatal("shutdown retained owned directory", err)
	}
	if _, err := os.Lstat(other); err != nil {
		t.Fatal("shutdown removed other owner's live pin", err)
	}
	conn, err := net.Dial("unix", other)
	if err != nil {
		t.Fatal("other live session lost", err)
	}
	conn.Close()
	if _, err := a.pinTerminalMaster(socket); err == nil {
		t.Fatal("shutdown admitted late pin")
	}
	b.Close()
	if _, err := os.Lstat(filepath.Dir(other)); !os.IsNotExist(err) {
		t.Fatal("second shutdown retained owned directory", err)
	}
}

func TestIssue92ReaperPreservesUnknownAndReplacedPaths(t *testing.T) {
	a := testApp(t)
	socket := filepath.Join(t.TempDir(), "master.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := a.pinTerminalMaster(socket)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	dir := filepath.Dir(pin)
	unknown := filepath.Join(dir, "do-not-delete.txt")
	if err := os.WriteFile(unknown, []byte("owned-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reapTerminalMasterPins(ctx, dir, a.terminalPins.info, false)
	if _, err := os.Lstat(pin); err != nil {
		t.Fatal("cancelled sweep removed pin", err)
	}
	foreignInfo, err := os.Lstat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reapTerminalMasterPins(context.Background(), dir, foreignInfo, false)
	if _, err := os.Lstat(pin); err != nil {
		t.Fatal("mismatched directory identity accepted", err)
	}
	reapTerminalMasterPins(context.Background(), dir, a.terminalPins.info, false)
	if _, err := os.Lstat(pin); !os.IsNotExist(err) {
		t.Fatal("dead pin retained", err)
	}
	a.Close()
	if _, err := os.Stat(unknown); err != nil {
		t.Fatal("unknown file removed", err)
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
}

func TestIssue92ConcurrentPinShutdownHasNoLateLinks(t *testing.T) {
	a := testApp(t)
	socket := filepath.Join(t.TempDir(), "master.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	first, err := a.pinTerminalMaster(socket)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 8; j++ {
				_, _ = a.pinTerminalMaster(socket)
			}
		}()
	}
	close(start)
	a.Close()
	workers.Wait()
	if _, err := os.Lstat(filepath.Dir(first)); !os.IsNotExist(err) {
		t.Fatal("concurrent pin escaped shutdown", err)
	}
}
