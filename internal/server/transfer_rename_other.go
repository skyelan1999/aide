//go:build !linux && !darwin

package server

import (
	"errors"
	"os"
	"runtime"
)

func transferRenameExclusive(root *os.Root, old, new string) error {
	info, e := root.Stat(old)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		if e = root.Link(old, new); e != nil {
			return e
		}
		return root.Remove(old)
	}
	if runtime.GOOS != "windows" {
		return errors.New("此平台尚不支持目录的原子无覆盖发布")
	}
	if _, e = root.Lstat(new); e == nil {
		return os.ErrExist
	} else if !os.IsNotExist(e) {
		return e
	}
	return root.Rename(old, new)
}
