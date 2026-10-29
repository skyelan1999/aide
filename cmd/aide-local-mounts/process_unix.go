//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
)

func lockNativeLifecycle(context.Context, string) (func(), error) {
	return func() {}, nil
}

func platformProcessAlive(pid int) (bool, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, os.ErrPermission) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	return false, err
}

// Retain the existing POSIX boundary behavior.
func platformComposePath(path string) string { return path }
