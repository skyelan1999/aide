//go:build !linux && !darwin && !freebsd && !windows

package server

import "os"

func transferAvailableBytes(root *os.Root) (int64, error) { return -1, nil }
