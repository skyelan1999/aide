//go:build linux

package server

import (
	"bytes"
	"context"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
)

func executePWImage(ctx context.Context, image, input []byte) ([]byte, error) {
	fd, err := unix.MemfdCreate("aide-pw-runtime", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "aide-pw-runtime")
	defer f.Close()
	if _, err = f.Write(image); err != nil {
		return nil, err
	}
	if err = f.Chmod(0500); err != nil {
		return nil, err
	}
	if _, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	output := &pwBoundedOutput{limit: pwRuntimeMaxOutput}
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=/nonexistent"}
	cmd.WaitDelay = 500000000
	if err = cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
