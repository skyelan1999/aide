package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeTimelineLargeCompressedRetention(t *testing.T) {
	g := knowledgeGraph{Workspace: "w"}
	for i := 0; i < 20052; i++ {
		g.Nodes = append(g.Nodes, knowledgeNode{ID: fmt.Sprintf("n%05d", i), Text: strings.Repeat("large-map evidence ", 16)})
	}
	s := knowledgeTimeStore{Version: 1, Workspace: "w"}
	s, _, err := appendKnowledgeTime(s, g, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(s)
	if len(one)*2 <= knowledgeTimelineBytes {
		t.Fatal("fixture does not reproduce legacy eviction")
	}
	g.Nodes[0].Text = "new independently observed evidence"
	s, changed, err := appendKnowledgeTime(s, g, time.Unix(2, 0))
	if err != nil || !changed || len(s.Snapshots) != 2 || s.Dropped != 0 {
		t.Fatal("lost distinct observations", changed, err, len(s.Snapshots))
	}
	file := filepath.Join(t.TempDir(), "history.json")
	key := knowledgeTimeKey("fixture")
	if err := saveKnowledgeTime(file, s, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if len(raw) > knowledgeTimelineBytes {
		t.Fatal("physical budget exceeded")
	}
	s, err = loadKnowledgeTime(file, "w", false, key)
	if err != nil {
		t.Fatal(err)
	}
	c := compareKnowledgeTime(s.Snapshots[0], s.Snapshots[1])
	if len(c.Changed) != 1 || c.Changed[0].Before.Text != strings.Repeat("large-map evidence ", 16) || c.Changed[0].After.Text != g.Nodes[0].Text {
		t.Fatal("comparison lost exact evidence")
	}
	t.Logf("nodes=%d old snapshot=%d two snapshots stored=%d", len(g.Nodes), len(one), len(raw))
}

func TestKnowledgeTimelineLegacyCompressionMigration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "history.json")
	key := knowledgeTimeKey("fixture")
	s, _, _ := appendKnowledgeTime(knowledgeTimeStore{Version: 1, Workspace: "w"}, knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a", Text: "old evidence"}}}, time.Unix(1, 0))
	plain, _ := json.Marshal(s)
	ct, nonce, _ := sealWithKey(key, plain)
	if err := atomicJSON(file, knowledgeVectorEnvelope{1, ct, nonce}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(file)
	loaded, err := loadKnowledgeTime(file, "w", false, key)
	if err != nil || loaded.compressed {
		t.Fatal("legacy unreadable", err)
	}
	if _, changed, err := appendKnowledgeTime(loaded, s.Snapshots[0].Graph, time.Unix(2, 0)); changed || err != nil {
		t.Fatal("format created fake observation")
	}
	if err := saveKnowledgeTime(file, loaded, key); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(file + ".before-gzip-v2-" + hash(original)[:12] + ".json")
	if !bytes.Equal(original, backup) {
		t.Fatal("rollback bytes differ")
	}
	reloaded, err := loadKnowledgeTime(file, "w", false, key)
	if err != nil || !reloaded.compressed || reloaded.Snapshots[0].Digest != loaded.Snapshots[0].Digest {
		t.Fatal("migration altered history", err)
	}
	current, _ := os.ReadFile(file)
	if err := saveKnowledgeTime(file, loaded, key); err == nil {
		t.Fatal("stale migration accepted")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(current, after) {
		t.Fatal("failed migration replaced history")
	}
}

func TestKnowledgeTimelineCompressionRejectsCorruptionAndBomb(t *testing.T) {
	valid, _ := compressKnowledgeTime([]byte(`{"version":1}`))
	bomb, _ := compressKnowledgeTime(bytes.Repeat([]byte("x"), knowledgeTimelinePlainBytes+1))
	for _, b := range [][]byte{[]byte("invalid"), valid[:len(valid)-1], append(append([]byte(nil), valid...), 'x'), append(append([]byte(nil), valid...), valid...), bomb} {
		if plain, err := decompressKnowledgeTime(b); err == nil {
			zeroBytes(plain)
			t.Fatal("corrupt/oversize stream accepted")
		}
	}
	ct, nonce, _ := sealWithKey(knowledgeTimeKey("fixture"), valid)
	raw, _ := json.Marshal(knowledgeVectorEnvelope{2, ct, nonce})
	if knowledgeTimeEnvelopeSize(len(valid)) != len(raw) {
		t.Fatal("encoded budget estimate differs")
	}
}

func TestKnowledgeTimelineIndependentCapacityBounds(t *testing.T) {
	s := knowledgeTimeStore{Version: 1, Workspace: "w"}
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a", Text: strings.Repeat("z", 17<<20)}}}
	s, _, err := appendKnowledgeTime(s, g, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Number = 1
	next, _, err := appendKnowledgeTime(s, g, time.Unix(2, 0))
	if err != nil || len(next.Snapshots) != 1 || next.Dropped != 1 {
		t.Fatal("expanded budget not enforced", err)
	}
	// Deterministic, poorly compressible printable text exceeds the physical
	// envelope limit while remaining below the expanded JSON limit.
	rng := rand.New(rand.NewSource(1))
	text := make([]byte, 9<<20)
	for i := range text {
		text[i] = byte(32 + rng.Intn(95))
	}
	g.Nodes[0].Text = string(text)
	if _, changed, err := appendKnowledgeTime(knowledgeTimeStore{Version: 1, Workspace: "w"}, g, time.Unix(3, 0)); err == nil || changed {
		t.Fatal("physical budget not enforced")
	}
	if len(s.Snapshots) != 1 || s.Dropped != 0 {
		t.Fatal("capacity failure changed caller")
	}
}

func TestKnowledgeTimelineExistingV2ReaderCompatibility(t *testing.T) {
	s, _, err := appendKnowledgeTime(knowledgeTimeStore{Version: 1, Workspace: "w"}, knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a", Text: "previously persisted evidence"}}}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err = writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	key := knowledgeTimeKey("fixture")
	ciphertext, nonce, err := sealWithKey(key, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "existing-v2.json")
	if err = atomicJSON(file, knowledgeVectorEnvelope{2, ciphertext, nonce}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadKnowledgeTime(file, "w", false, key)
	if err != nil {
		t.Fatal("existing v2 history unreadable", err)
	}
	if len(loaded.Snapshots) != 1 || loaded.Snapshots[0].Digest != s.Snapshots[0].Digest || loaded.Snapshots[0].Graph.Nodes[0].Text != "previously persisted evidence" {
		t.Fatal("existing evidence changed")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read rewrote existing ciphertext", err)
	}
}
