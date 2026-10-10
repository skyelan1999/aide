package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func discoveryRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	p := t.TempDir()
	r, e := os.OpenRoot(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	return r, p
}
func TestKnowledgeDiscoveryResumesLargeDirectory(t *testing.T) {
	root, p := discoveryRoot(t)
	for i := 0; i < 1103; i++ {
		if e := os.WriteFile(filepath.Join(p, fmt.Sprintf("f%04d.txt", i)), []byte("text"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	d := newKnowledgeDiscovery(2000)
	steps := 0
	for !d.Complete {
		b, e := d.Step(context.Background(), root, 37, 2, 7, 50, nil)
		if e != nil {
			t.Fatal(e)
		}
		if b.Files > 37 || b.Inspected > 50 || b.Directories > 2 {
			t.Fatal(b)
		}
		steps++
		if steps > 100 {
			t.Fatal("did not converge")
		}
	}
	if len(d.Entries) != 1104 || steps < 30 {
		t.Fatal(len(d.Entries), steps)
	}
	if d.Queue != nil && len(d.Queue) != 0 {
		t.Fatal(d.Queue)
	}
}
func TestKnowledgeDiscoveryExcludedAndCancel(t *testing.T) {
	root, p := discoveryRoot(t)
	for i := 0; i < 90; i++ {
		os.WriteFile(filepath.Join(p, fmt.Sprintf(".hidden%d", i)), []byte("secret"), 0600)
	}
	os.WriteFile(filepath.Join(p, "good.txt"), []byte("ok"), 0600)
	os.Symlink("good.txt", filepath.Join(p, "alias.txt"))
	os.Mkdir(filepath.Join(p, "vendor"), 0700)
	d := newKnowledgeDiscovery(100)
	b, e := d.Step(context.Background(), root, 5, 1, 7, 10, nil)
	if e != nil || b.Inspected != 10 || b.Complete {
		t.Fatal(b, e)
	}
	c := d.Clone()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = c.Step(ctx, root, 5, 1, 7, 10, nil)
	if e != context.Canceled {
		t.Fatal(e)
	}
	if c.Queue[0].Offset != d.Queue[0].Offset {
		t.Fatal("cancel moved cursor")
	}
	for !d.Complete {
		if _, e = d.Step(context.Background(), root, 5, 1, 7, 10, nil); e != nil {
			t.Fatal(e)
		}
	}
	if len(d.Entries) != 2 {
		t.Fatal(d.Entries)
	}
}
func TestKnowledgeDiscoveryCloneAndDirectoryMutation(t *testing.T) {
	root, p := discoveryRoot(t)
	os.WriteFile(filepath.Join(p, "old.txt"), []byte("old"), 0600)
	os.WriteFile(filepath.Join(p, "keep.txt"), []byte("keep"), 0600)
	d := newKnowledgeDiscovery(100)
	d.Step(context.Background(), root, 1, 1, 7, 10, nil)
	clone := d.Clone()
	clone.Step(context.Background(), root, 1, 1, 7, 10, nil)
	if len(d.Entries) != 2 {
		t.Fatal("clone changed published state")
	}
	os.Remove(filepath.Join(p, "old.txt"))
	os.WriteFile(filepath.Join(p, "new.txt"), []byte("new"), 0600)
	for !d.Complete {
		if _, e := d.Step(context.Background(), root, 1, 1, 7, 10, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, ok := d.Entries["old.txt"]; ok {
		t.Fatal("stale deleted file")
	}
	if _, ok := d.Entries["new.txt"]; !ok {
		t.Fatal("missing added file")
	}
}
func TestKnowledgeDiscoveryDepthAndCatalogLimits(t *testing.T) {
	root, p := discoveryRoot(t)
	os.MkdirAll(filepath.Join(p, "a", "b"), 0700)
	os.WriteFile(filepath.Join(p, "a", "b", "deep.txt"), []byte("deep"), 0600)
	d := newKnowledgeDiscovery(10)
	for !d.Complete {
		if _, e := d.Step(context.Background(), root, 5, 2, 1, 10, nil); e != nil {
			t.Fatal(e)
		}
	}
	if !d.Reasons["depth"] {
		t.Fatal("no scope warning")
	}
	if _, ok := d.Entries["a/b/deep.txt"]; ok {
		t.Fatal("depth bypass")
	}
	small := newKnowledgeDiscovery(1)
	b, e := small.Step(context.Background(), root, 5, 2, 7, 10, nil)
	if e != nil || b.Complete || !small.Reasons["catalog_limit"] {
		t.Fatal(b, e)
	}
}

func TestKnowledgeDiscoveryCatalogCanResume(t *testing.T) {
	root, p := discoveryRoot(t)
	for i := 0; i < 3; i++ {
		if e := os.WriteFile(filepath.Join(p, fmt.Sprintf("f%d.txt", i)), []byte("text"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	d := newKnowledgeDiscovery(2)
	b, e := d.Step(context.Background(), root, 10, 2, 7, 20, nil)
	if e != nil || b.Complete || !d.Reasons["catalog_limit"] || len(d.Entries) != 2 {
		t.Fatal(b, e)
	}
	d.MaxEntries = 10
	for !d.Complete {
		if _, e = d.Step(context.Background(), root, 10, 2, 7, 20, nil); e != nil {
			t.Fatal(e)
		}
	}
	if len(d.Entries) != 4 || d.Reasons["catalog_limit"] {
		t.Fatal(d.Entries, d.Reasons)
	}
}

func TestKnowledgeDiscoveryDirectPriority(t *testing.T) {
	root, p := discoveryRoot(t)
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(p, fmt.Sprintf("ordinary-%04d.txt", i)), []byte("other"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(p, "important", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p, "important", "deep", "target.txt"), []byte("target"), 0600)
	d := newKnowledgeDiscovery(2000)
	b, err := d.Step(context.Background(), root, 1, 4, 2, 8, []string{"important/deep/target.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Entries["important/deep/target.txt"]; !ok {
		t.Fatal("priority target delayed", b)
	}
	if b.Files != 1 || b.Inspected > 8 || b.Directories > 4 {
		t.Fatal(b)
	}
	for _, parent := range []string{".", "important", "important/deep"} {
		if _, ok := d.Entries[parent]; !ok {
			t.Fatal("missing ancestor", parent)
		}
	}
	for !d.Complete {
		if _, err = d.Step(context.Background(), root, 50, 10, 7, 100, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(d.Entries) != 1004 {
		t.Fatal("lost ordinary files", len(d.Entries))
	}
}
func TestKnowledgeDiscoveryPriorityRejectsSymlinkAncestor(t *testing.T) {
	root, p := discoveryRoot(t)
	os.Mkdir(filepath.Join(p, "real"), 0700)
	os.WriteFile(filepath.Join(p, "real", "secret.txt"), []byte("secret"), 0600)
	os.Symlink("real", filepath.Join(p, "alias"))
	d := newKnowledgeDiscovery(100)
	for !d.Complete {
		if _, err := d.Step(context.Background(), root, 2, 2, 7, 8, []string{"alias/secret.txt", ".hidden/file.txt", "../escape"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := d.Entries["alias/secret.txt"]; ok {
		t.Fatal("symlink ancestor admitted")
	}
}

func TestKnowledgeDiscoveryScopedDirectAddressing(t *testing.T) {
	root, p := discoveryRoot(t)
	os.MkdirAll(filepath.Join(p, "focus", "deep"), 0700)
	os.WriteFile(filepath.Join(p, "focus", "deep", "inside.txt"), []byte("inside"), 0600)
	for i := 0; i < 1100; i++ {
		os.WriteFile(filepath.Join(p, fmt.Sprintf("outside%04d.txt", i)), []byte("outside"), 0600)
	}
	d := newKnowledgeDiscovery(100)
	d.ScopePaths = []string{"focus/deep"}
	for !d.Complete {
		b, err := d.Step(context.Background(), root, 2, 2, 7, 5, nil)
		if err != nil {
			t.Fatal(err)
		}
		if b.Files > 2 || b.Inspected > 5 || b.Directories > 2 {
			t.Fatal(b)
		}
	}
	if len(d.Entries) != 4 {
		t.Fatal("scope includes siblings", len(d.Entries))
	}
	if _, ok := d.Entries["focus/deep/inside.txt"]; !ok {
		t.Fatal("scope missed")
	}
}
