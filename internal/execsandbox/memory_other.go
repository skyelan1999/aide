//go:build !linux

package execsandbox

import "os/exec"

func RunBounded(cmd *exec.Cmd, memoryMiB int) error {
	// Preserve existing native full-access execution. The advertised RSS guard
	// applies only to the Linux workbench; platform sandbox policy is unchanged.
	return cmd.Run()
}
