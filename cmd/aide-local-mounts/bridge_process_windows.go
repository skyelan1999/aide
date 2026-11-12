package main

import (
	"os/exec"
	"syscall"
)

// Retain the resident native host job; never escape its owned-process boundary.
func detachBridge(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000}
}
