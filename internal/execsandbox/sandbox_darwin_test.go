//go:build darwin

package execsandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func darwinFixture(t *testing.T) (string, string) {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	for _, path := range []string{workspace, outside} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, outside
}

func runDarwinFixture(t *testing.T, mode, workspace, command string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, cleanup, err := Command(ctx, mode, workspace, command)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestDarwinSandboxWorkspaceWriteAndReadOnly(t *testing.T) {
	workspace, outside := darwinFixture(t)
	t.Run("workspace-write", func(t *testing.T) {
		out, err := runDarwinFixture(t, "workspace-write", workspace, `echo allowed > inside.txt && cat inside.txt`)
		if err != nil || strings.TrimSpace(out) != "allowed" {
			t.Fatalf("workspace write failed: %q %v", out, err)
		}
	})
	t.Run("read-only", func(t *testing.T) {
		out, err := runDarwinFixture(t, "read-only", workspace, `echo denied > readonly.txt`)
		if err == nil {
			t.Fatalf("read-only write succeeded: %q", out)
		}
		if _, statErr := os.Stat(filepath.Join(workspace, "readonly.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("read-only command changed workspace: %v", statErr)
		}
	})
	t.Run("outside-write", func(t *testing.T) {
		target := filepath.Join(outside, "outside.txt")
		out, err := runDarwinFixture(t, "workspace-write", workspace, "echo denied > "+darwinShellQuote(target))
		if err == nil {
			t.Fatalf("outside write succeeded: %q", out)
		}
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("outside file was created: %v", statErr)
		}
	})
	t.Run("outside-read", func(t *testing.T) {
		target := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := runDarwinFixture(t, "workspace-write", workspace, "cat "+darwinShellQuote(target))
		if err == nil || strings.TrimSpace(out) == "secret" {
			t.Fatalf("outside file was readable: %q %v", out, err)
		}
	})
}

func TestDarwinSandboxBundledRuntimeAndPrivateTemp(t *testing.T) {
	workspace, _ := darwinFixture(t)
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheRoot, 0700); err != nil {
		t.Fatal(err)
	}
	runtimeRoot, err := os.MkdirTemp(cacheRoot, "aide-sandbox-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	outsideFile, err := os.CreateTemp(cacheRoot, "aide-sandbox-outside-")
	if err != nil {
		t.Fatal(err)
	}
	outsidePath := outsideFile.Name()
	if _, err := outsideFile.WriteString("forbidden-value"); err != nil {
		outsideFile.Close()
		t.Fatal(err)
	}
	if err := outsideFile.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outsidePath) })
	bin := filepath.Join(runtimeRoot, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(bin, "aide-runtime-fixture")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'bundled-runtime-ok\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_NATIVE_RUNTIME_ROOT", runtimeRoot)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	out, err := runDarwinFixture(t, "workspace-write", workspace, `aide-runtime-fixture && echo scratch > "$TMPDIR/scratch" && cat "$TMPDIR/scratch"`)
	if err != nil || !strings.Contains(out, "bundled-runtime-ok") || !strings.Contains(out, "scratch") {
		t.Fatalf("runtime or private temp is unavailable: %q %v", out, err)
	}
	out, err = runDarwinFixture(t, "workspace-write", workspace, "cat "+darwinShellQuote(outsidePath))
	if err == nil || strings.Contains(out, "forbidden-value") {
		t.Fatalf("runtime path permission exposed a sibling file: %q %v", out, err)
	}
}
