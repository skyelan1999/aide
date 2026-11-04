//go:build !windows

package server

import (
	"os"
	"syscall"
)

func isHardlinked(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
