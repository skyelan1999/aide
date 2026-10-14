package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestKnowledgeBatchMaterialAliasesReadEachBodyOnce(t *testing.T) {
	a := testApp(t)
	for _, name := range []string{"good.md", "unavailable.md"} {
		if err := os.WriteFile(filepath.Join(a.refPath, name), []byte("readable reference"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "body-alias", Name: "body-alias", Type: "local", Enabled: true})
	a.mu.Unlock()
	reads := map[string]int{}
	cache := newKnowledgeScanCache(nil)
	cache.readText = func(root *os.Root, p string) (string, error) {
		reads[p]++
		if p == "unavailable.md" {
			return "", &os.PathError{Op: "read", Path: p, Err: syscall.EIO}
		}
		return knowledgeReadText(root, p)
	}
	g := a.knowledgeSnapshotModeCached(context.Background(), false, cache)
	if cache.err != nil {
		t.Fatal(cache.err)
	}
	if reads["good.md"] != 1 || reads["unavailable.md"] != 1 {
		t.Fatal("aliases repeated current-batch body I/O", reads)
	}
	good, bad := map[string]knowledgeNode{}, map[string]knowledgeNode{}
	for _, n := range g.Nodes {
		if n.Path == "good.md" {
			good[n.Region] = n
		}
		if n.Path == "unavailable.md" {
			bad[n.Region] = n
		}
	}
	for _, region := range []string{"context", "source:body-alias"} {
		if good[region].Text != "readable reference" || bad[region].ReadState != "unavailable" || bad[region].Text != "" {
			t.Fatal("alias lost its own observation or body gap", region, good[region], bad[region])
		}
	}
	if good["context"].ID == good["source:body-alias"].ID || good["context"].Origin == good["source:body-alias"].Origin {
		t.Fatal("body reuse merged source identities")
	}
	// An I/O failure may heal without metadata changing (for example a cloud
	// placeholder becoming available). Do not inherit failures into the next batch.
	next := newKnowledgeScanCache(cache)
	if _, ok := next.previous[a.refPath+"\x00unavailable.md"]; ok {
		t.Fatal("failed body became a successful durable material")
	}
	nextReads := map[string]int{}
	next.readText = func(root *os.Root, p string) (string, error) {
		nextReads[p]++
		return knowledgeReadText(root, p)
	}
	recovered := a.knowledgeSnapshotModeCached(context.Background(), false, next)
	if next.err != nil || nextReads["unavailable.md"] != 1 || nextReads["good.md"] != 0 {
		t.Fatal("next-batch repair or successful memo reuse failed", next.err, nextReads)
	}
	repaired := map[string]bool{}
	for _, n := range recovered.Nodes {
		if n.Path == "unavailable.md" {
			if n.ReadState != "" || n.Text != "readable reference" {
				t.Fatal("same-metadata body repair remained unavailable", n)
			}
			repaired[n.Region] = true
		}
	}
	if !repaired["context"] || !repaired["source:body-alias"] {
		t.Fatal("repaired body missing from an alias", repaired)
	}
}

func TestKnowledgeBatchMaterialRevalidatesStampAndLocation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.md")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	cache := newKnowledgeScanCache(nil)
	reads := 0
	cache.readText = func(r *os.Root, p string) (string, error) { reads++; return knowledgeReadText(r, p) }
	info, _ := root.Lstat("file.md")
	if _, _, err := cache.material(root, dir, "file.md", info, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed body"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.material(root, dir, "file.md", info, false); err == nil {
		t.Fatal("stale caller metadata accepted a current-batch memo")
	}
	cache.err = nil
	info, _ = root.Lstat("file.md")
	text, _, err := cache.material(root, dir, "file.md", info, false)
	if err != nil || text != "changed body" || reads != 2 {
		t.Fatal("changed stamp reused old body", text, err, reads)
	}
	if _, _, err := cache.material(root, "different-location", "file.md", info, false); err != nil || reads != 3 {
		t.Fatal("distinct resolved location shared a body key", err, reads)
	}
}

func TestKnowledgeBatchMaterialCodeUpgradeKeepsVerifiedText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package fixture\nfunc Example() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	info, _ := root.Lstat("file.go")
	cache, reads := newKnowledgeScanCache(nil), 0
	cache.readText = func(r *os.Root, p string) (string, error) { reads++; return knowledgeReadText(r, p) }
	for _, want := range []bool{false, true, false, true} {
		text, code, err := cache.material(root, dir, "file.go", info, want)
		if err != nil || !strings.Contains(text, "Example") || want != strings.Contains(code, "Example") {
			t.Fatal("code upgrade/downgrade corrupted requested material", want, text, code, err)
		}
	}
	if reads != 1 {
		t.Fatal("code upgrade re-read verified text", reads)
	}
	// A code-only failure must not deny an alias that only requires text.
	cache.failedMaterial = map[string]knowledgeMaterialFailure{dir + "\x00file.go": {stamp: knowledgeStamp(info), err: syscall.EIO, codeOnly: true}}
	if _, _, err := cache.material(root, dir, "file.go", info, true); !errors.Is(err, syscall.EIO) {
		t.Fatal("code failure not retained for another code request", err)
	}
	if text, code, err := cache.material(root, dir, "file.go", info, false); err != nil || text == "" || code != "" {
		t.Fatal("code-only error poisoned plain text alias", text, code, err)
	}
}
