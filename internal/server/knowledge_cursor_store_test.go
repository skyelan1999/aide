package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeCursorRestart(t *testing.T) {
	ar := knowledgeArea{root: "workspace", scope: "test", kind: "ssh", origin: "old-process", name: "workspace"}
	p := defaultKnowledgeIndexPolicy()
	d := newKnowledgeRemoteDiscovery()
	list := func(string) ([]map[string]any, error) {
		return []map[string]any{{"name": "a.md", "size": int64(6)}, {"name": "b.md", "size": int64(6)}}, nil
	}
	read := func(string, int64) ([]byte, error) { return []byte("secret"), nil }
	if _, err := d.step(context.Background(), ar, p, false, 1, 1, list, read); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "cache", "cursor.json")
	key := knowledgeTimeKey("test-cursor")
	if err := saveKnowledgeCursor(file, "identity", d, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("a.md")) {
		t.Fatal("unencrypted checkpoint")
	}
	ar.origin = "new-process"
	restored, err := loadKnowledgeCursor(file, "identity", ar, key)
	if err != nil || restored == nil || restored.Queue[0].Offset != 1 || restored.Published["a.md"].Origin != ar.origin {
		t.Fatalf("resume failed: %v", err)
	}
	candidate := restored.clone()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := candidate.step(cancelled, ar, p, false, 1, 1, list, read); err == nil {
		t.Fatal("cancelled scan accepted")
	}
	preserved := restored.snapshot(ar, p, 1, 1)
	if preserved.coverage.Files != 1 || restored.Queue[0].Offset != 1 || preserved.coverage.TotalKnown {
		t.Fatal("restored observations lost after failed candidate")
	}
	found := false
	for _, n := range preserved.nodes {
		if n.Path == "a.md" && n.Text == "secret" && n.Origin == ar.origin {
			found = true
		}
	}
	if !found {
		t.Fatal("restored text/current origin missing")
	}
	r, err := restored.step(context.Background(), ar, p, false, 1, 1, list, read)
	if err != nil || !r.coverage.TotalKnown || r.coverage.Files != 2 {
		t.Fatalf("continuation: %+v %v", r.coverage, err)
	}
	if other, err := loadKnowledgeCursor(file, "changed-policy", ar, key); err != nil || other != nil {
		t.Fatal("changed identity reused")
	}
	if _, err := loadKnowledgeCursor(file, "identity", ar, knowledgeTimeKey("other")); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKnowledgeCursor(file, "identity", ar, key); err == nil {
		t.Fatal("corrupt cache accepted")
	}
}

func TestKnowledgeCursorInvalidPaths(t *testing.T) {
	ar := knowledgeArea{root: "workspace", scope: "test", kind: "ssh"}
	d := newKnowledgeRemoteDiscovery()
	d.Queue[0].Path = "../escape"
	if validateKnowledgeRemoteCursor(d, ar) == nil {
		t.Fatal("unsafe path accepted")
	}
	file := filepath.Join(t.TempDir(), "cursor.json")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKnowledgeCursor(file, "id", ar, knowledgeTimeKey("k")); err == nil {
		t.Fatal("symlink read accepted")
	}
	if err := saveKnowledgeCursor(file, "id", newKnowledgeRemoteDiscovery(), knowledgeTimeKey("k")); err == nil {
		t.Fatal("symlink write accepted")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatal("target changed")
	}
}
