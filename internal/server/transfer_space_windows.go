//go:build windows

package server

import (
	"golang.org/x/sys/windows"
	"os"
)

func transferAvailableBytes(root *os.Root) (int64, error) {
	p, e := windows.UTF16PtrFromString(root.Name())
	if e != nil {
		return 0, e
	}
	var free uint64
	if e = windows.GetDiskFreeSpaceEx(p, &free, nil, nil); e != nil {
		return 0, e
	}
	if free > uint64(^uint64(0)>>1) {
		free = uint64(^uint64(0) >> 1)
	}
	return int64(free), nil
}
