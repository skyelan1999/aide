package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchTextStaysInsideCapturedWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "inside.md"), []byte("visible marker\nharmless\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "outside.md")
	if err := os.WriteFile(secret, []byte("outside marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a := &App{}

	if got := a.searchText(root, "visible", "."); !strings.Contains(got, "inside.md:1:visible marker") {
		t.Fatalf("expected workspace match, got %q", got)
	}
	for _, path := range []string{secret, "../outside/outside.md", "/etc/passwd"} {
		got := a.searchText(root, "marker", path)
		if strings.Contains(got, "outside marker") || strings.Contains(got, "root:") {
			t.Fatalf("search escaped workspace for %q: %q", path, got)
		}
		if !strings.Contains(got, "相对路径") {
			t.Fatalf("expected rejected path %q, got %q", path, got)
		}
	}
	link := filepath.Join(workspace, "outside-link.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := a.searchText(root, "outside marker", "."); strings.Contains(got, "outside-link.md:") {
		t.Fatalf("search followed an external symlink: %q", got)
	}
}

func TestTaskMemoryRejectsDifferentWorkspace(t *testing.T) {
	a, _, _ := parallelFixture(t, 1)
	a.mu.Lock()
	task := &Task{WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: a.workspaceMode()}
	a.wsConfig.Workspace.Path = "different-project"
	a.wsRevision++
	a.mu.Unlock()
	if got := a.readTaskMemory(task); !strings.Contains(got, "拒绝读取") {
		t.Fatalf("stale task memory read was not rejected: %q", got)
	}
	if got := a.writeTaskMemory(task, "must-not-write"); !strings.Contains(got, "拒绝写入") {
		t.Fatalf("stale task memory write was not rejected: %q", got)
	}
}
