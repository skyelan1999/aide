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
