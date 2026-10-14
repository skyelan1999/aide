//go:build linux

package execsandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sandboxFixture(t *testing.T) (string, string) {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	for _, p := range []string{workspace, outside} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, outside
}
func runFixture(t *testing.T, mode, workspace, command string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd, cleanup, err := Command(ctx, mode, workspace, command)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func TestSandboxFilesystemWrites(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			workspace, outside := sandboxFixture(t)
			if _, err := runFixture(t, mode, workspace, "echo inside > inside.txt"); (err == nil) != (mode != "read-only") {
				t.Fatalf("inside write mode=%s err=%v", mode, err)
			}
			target := filepath.Join(outside, "outside.txt")
			command := fmt.Sprintf(`python3 -c 'import pathlib; pathlib.Path(%q).write_text("outside")'`, target)
			out, err := runFixture(t, mode, workspace, command)
			if (err == nil) != (mode == "danger-full-access") {
				t.Fatalf("outside Python write mode=%s err=%v out=%s", mode, err, out)
			}
			_, statErr := os.Stat(target)
			if (statErr == nil) != (mode == "danger-full-access") {
				t.Fatal("outside bytes changed contrary to mode")
			}
		})
	}
}
func TestSandboxSymlinkAndScriptContainment(t *testing.T) {
	workspace, outside := sandboxFixture(t)
	target := filepath.Join(outside, "original.txt")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"echo changed > escape/original.txt", `python3 -c 'open("escape/original.txt","w").write("changed")'`, `node -e 'require("fs").writeFileSync("escape/original.txt","changed")'`, "rm escape/original.txt", fmt.Sprintf("mv escape/original.txt %q", filepath.Join(workspace, "moved"))} {
		if out, err := runFixture(t, "workspace-write", workspace, command); err == nil {
			t.Fatalf("escape succeeded: %s (%s)", command, out)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "original" {
			t.Fatalf("outside original mutated: %q %v", data, err)
		}
	}
	if out, err := runFixture(t, "workspace-write", workspace, `mkdir docs && echo valid > docs/ok.txt && cat docs/ok.txt`); err != nil || !strings.Contains(out, "valid") {
		t.Fatalf("ordinary workspace command failed: %s %v", out, err)
	}
	if _, err := exec.LookPath("node"); err == nil {
		if out, err := runFixture(t, "workspace-write", workspace, `node -e 'require("fs").writeFileSync("node-ok.txt","valid")'`); err != nil {
			t.Fatalf("Node cannot write inside allowed workspace: %s %v", out, err)
		}
	}
	if out, err := runFixture(t, "workspace-write", workspace, `python3 -c 'open("python-ok.txt","w").write("valid")'`); err != nil {
		t.Fatalf("Python cannot write inside allowed workspace: %s %v", out, err)
	}
	if _, err := runFixture(t, "workspace-write", workspace, fmt.Sprintf("cat %q", target)); err == nil {
		t.Fatal("ungranted outside content readable")
	}
}
func TestSandboxPrivateTemporaryDirectory(t *testing.T) {
	workspace, _ := sandboxFixture(t)
	out, err := runFixture(t, "read-only", workspace, `echo temporary > "$TMPDIR/tmp.txt" && cat "$TMPDIR/tmp.txt"`)
	if err != nil || !strings.Contains(out, "temporary") {
		t.Fatalf("isolated scratch failed: %s %v", out, err)
	}
}
