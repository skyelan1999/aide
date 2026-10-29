package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the real tool dispatch and SFTP batch construction with owned clients.
func TestIssue563ToolListsRemoteRootOnce(t *testing.T) {
	for _, root := range []string{"fixture/project", "/srv/fixture/project", ""} {
		t.Run(root, func(t *testing.T) {
			a := testApp(t)
			dir := t.TempDir()
			log := filepath.Join(dir, "batch")
			a.sshBin = filepath.Join(dir, "ssh")
			a.sftpBin = filepath.Join(dir, "sftp")
			writeStub(t, a.sshBin, "#!/bin/sh\nexit 0\n")
			writeStub(t, a.sftpBin, "#!/bin/sh\ncat > "+shellQuote(log)+"\nprintf '%s\\n' '-rw-r--r-- 1 fixture fixture 4 Jan 1 00:00 readme.md'\n")
			a.wsConfig.Workspace.Mode = "ssh"
			a.wsConfig.Workspace.Path = root
			a.wsConfig.Workspace.Host = "example.invalid"
			a.wsConfig.Workspace.Username = "fixture"
			task := &Task{ID: "remote-root-owned", WorkspaceID: a.wsID(), WorkspaceMode: "ssh", WorkspaceRemotePath: root}
			for _, p := range []string{"", ".", "nested"} {
				result := boundCall(a, task, "list_files", map[string]any{"path": p})
				if !strings.Contains(result, "readme.md") {
					t.Fatal(result)
				}
				b, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				want := pathJoinRemote(root, p)
				if p == "" {
					want = pathJoinRemote(root, ".")
				}
				if string(b) != "cd "+shellQuoteRemote(want)+"\nls -l\n" {
					t.Errorf("root=%q p=%q batch=%q want=%q", root, p, b, want)
				}
			}
			before, _ := os.ReadFile(log)
			for _, p := range []string{"/etc", "../outside", "nested/../../outside"} {
				_ = boundCall(a, task, "list_files", map[string]any{"path": p})
				after, _ := os.ReadFile(log)
				if string(after) != string(before) {
					t.Fatal("unsafe path reached SFTP", p)
				}
			}
			// A task snapshot must not be re-prefixed with the currently configured root.
			task.WorkspaceRemotePath = "fixture/original"
			a.wsConfig.Workspace.Path = "fixture/switched"
			_ = boundCall(a, task, "list_files", map[string]any{"path": "."})
			b, _ := os.ReadFile(log)
			if !strings.Contains(string(b), `cd "fixture/original/."`) {
				t.Fatalf("lost task root: %s", b)
			}
		})
	}
}
