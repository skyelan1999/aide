package server

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestOpenRuntimeLocalRootUsesHostHomeInNativeMode(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "Library"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_RUNTIME_MODE", "native")
	root, err := openRuntimeLocalRoot(work, home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if root.Name() != home {
		t.Fatalf("native /local root = %q, want host home %q", root.Name(), home)
	}
	if _, err := root.Stat("Library"); err != nil {
		t.Fatalf("host path should resolve under native /local root: %v", err)
	}
}

func TestOpenHostRootResolvesVirtualLocalPath(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(home, "Projects", "Aide")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a := &App{localRoot: root}
	opened, err := a.openHostRoot("/local/Projects/Aide")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if opened.Name() != workspace {
		t.Fatalf("opened path = %q, want %q", opened.Name(), workspace)
	}
	if _, err := a.openHostRoot("/local/../outside"); err == nil {
		t.Fatal("accepted path outside the local root")
	}
}

func TestOpenRuntimeLocalRootUsesMountedLocalInContainerMode(t *testing.T) {
	local, work := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(local, "Library"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_RUNTIME_MODE", "container")
	root, err := openRuntimeLocalRootAt(local, work, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if root.Name() != local {
		t.Fatalf("container /local root = %q, want mounted path %q", root.Name(), local)
	}
}
