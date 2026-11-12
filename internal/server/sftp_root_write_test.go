package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIssue695And697MutationsRejectLinks(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			a, src, remote := issue79SFTPFixture(t, source)
			if source {
				src.RW = true
				for i := range a.sourceRegistry.Sources {
					if a.sourceRegistry.Sources[i].ID == src.ID {
						a.sourceRegistry.Sources[i].RW = true
					}
				}
			}
			outside := t.TempDir()
			private := filepath.Join(outside, "private")
			if err := os.WriteFile(private, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			for name, target := range map[string]string{"escape": outside, "link": private, "staging.aide-tmp": private} {
				if err := os.Symlink(target, filepath.Join(remote, name)); err != nil {
					t.Fatal(err)
				}
			}
			write := func(p string) error {
				if source {
					return a.sftpWriteSource(src, p, []byte("new bytes"))
				}
				return a.sftpWrite(filepath.Join(remote, p), []byte("new bytes"))
			}
			for _, p := range []string{"escape/new", "escape/private", "link"} {
				if err := write(p); err == nil {
					t.Fatalf("write followed %s", p)
				}
			}
			if err := write("normal"); err != nil {
				t.Fatal(err)
			}
			if err := write("normal"); err != nil {
				t.Fatalf("overwrite: %v", err)
			}
			if err := write("staging"); err != nil {
				t.Fatal(err)
			}
			if !source {
				if err := write("nested/deeper/file"); err != nil {
					t.Fatal(err)
				}
				for _, err := range []error{a.sftpMakeDirectory(filepath.Join(remote, "escape/created")), a.sftpRemoveWorkspacePath("escape/private", false), a.sftpRemoveWorkspacePath("escape", true), a.sftpRenameDirectory(filepath.Join(remote, "normal"), filepath.Join(remote, "escape/moved")), a.sftpRenameDirectory(filepath.Join(remote, "escape/private"), filepath.Join(remote, "stolen"))} {
					if err == nil {
						t.Fatal("mutation followed symlink")
					}
				}
				if err := a.sftpMakeDirectory(filepath.Join(remote, "directory")); err != nil {
					t.Fatal(err)
				}
				if err := a.sftpRenameDirectory(filepath.Join(remote, "directory"), filepath.Join(remote, "renamed")); err != nil {
					t.Fatal(err)
				}
				if err := a.sftpRemoveWorkspacePath("nested", true); err != nil {
					t.Fatal(err)
				}
			}
			got, err := os.ReadFile(private)
			if err != nil || string(got) != "unchanged" {
				t.Fatalf("outside changed %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
				t.Fatalf("outside file created: %v", err)
			}
		})
	}
}
