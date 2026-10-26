package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIssue90ExplicitSnapshotRootsRespectHostExclusions(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	for _, protected := range pluginSourceProtected {
		for _, nested := range []bool{false, true} {
			prefix := ""
			if nested {
				prefix = "folder/"
			}
			p := prefix + protected
			for _, recursive := range []bool{false, true} {
				t.Run(p+map[bool]string{false: "/file", true: "/directory"}[recursive], func(t *testing.T) {
					selected := p
					if recursive {
						if err := os.MkdirAll(filepath.Join(a.workspace.Name(), p), 0700); err != nil {
							t.Fatal(err)
						}
						selected += "/secret.txt"
					} else {
						if err := os.MkdirAll(filepath.Dir(filepath.Join(a.workspace.Name(), p)), 0700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(a.workspace.Name(), selected), []byte("owned-sensitive-marker"), 0600); err != nil {
						t.Fatal(err)
					}
					var total int64
					dest := t.TempDir()
					_, err := a.pluginSourceRequest(context.Background(), areas, dest, &total, map[string]bool{}, pluginSourceRequest{Operation: "stage", Path: p, Slot: "left", Recursive: recursive, Exclude: []string{}})
					if err == nil {
						t.Errorf("explicit protected path accepted: %v", err)
					}
					if total != 0 {
						t.Errorf("protected bytes read: %d", total)
					}
					entries, err := os.ReadDir(dest)
					if err != nil || len(entries) != 0 {
						t.Errorf("protected snapshot created entries: %v %v", entries, err)
					}
					if err := os.RemoveAll(filepath.Join(a.workspace.Name(), p)); err != nil {
						t.Fatal(err)
					}
				})
			}
			// Selecting a child directly must not bypass an excluded ancestor,
			// even if that child does not exist (reject before filesystem I/O).
			var total int64
			_, err := a.pluginSourceRequest(context.Background(), areas, t.TempDir(), &total, map[string]bool{}, pluginSourceRequest{Operation: "stage", Path: p + "/absent.txt", Slot: "left"})
			if err == nil {
				t.Errorf("protected ancestor bypassed: %s: %v", p, err)
			}
		}
	}
}

func TestIssue90RecursiveSnapshotKeepsOrdinaryAndUserExclusions(t *testing.T) {
	a := testApp(t)
	for _, p := range []string{"src/keep.txt", "src/skip.txt", "src/.env", "src/.git/config", "src/.env.example"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(a.workspace.Name(), p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), p), []byte(p), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	var total int64
	dest := t.TempDir()
	_, err := a.pluginSourceRequest(context.Background(), areas, dest, &total, map[string]bool{}, pluginSourceRequest{Operation: "stage", Path: "src", Slot: "left", Recursive: true, Exclude: []string{"skip.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"keep.txt", ".env.example"} {
		if _, err := os.Stat(filepath.Join(dest, "left/src", p)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"skip.txt", ".env", ".git"} {
		if _, err := os.Stat(filepath.Join(dest, "left/src", p)); !os.IsNotExist(err) {
			t.Fatalf("excluded path retained: %s: %v", p, err)
		}
	}
}
