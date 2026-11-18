package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

func snapshotStoredBytes(s RequestSnapshot) int {
	n := 0
	for _, chunk := range s.bodyChunks {
		n += len(chunk.data)
	}
	return n
}

func TestRequestSnapshotBodyMemoryRoundTrip(t *testing.T) {
	body := json.RawMessage(`{"messages":[{"content":"` + strings.Repeat("图纸 image-data-", 40000) + `"}],"stream":true}`)
	sum := sha256.Sum256(body)
	s := RequestSnapshot{Purpose: "chat#1", Model: "test", Messages: []Message{{Role: "user", Content: "original input"}}, Body: body, SHA256: hex.EncodeToString(sum[:]), Outcome: "completed"}
	s.compactBody()
	if s.Body != nil || snapshotStoredBytes(s) >= len(body)/10 || s.bodyLength() != len(body) {
		t.Fatalf("large repetitive body not compacted: raw=%d stored=%d original=%d", len(s.Body), snapshotStoredBytes(s), len(body))
	}
	decoded, err := s.bodyBytes()
	if err != nil || !bytes.Equal(decoded, body) {
		t.Fatalf("exact provider body changed: %v", err)
	}
	wire, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var ordinary map[string]json.RawMessage
	if err := json.Unmarshal(wire, &ordinary); err != nil {
		t.Fatal(err)
	}
	if len(ordinary) != 7 || !bytes.Equal(ordinary["body"], body) {
		t.Fatal("public/persistent snapshot JSON schema or body changed")
	}
	var restored RequestSnapshot
	if err := json.Unmarshal(wire, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Body != nil || len(restored.bodyChunks) == 0 || restored.SHA256 != s.SHA256 || restored.Messages[0].Content != "original input" {
		t.Fatal("restart did not retain compressed body, source hash and retry messages")
	}
	// Updating completion metadata must preserve the complete immutable body.
	restored.ElapsedMillis, restored.Outcome = 123, "failed"
	again, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	var fresh struct {
		Body    json.RawMessage `json:"body"`
		Outcome string          `json:"outcome"`
	}
	if err := json.Unmarshal(again, &fresh); err != nil || !bytes.Equal(fresh.Body, body) || fresh.Outcome != "failed" {
		t.Fatalf("metadata update lost the body: %v", err)
	}
}

func TestRequestSnapshotCompressedImageCapacity(t *testing.T) {
	image := make([]byte, 1<<20)
	_, _ = rand.New(rand.NewSource(17)).Read(image)
	body := json.RawMessage(`{"image":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(image) + `"}`)
	s := RequestSnapshot{Body: body}
	s.compactBody()
	if s.Body != nil || len(s.bodyChunks) == 0 {
		t.Fatal("base64 image body was not compacted")
	}
	for _, chunk := range s.bodyChunks {
		if cap(chunk.data) > len(chunk.data)+8192 {
			t.Fatalf("retained block buffer capacity: len=%d cap=%d", len(chunk.data), cap(chunk.data))
		}
	}
	decoded, err := s.bodyBytes()
	if err != nil || !bytes.Equal(decoded, body) {
		t.Fatalf("inline image data changed: %v", err)
	}
}

func TestRequestSnapshotBlockSharingAndEviction(t *testing.T) {
	body := json.RawMessage(`{"x":"` + strings.Repeat("shared-image-data-", 30000) + `"}`)
	first, second := RequestSnapshot{Body: body}, RequestSnapshot{Body: bytes.Clone(body)}
	first.compactBody()
	second.compactBody()
	for i, c := range first.bodyChunks {
		if c != second.bodyChunks[i] {
			t.Fatal("identical request blocks were duplicated")
		}
	}
	cache := snapshotBodyChunkCache{limit: 2}
	owned := cache.intern([]byte("original"))
	cache.intern([]byte("second"))
	cache.intern([]byte("third"))
	if len(cache.entries) != 2 || string(owned.data) != "original" {
		t.Fatal("lookup eviction lost an owning snapshot or exceeded its bound")
	}
	if cache.intern([]byte("original")) == owned {
		t.Fatal("old lookup entry was not evicted")
	}
}

func TestRequestSnapshotConcurrentBlockInterning(t *testing.T) {
	cache := snapshotBodyChunkCache{limit: 4}
	raw := []byte(strings.Repeat("immutable", 1000))
	got := make([]*snapshotBodyChunk, 12)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) { defer wg.Done(); got[i] = cache.intern(raw) }(i)
	}
	wg.Wait()
	for _, p := range got {
		if p != got[0] {
			t.Fatal("concurrent insertion retained duplicate blocks")
		}
	}
}

func TestRequestSnapshotShiftedInlineImageSharing(t *testing.T) {
	image := make([]byte, 700000)
	_, _ = rand.New(rand.NewSource(29)).Read(image)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
	makeBody := func(prefix string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"a": prefix, "image": url})
		return b
	}
	one, two := RequestSnapshot{Body: makeBody("short")}, RequestSnapshot{Body: makeBody(strings.Repeat("longer context ", 1000))}
	one.compactBody()
	two.compactBody()
	shared := 0
	seen := map[*snapshotBodyChunk]bool{}
	for _, c := range one.bodyChunks {
		seen[c] = true
	}
	for _, c := range two.bodyChunks {
		if seen[c] {
			shared++
		}
	}
	if shared < 10 {
		t.Fatalf("prefix shift duplicated inline image blocks: shared=%d", shared)
	}
	for _, s := range []RequestSnapshot{one, two} {
		b, err := s.bodyBytes()
		if err != nil || !json.Valid(b) {
			t.Fatalf("invalid reconstructed body: %v", err)
		}
		var got map[string]string
		if json.Unmarshal(b, &got) != nil || got["image"] != url {
			t.Fatal("inline image was changed")
		}
	}
}

func TestRequestSnapshotSmallAndDamagedBodies(t *testing.T) {
	for _, body := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(`{"x":1}`)} {
		s := RequestSnapshot{Body: body}
		s.compactBody()
		if s.bodyChunks != nil || !bytes.Equal(s.Body, body) {
			t.Fatal("small/legacy body changed")
		}
		wire, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var back RequestSnapshot
		if err := json.Unmarshal(wire, &back); err != nil {
			t.Fatal(err)
		}
	}
	s := RequestSnapshot{Body: json.RawMessage(`{"x":"` + strings.Repeat("x", 200000) + `"}`)}
	s.compactBody()
	chunk := s.bodyChunks[len(s.bodyChunks)-1]
	pos := len(chunk.data) / 2
	chunk.data[pos] ^= 0xff
	defer func() { chunk.data[pos] ^= 0xff }()
	if _, err := json.Marshal(s); err == nil {
		t.Fatal("damaged in-memory body must fail persistence, never become null")
	}
}
