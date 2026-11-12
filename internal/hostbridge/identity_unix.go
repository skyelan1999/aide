//go:build !windows

package hostbridge

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func Identity(root *os.Root) (string, error) {
	info, e := root.Stat(".")
	if e != nil {
		return "", e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("directory identity unavailable")
	}
	return fmt.Sprintf("unix:%x:%x", uint64(st.Dev), uint64(st.Ino)), nil
}
