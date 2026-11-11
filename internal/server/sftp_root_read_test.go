package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIssue691And694ScopedReadsRejectLinks(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace-download"
		if source {
			name = "source-text"
		}
		t.Run(name, func(t *testing.T) {
			a, src, remote := issue79SFTPFixture(t, source)
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "private"), []byte("outside marker"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(remote, "normal"), []byte("normal bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			for link, target := range map[string]string{"link": filepath.Join(outside, "private"), "parent": outside} {
				if err := os.Symlink(target, filepath.Join(remote, link)); err != nil {
					t.Fatal(err)
				}
			}
			read := func(p string) ([]byte, error) {
				if source {
					return a.sftpReadSource(src, p)
				}
				return a.downloadRaw(p, Source{}, false)
			}
			if b, err := read("normal"); err != nil || string(b) != "normal bytes" {
				t.Fatalf("normal read %q: %v", b, err)
			}
			for _, p := range []string{"link", "parent/private"} {
				if b, err := read(p); err == nil {
					t.Fatalf("escaped through %s: %q", p, b)
				}
				if !source {
					if _, err := a.collectArchive(p, Source{}, false, false); err == nil {
						t.Fatalf("archive followed %s", p)
					}
				}
			}
		})
	}
}
