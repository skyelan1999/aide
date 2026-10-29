package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func lockNativeLifecycle(ctx context.Context, root string) (func(), error) {
	directory := filepath.Join(root, ".aide-data", "native")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "startup.lock")
	for _, item := range []string{directory, path} {
		name, err := windows.UTF16PtrFromString(item)
		if err != nil {
			return nil, err
		}
		attributes, err := windows.GetFileAttributes(name)
		if err == nil && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, errors.New("native lifecycle lock cannot be a link or junction")
		}
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err == nil {
			var info windows.ByHandleFileInformation
			if err = windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				windows.CloseHandle(handle)
				return nil, errors.New("native lifecycle handle is unreadable or a reparse point")
			}
			var once sync.Once
			return func() { once.Do(func() { windows.CloseHandle(handle) }) }, nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("another native lifecycle task remained busy")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func platformProcessAlive(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// Refuse stale-lock cleanup when a protected process could own the PID.
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	if status == windows.WAIT_OBJECT_0 {
		return false, nil
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return true, nil
	}
	return false, errors.New("cannot determine native Windows process lifetime")
}

// Compose config escapes literal dollars for serialization.
func platformComposePath(path string) string { return strings.ReplaceAll(path, "$$", "$") }
