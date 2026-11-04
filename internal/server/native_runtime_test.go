package server

import "testing"

func TestNativeLoopbackHTTPRequiresExplicitFlagAndLoopbackAddress(t *testing.T) {
	t.Setenv("AIDE_NATIVE_LOCAL_HTTP", "")
	if nativeLoopbackHTTPEnabled("127.0.0.1:8097") {
		t.Fatal("loopback HTTP enabled without explicit opt-in")
	}
	t.Setenv("AIDE_NATIVE_LOCAL_HTTP", "1")
	for _, addr := range []string{"127.0.0.1:8097", "[::1]:8097", "localhost:8097"} {
		if !nativeLoopbackHTTPEnabled(addr) {
			t.Errorf("expected local HTTP for %q", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8097", "192.168.1.9:8097", "aide.local:8097", "invalid"} {
		if nativeLoopbackHTTPEnabled(addr) {
			t.Errorf("local HTTP must reject non-loopback address %q", addr)
		}
	}
}
