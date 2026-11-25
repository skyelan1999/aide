//go:build darwin || freebsd

package server

import "golang.org/x/sys/unix"

// These platforms report the allocation block used for capacity in Bsize.
func transferFragmentSize(stat unix.Statfs_t) uint64 { return 0 }
