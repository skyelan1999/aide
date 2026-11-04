package server

import "testing"

func TestRuntimeMode(t *testing.T) {
	t.Setenv("AIDE_RUNTIME_MODE", "")
	if got := runtimeMode(); got != "container" {
		t.Fatalf("default runtime mode = %q, want container", got)
	}
	t.Setenv("AIDE_RUNTIME_MODE", " Native ")
	if got := runtimeMode(); got != "native" {
		t.Fatalf("native runtime mode = %q, want native", got)
	}
}
