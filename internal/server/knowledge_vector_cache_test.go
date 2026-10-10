package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestKnowledgeVectorCachePersistenceAndIsolation(t *testing.T) {
	dir := t.TempDir()
	key := knowledgeVectorEncryptionKey([]byte("fixture-only"))
	p := knowledgeEmbeddingPolicy{BaseURL: "http://fixture/v1", Model: "fixture", Generation: 1}
	ns := knowledgeVectorNamespace("workspace-a", p)
	id := hash([]byte("private-document-id"))
	c := &knowledgeVectorCache{}
	if err := c.store(ns, dir, key, map[string][]float64{id: {.123456789, .987654321}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ns+".vec"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(id)) || bytes.Contains(raw, []byte("0.123456789")) {
		t.Fatal("plaintext vectors leaked")
	}
	cold := &knowledgeVectorCache{}
	hits, warning := cold.lookup(ns, dir, key, []string{id})
	if warning != "" || len(hits) != 1 || hits[0][0] != .123456789 {
		t.Fatal("cold reload", hits, warning)
	}
	hits[0][0] = 99
	again, _ := cold.lookup(ns, dir, key, []string{id})
	if again[0][0] == 99 {
		t.Fatal("mutable cache alias")
	}
	for _, other := range []string{knowledgeVectorNamespace("workspace-b", p), knowledgeVectorNamespace("workspace-a", knowledgeEmbeddingPolicy{BaseURL: p.BaseURL, Model: "other", Generation: 1}), knowledgeVectorNamespace("workspace-a", knowledgeEmbeddingPolicy{BaseURL: p.BaseURL, Model: p.Model, Generation: 2})} {
		hits, warning := cold.lookup(other, dir, key, []string{id})
		if len(hits) != 0 || warning != "" {
			t.Fatal("identity reused", hits, warning)
		}
	}
	badKey := knowledgeVectorEncryptionKey([]byte("changed-access-token"))
	hits, warning = (&knowledgeVectorCache{}).lookup(ns, dir, badKey, []string{id})
	if len(hits) != 0 || warning == "" {
		t.Fatal("wrong key accepted")
	}
}

func TestKnowledgeVectorChunkKeyTracksContent(t *testing.T) {
	hit := documentHit{ID: "chunk", Hash: "file-hash", Text: "first", Locator: "paragraph 1"}
	initial := knowledgeVectorChunkKey(hit)
	hit.Text = "second"
	if initial == knowledgeVectorChunkKey(hit) {
		t.Fatal("changed text reused")
	}
	hit.Text = "first"
	hit.Hash = "new-file-hash"
	if initial == knowledgeVectorChunkKey(hit) {
		t.Fatal("file digest ignored")
	}
	hit.Hash = "file-hash"
	hit.Locator = "paragraph 2"
	if initial == knowledgeVectorChunkKey(hit) {
		t.Fatal("locator ignored")
	}
}

func TestKnowledgeVectorCacheBoundsAndCorruption(t *testing.T) {
	dir := t.TempDir()
	key := knowledgeVectorEncryptionKey([]byte("fixture"))
	ns := hash([]byte("fixture"))
	c := &knowledgeVectorCache{}
	rows := map[string][]float64{}
	v := make([]float64, 4096)
	v[0] = 1
	for i := 0; i < 150; i++ {
		rows[hash([]byte(fmt.Sprint(i)))] = v
	}
	if err := c.store(ns, dir, key, rows); err != nil {
		t.Fatal(err)
	}
	if c.bytes > knowledgeVectorBytes || len(c.rows) >= len(rows) {
		t.Fatal("memory limit not enforced", c.bytes, len(c.rows))
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("user file"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := c.store(hash([]byte(fmt.Sprint("namespace", i))), dir, key, map[string][]float64{hash([]byte("id")): {1}}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*.vec"))
	if len(entries) != 8 {
		t.Fatal("disk limit", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal("unrelated file deleted")
	}
	if err := os.WriteFile(filepath.Join(dir, ns+".vec"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	cold := &knowledgeVectorCache{}
	id := hash([]byte("regenerated"))
	hits, w := cold.lookup(ns, dir, key, []string{id})
	if len(hits) != 0 || w == "" {
		t.Fatal("corruption reused")
	}
	if err := cold.store(ns, dir, key, map[string][]float64{id: {1}}); err != nil {
		t.Fatal(err)
	}
	hits, w = (&knowledgeVectorCache{}).lookup(ns, dir, key, []string{id})
	if len(hits) != 1 || w != "" {
		t.Fatal("regeneration failed", w)
	}
	cold.invalidate(ns, dir)
	if _, err := os.Stat(filepath.Join(dir, ns+".vec")); !os.IsNotExist(err) {
		t.Fatal("invalidation failed", err)
	}
}

func TestKnowledgeVectorCacheConcurrent(t *testing.T) {
	dir := t.TempDir()
	key := knowledgeVectorEncryptionKey([]byte("fixture"))
	c := &knowledgeVectorCache{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ns := hash([]byte(fmt.Sprint(i % 2)))
			id := hash([]byte(fmt.Sprint(i)))
			for j := 0; j < 3; j++ {
				if err := c.store(ns, dir, key, map[string][]float64{id: {1, 0}}); err != nil {
					t.Error(err)
				}
				c.lookup(ns, dir, key, []string{id})
			}
		}(i)
	}
	wg.Wait()
}
