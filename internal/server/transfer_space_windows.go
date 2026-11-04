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

func filesystemFreeBytes(path string) uint64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var free uint64
	if windows.GetDiskFreeSpaceEx(p, &free, nil, nil) != nil {
		return 0
	}
	return free
}
