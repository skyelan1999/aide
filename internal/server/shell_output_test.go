package server

import (
	"strings"
	"testing"
)

func TestCappedShellOutputBoundsCollection(t *testing.T) {
	var out cappedShellOutput
	chunk := strings.Repeat("x", 1<<20)
	n, err := out.Write([]byte(chunk))
	if err != nil || n != len(chunk) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(chunk))
	}
	if out.Len() != 64<<10 {
		t.Fatalf("retained %d bytes, want 64 KiB", out.Len())
	}
	if !out.truncated || !strings.Contains(out.result("stdout"), "stdout 已截断") {
		t.Fatal("truncated output was not reported")
	}
}

func TestCappedShellOutputExactLimitIsNotTruncated(t *testing.T) {
	var out cappedShellOutput
	if _, err := out.Write([]byte(strings.Repeat("x", 64<<10))); err != nil {
		t.Fatal(err)
	}
	if out.truncated || strings.Contains(out.result("stderr"), "已截断") {
		t.Fatal("exact-limit output should not be marked truncated")
	}
}
