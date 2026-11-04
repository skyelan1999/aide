package server

import (
	"strings"
	"testing"
)

func TestWindowsSSHArgumentsDoNotUseControlMasterSockets(t *testing.T) {
	args := strings.Join(sshConnectionArgsForOS("windows", `C:\Temp\aide.sock`, 2202, `C:\Users\tester`), " ")
	for _, forbidden := range []string{"ControlPath=", "ControlMaster", "C:\\Temp\\aide.sock"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("Windows SSH arguments contain unsupported control socket option %q: %s", forbidden, args)
		}
	}
	for _, required := range []string{"UserKnownHostsFile=C:\\Users\\tester\\.ssh\\known_hosts", "-p 2202", "StrictHostKeyChecking=accept-new"} {
		if !strings.Contains(args, required) {
			t.Fatalf("Windows SSH arguments missing %q: %s", required, args)
		}
	}
	if got := sshControlPrefixForOS("windows", "ignored"); len(got) != 0 {
		t.Fatalf("Windows SSH command has a control prefix: %#v", got)
	}
	if got := sshControlOperationForOS("windows", "ignored", "check"); len(got) != 0 {
		t.Fatalf("Windows SSH command has a control operation: %#v", got)
	}
	if got := strings.Join(sshSFTPConnectionArgsForOS("windows", "ignored", 2202), " "); strings.Contains(got, "ControlPath=") || !strings.Contains(got, "-P 2202") {
		t.Fatalf("Windows SFTP arguments are invalid: %s", got)
	}
	identity := strings.Join(appendWindowsSSHIdentity(nil, `C:\Users\tester\.ssh\work_key`), " ")
	if identity != `-i C:\Users\tester\.ssh\work_key -o IdentitiesOnly=yes` {
		t.Fatalf("Windows referenced identity was not attached: %s", identity)
	}
}

func TestUnixSSHArgumentsRetainConnectionMultiplexing(t *testing.T) {
	args := strings.Join(sshConnectionArgsForOS("linux", "/tmp/aide.sock", 2222, "/home/aide"), " ")
	if !strings.Contains(args, "ControlPath=/tmp/aide.sock") || !strings.Contains(args, "UserKnownHostsFile=/home/aide/.ssh/known_hosts") {
		t.Fatalf("Unix SSH arguments lost existing behavior: %s", args)
	}
	if got := strings.Join(sshControlPrefixForOS("linux", "/tmp/aide.sock"), " "); got != "-S /tmp/aide.sock" {
		t.Fatalf("unexpected Unix SSH control prefix: %s", got)
	}
}

func TestSSHAgentCapabilityIsScopedToSSHClient(t *testing.T) {
	t.Setenv("AIDE_RUNTIME_MODE", "native")
	t.Setenv("SSH_AUTH_SOCK", `\\.\pipe\openssh-ssh-agent`)
	if got := strings.Join(aideRuntimeEnvironmentForOS("windows"), "\n"); strings.Contains(got, "SSH_AUTH_SOCK") {
		t.Fatalf("SSH agent capability leaked into general child environment: %s", got)
	}
	if got := strings.Join(sshClientEnvironment(), "\n"); !strings.Contains(got, `SSH_AUTH_SOCK=\\.\pipe\openssh-ssh-agent`) {
		t.Fatalf("SSH client did not receive configured agent socket: %s", got)
	}
}

func TestHostShellUsesBundledWindowsCompatibilityShell(t *testing.T) {
	if got := hostShellCommandForOS("windows"); got != "sh" {
		t.Fatalf("Windows native runtime must resolve bundled MinGit sh from PATH, got %q", got)
	}
	if got := hostShellCommandForOS("linux"); got != "/bin/sh" {
		t.Fatalf("Linux shell changed unexpectedly: %q", got)
	}
}
