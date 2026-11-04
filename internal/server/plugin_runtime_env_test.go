package server

import (
	"strings"
	"testing"
)

func TestPluginHostEnvironmentKeepsSecretsOutAndUsesNativeRuntime(t *testing.T) {
	t.Setenv("AIDE_RUNTIME_MODE", "native")
	t.Setenv("PATH", "/Applications/Aide.app/Contents/Resources/runtime/bin:/usr/bin")
	t.Setenv("HOME", "/Users/test/Library/Application Support/Aide")
	t.Setenv("TMPDIR", "/Users/test/Library/Caches/Aide")
	t.Setenv("PYTHONPATH", "/Applications/Aide.app/Contents/Resources/runtime/python/lib/python3.12/site-packages")
	t.Setenv("AI_API_KEY", "must-not-leak")
	t.Setenv("AIDE_COMPUTER_BRIDGE_TOKEN", "must-not-leak-either")
	got := pluginHostEnvironment()
	joined := strings.Join(got, "\n")
	for _, want := range []string{"PATH=/Applications/Aide.app/Contents/Resources/runtime/bin:/usr/bin", "HOME=/Users/test/Library/Application Support/Aide", "TMPDIR=/Users/test/Library/Caches/Aide", "PYTHONPATH=/Applications/Aide.app/Contents/Resources/runtime/python/lib/python3.12/site-packages"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("native runtime env missing %q: %q", want, got)
		}
	}
	for _, secret := range []string{"AI_API_KEY", "must-not-leak", "AIDE_COMPUTER_BRIDGE_TOKEN"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("secret entered plugin environment: %q", got)
		}
	}
}

func TestPluginHostEnvironmentKeepsContainerDefaults(t *testing.T) {
	t.Setenv("AIDE_RUNTIME_MODE", "release-image")
	t.Setenv("PATH", "/custom/host/path")
	t.Setenv("HOME", "/Users/test")
	got := strings.Join(pluginHostEnvironment(), "\n")
	if !strings.Contains(got, "PATH=/usr/local/bin:/usr/bin:/bin") || !strings.Contains(got, "HOME=/home/aide") {
		t.Fatalf("container runtime defaults changed: %q", got)
	}
}

func TestNativeWindowsRuntimeEnvironmentKeepsSystemPathsWithoutSecrets(t *testing.T) {
	t.Setenv("AIDE_RUNTIME_MODE", "native")
	t.Setenv("PATH", `C:\Aide\runtime\node;C:\Windows\System32`)
	t.Setenv("HOME", `C:\Users\test\AppData\Local\Aide`)
	t.Setenv("SYSTEMROOT", `C:\Windows`)
	t.Setenv("WINDIR", `C:\Windows`)
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("USERPROFILE", `C:\Users\test`)
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	t.Setenv("SYSTEMDRIVE", "C:")
	t.Setenv("AI_API_KEY", "must-not-leak")
	got := strings.Join(aideRuntimeEnvironmentForOS("windows"), "\n")
	for _, want := range []string{`PATH=C:\Aide\runtime\node;C:\Windows\System32`, `SYSTEMROOT=C:\Windows`, `COMSPEC=C:\Windows\System32\cmd.exe`, `USERPROFILE=C:\Users\test`, `LOCALAPPDATA=C:\Users\test\AppData\Local`, `PROGRAMDATA=C:\ProgramData`} {
		if !strings.Contains(got, want) {
			t.Fatalf("Windows runtime env missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "AI_API_KEY") || strings.Contains(got, "must-not-leak") || strings.Contains(got, "SSH_AUTH_SOCK") {
		t.Fatalf("secret entered Windows plugin environment: %q", got)
	}
}
