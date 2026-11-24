//go:build linux

package server

import (
	"errors"
	"os"
	"path"

	"golang.org/x/sys/unix"
)

func transferRenameNoReplaceUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP)
}

func transferRenameExclusiveResult(root *os.Root, old, new string, err error) error {
	if transferRenameNoReplaceUnsupported(err) {
		// Docker Desktop bind mounts and some network filesystems reject
		// RENAME_NOREPLACE with EINVAL even though ordinary rename works.
		return transferRenameExclusiveFallback(root, old, new)
	}
	return err
}

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
	e = unix.Renameat2(int(a.Fd()), path.Base(old), int(b.Fd()), path.Base(new), unix.RENAME_NOREPLACE)
	return transferRenameExclusiveResult(root, old, new, e)
}
