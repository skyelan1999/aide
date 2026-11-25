//go:build linux

package server

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestAvailableBytesUsesFilesystemFragmentSize(t *testing.T) {
	// Docker Desktop VirtioFS can report a 1 MiB allocation block and a 4 KiB
	// fundamental fragment. Using Bsize here inflated free space 256-fold.
	got := availableBytesFromStatfs(unix.Statfs_t{Bsize: 1048576, Frsize: 4096, Bavail: 59600000})
	want := int64(59600000 * 4096)
	if got != want {
		t.Fatalf("available bytes = %d, want %d", got, want)
	}
}
