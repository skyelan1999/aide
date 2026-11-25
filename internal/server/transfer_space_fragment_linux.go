//go:build linux

package server

import "golang.org/x/sys/unix"

func transferFragmentSize(stat unix.Statfs_t) uint64 { return uint64(stat.Frsize) }
