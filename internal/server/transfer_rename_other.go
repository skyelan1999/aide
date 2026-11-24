//go:build !linux && !darwin

package server

import (
	"os"
)

func transferRenameExclusive(root *os.Root, old, new string) error {
	info, e := root.Stat(old)
	if e != nil {
		return e
	}
	if info.IsDir() {
		return transferRenameExclusiveFallback(root, old, new)
	}
	if e = root.Link(old, new); e == nil {
		return root.Remove(old)
	}
	if os.IsExist(e) {
		return e
	}
	// FAT/exFAT and some Windows filesystem filters cannot create hard links.
	// Preserve the no-overwrite contract with an exclusive destination marker.
	return transferRenameExclusiveFallback(root, old, new)
}
