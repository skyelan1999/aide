//go:build linux

package server

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestTransferRenameNoReplaceUnsupported(t *testing.T) {
	for _, err := range []error{unix.EINVAL, unix.ENOSYS, unix.ENOTSUP} {
		if !transferRenameNoReplaceUnsupported(err) {
			t.Fatalf("expected fallback for %v", err)
		}
	}
	for _, err := range []error{nil, unix.EEXIST, unix.EACCES, unix.EXDEV} {
		if transferRenameNoReplaceUnsupported(err) {
			t.Fatalf("unexpected fallback for %v", err)
		}
	}
}

func TestTransferRenameExclusiveResultFallsBackFromInvalidArgument(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile("stage", []byte("windows bind mount upload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = transferRenameExclusiveResult(root, "stage", "target", unix.EINVAL); err != nil {
		t.Fatal(err)
	}
	content, err := root.ReadFile("target")
	if err != nil || string(content) != "windows bind mount upload" {
		t.Fatalf("fallback published %q, %v", content, err)
	}
}

func TestTransferRenameExclusiveResultDoesNotMaskConflict(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	conflict := &os.LinkError{Op: "renameat2", Old: "stage", New: "target", Err: unix.EEXIST}
	if err = transferRenameExclusiveResult(root, "stage", "target", conflict); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("conflict error = %v", err)
	}
	if _, statErr := root.Lstat("target"); !os.IsNotExist(statErr) {
		t.Fatalf("conflict created a target: %v", statErr)
	}
}
