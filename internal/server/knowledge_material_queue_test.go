package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestKnowledgeMaterialTimeSliceLeavesExplicitPending(t *testing.T) {
	a := testApp(t)
	fixture := t.TempDir()
	root, openErr := os.OpenRoot(fixture)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer root.Close()
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(fixture, fmt.Sprintf("file-%02d.md", i)), []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ar := knowledgeArea{root: "workspace", kind: "local", location: fixture, scope: a.wsID(), origin: "fixture", enabled: true}
	cache := newKnowledgeScanCache(nil)
	reads := 0
	cache.readText = func(root *os.Root, p string) (string, error) {
		reads++
		time.Sleep(100 * time.Millisecond)
		return knowledgeReadText(root, p)
	}
	run := func(c *knowledgeScanCache) knowledgeGraph {
		g := knowledgeGraph{Workspace: a.wsID(), Sources: []knowledgeSource{{Region: "workspace"}}}
		var code []knowledgeCodeFile
		size, skipped := 0, 0
		a.knowledgeProgressiveArea(context.Background(), &g, c, root, ar, ar.scope, "", defaultKnowledgeIndexPolicy(), 10, 3, false, &code, &size, &skipped)
		if c.err != nil {
			t.Fatal(c.err)
		}
		return g
	}
	g := run(cache)
	if reads < 1 || reads > 5 {
		t.Fatal("material phase ignored time slice between reads", reads)
	}
	if g.Sources[0].Coverage.MaterialPending < 5 || g.Sources[0].Coverage.ReadPending < 5 || !knowledgeGraphScanning(g) {
		t.Fatal("deferred work not reported/scanned", g.Sources[0].Coverage)
	}
	payload := localPayload("resume", cache.discovery[knowledgeDiscoveryKey(ar)], cache.files, ar.location)
	file := filepath.Join(t.TempDir(), "cursor.json")
	key := knowledgeTimeKey("material-resume")
	if err := saveKnowledgeLocalCursor(file, payload, key); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadKnowledgeLocalCursor(file, "resume", key)
	if err != nil {
		t.Fatal(err)
	}
	v, materials := loaded.restore(ar.location)
	next := newKnowledgeScanCache(nil)
	next.discovery[knowledgeDiscoveryKey(ar)] = v
	next.previous = materials
	nextReads := 0
	next.readText = func(root *os.Root, p string) (string, error) { nextReads++; return knowledgeReadText(root, p) }
	finished := run(next)
	if nextReads != 10-reads || finished.Sources[0].Coverage.MaterialPending != 0 || finished.Sources[0].Coverage.ReadPending != 0 {
		t.Fatal("restart did not resume deferred bodies", reads, nextReads, finished.Sources[0].Coverage)
	}
}

