//go:build linux

package server

import (
	"golang.org/x/sys/unix"
	"os"
	"path"
)

func transferRenameExclusive(root *os.Root, old, new string) error {
	a, e := root.Open(path.Dir(old))
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := root.Open(path.Dir(new))
	if e != nil {
		return e
	}
	defer b.Close()
	return unix.Renameat2(int(a.Fd()), path.Base(old), int(b.Fd()), path.Base(new), unix.RENAME_NOREPLACE)
}
