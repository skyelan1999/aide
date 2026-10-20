//go:build darwin

package server

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
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
	return unix.RenameatxNp(int(a.Fd()), path.Base(old), int(b.Fd()), path.Base(new), unix.RENAME_EXCL)
}