func TestKnowledgeMaterialBatchBoundsAndFairness(t *testing.T) {
	a := testApp(t)
	fixture := t.TempDir()
	root, openErr := os.OpenRoot(fixture)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer root.Close()
	for i := 0; i < 61; i++ {
		if err := os.WriteFile(filepath.Join(fixture, fmt.Sprintf("file-%03d.md", i)), []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ar := knowledgeArea{root: "workspace", kind: "local", name: "fixture", location: fixture, scope: a.wsID(), origin: "fixture", enabled: true}
	var previous *knowledgeScanCache
	seen := map[string]bool{}
	complete := false
	for batch := 0; batch < 50; batch++ {
		cache := newKnowledgeScanCache(previous)
		reads := 0
		cache.readText = func(r *os.Root, p string) (string, error) {
			reads++
			seen[p] = true
			if p != "file-060.md" {
				return "", syscall.EIO
			}
			return knowledgeReadText(r, p)
		}
		g := knowledgeGraph{Workspace: a.wsID(), Sources: []knowledgeSource{{Region: "workspace"}}}
		var code []knowledgeCodeFile
		size, skipped := 0, 0
		a.knowledgeProgressiveArea(context.Background(), &g, cache, root, ar, ar.scope, "", defaultKnowledgeIndexPolicy(), 7, 3, false, &code, &size, &skipped)
		if cache.err != nil {
			t.Fatal(cache.err)
		}
		if reads > 7 {
			t.Fatalf("batch%d reopened growing prefix: reads=%d budget=7", batch, reads)
		}
		if len(seen) == 61 {
			for _, s := range g.Sources {
				if s.Coverage != nil && s.Coverage.Pending == 0 && s.Coverage.MaterialPending == 0 {
					complete = true
				}
			}
		}
		if complete {
			break
		}
		previous = cache
	}
	if len(seen) != 61 || !complete {
		t.Fatalf("material queue starved tail or never completed: seen=%d complete=%v", len(seen), complete)
	}
}

func TestKnowledgeMaterialQueueCheckpointAndClone(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	v := &knowledgeDiscoveryView{Cursor: newKnowledgeDiscovery(100), Cycle: 1, Published: map[string]knowledgeDiscoveryEntry{}}
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		info, e := root.Lstat(p)
		if e != nil {
			t.Fatal(e)
		}
		v.Published[p] = knowledgeDiscoveryEntry{p, info}
		v.Cursor.Entries[p] = v.Published[p]
	}
	v.prepareMaterial()
	v.Material.FileAfter = "a.md"
	v.Material.Validated["a.md"] = knowledgeStamp(v.Published["a.md"].Info)
	v.Material.Unavailable["a.md"] = knowledgeMaterialGap{knowledgeStamp(v.Published["a.md"].Info), "io"}
	clone := v.clone()
	clone.Material.FileAfter = "b.md"
	delete(clone.Material.Validated, "a.md")
	delete(clone.Material.Unavailable, "a.md")
	if v.Material.FileAfter != "a.md" || len(v.Material.Validated) != 1 || len(v.Material.Unavailable) != 1 {
		t.Fatal("clone mutated published queue")
	}
	payload := localPayload("identity", v, map[string]knowledgeFileMemo{}, dir)
	if err := validateKnowledgeLocalCursor(&payload); err != nil {
		t.Fatal(err)
	}
	key := knowledgeTimeKey("material-queue")
	file := filepath.Join(t.TempDir(), "cursor.json")
	if err := saveKnowledgeLocalCursor(file, payload, key); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadKnowledgeLocalCursor(file, "identity", key)
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := loaded.restore(dir)
	if restored.Material.FileAfter != "a.md" || restored.materialPending() != 2 || restored.Material.Unavailable["a.md"].Kind != "io" {
		t.Fatal("checkpoint lost pending/gap observations", restored.Material)
	}
	if got := materialQueue([]string{"a.md", "b.md", "c.md"}, restored.Material.FileAfter, 1); len(got) != 1 || got[0] != "b.md" {
		t.Fatal("restart repeated prefix", got)
	}
	bad := *loaded
	bad.MaterialState.FileAfter = "../escape"
	if validateKnowledgeLocalCursor(&bad) == nil {
		t.Fatal("unsafe queue anchor accepted")
	}
}

func TestKnowledgeMaterialCycleWaitsForValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body.md"), []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	info, err := root.Lstat("body.md")
	if err != nil {
		t.Fatal(err)
	}
	ar := knowledgeArea{root: "workspace", location: dir, origin: "fixture"}
	cache := newKnowledgeScanCache(nil)
	v := &knowledgeDiscoveryView{Cursor: newKnowledgeDiscovery(100), Cycle: 1, Published: map[string]knowledgeDiscoveryEntry{"body.md": {"body.md", info}}}
	v.Cursor.Complete = true
	v.Cursor.Queue = nil
	v.Cursor.Entries["body.md"] = v.Published["body.md"]
	cache.discovery[knowledgeDiscoveryKey(ar)] = v
	if _, _, err := cache.discover(context.Background(), root, ar, defaultKnowledgeIndexPolicy(), 1, 1); err != nil {
		t.Fatal(err)
	}
	if v.Cycle != 1 || v.materialPending() != 1 {
		t.Fatal("discovery restarted before pending material was validated")
	}
	v.Material.Validated["body.md"] = knowledgeStamp(info)
	if _, _, err := cache.discover(context.Background(), root, ar, defaultKnowledgeIndexPolicy(), 1, 1); err != nil {
		t.Fatal(err)
	}
	if v.Cycle != 2 {
		t.Fatal("completed material did not release next discovery cycle")
	}
}
