//go:build linux || darwin || freebsd

package server

import (
	"golang.org/x/sys/unix"
	"os"
)

func transferAvailableBytes(root *os.Root) (int64, error) {
	f, e := root.Open(".")
	if e != nil {
		return 0, e
	}
	defer f.Close()
	var stat unix.Statfs_t
	if e = unix.Fstatfs(int(f.Fd()), &stat); e != nil {
		return 0, e
	}
	a, b := uint64(stat.Bavail), uint64(stat.Bsize)
	if b > 0 && a > uint64(^uint64(0)>>1)/b {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(a * b), nil
}
