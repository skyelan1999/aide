//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func detachBridge(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
