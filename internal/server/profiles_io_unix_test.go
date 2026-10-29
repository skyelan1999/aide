//go:build unix

package server

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestIssue147PolicyFIFODoesNotBlockAndUsesRegularFallback(t *testing.T) {
	a := testApp(t)
	fifo := filepath.Join(a.workPath, routingPolicyJSON)
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fifoHandle := os.NewFile(uintptr(fd), fifo)
	defer fifoHandle.Close()
	if err := os.WriteFile(filepath.Join(a.workPath, routingPolicyMD), []byte("```json\n{\"default\":\"precise\"}\n```"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan *routingPolicy, 1)
	go func() { done <- a.readRoutingPolicy() }()
	select {
	case got := <-done:
		if got == nil || got.Default != "precise" {
			t.Fatal("FIFO prevented regular fallback", got)
		}
	case <-time.After(time.Second):
		fifoHandle.Close() // Unblock a regressed reader before reporting failure.
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("routing attempted to read a FIFO")
	}
}
