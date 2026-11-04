package server

import "runtime"

// hostShellCommand resolves the POSIX compatibility shell bundled with the
// native Windows runtime (MinGit) through PATH. Unix keeps its system shell.
func hostShellCommand() string {
	return hostShellCommandForOS(runtime.GOOS)
}

func hostShellCommandForOS(goos string) string {
	if goos == "windows" {
		return "sh"
	}
	return "/bin/sh"
}
