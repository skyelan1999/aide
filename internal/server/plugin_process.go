package server

import (
	"os/exec"
	"syscall"
	"time"
)

// A handler can launch Python or another child. Ending only its Node parent
// must not let a late child writer outlive the resource reservation.
func configurePluginProcess(cmd *exec.Cmd, cancellable bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	if cancellable {
		cmd.Cancel = func() error { return killPluginProcess(cmd) }
	}
}

func killPluginProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
