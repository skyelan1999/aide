package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBinarySHA256FreshContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "program")
	stamp := time.Unix(1000, 0)
	check := func(content []byte) {
		t.Helper()
		if err := os.WriteFile(file, content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		got, err := binarySHA256(file)
		want := sha256.Sum256(content)
		if err != nil || got != hex.EncodeToString(want[:]) {
			t.Fatalf("digest mismatch: %v", err)
		}
	}
	check(nil)
	content := bytes.Repeat([]byte("fresh executable bytes"), 8192)
	check(content)
	// Same size and timestamp must not reuse an earlier result.
	content[len(content)/2] ^= 1
	check(content)
	replacement := filepath.Join(filepath.Dir(file), "replacement")
	if err := os.Rename(file, replacement); err != nil {
		t.Fatal(err)
	}
	check(bytes.Repeat([]byte("z"), len(content)))
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if sum, err := binarySHA256(file); err == nil || sum != "" {
		t.Fatal("missing executable accepted")
	}
	if sum, err := binarySHA256(filepath.Dir(file)); err == nil || sum != "" {
		t.Fatal("unreadable content accepted")
	}
}

func BenchmarkBinarySHA256(b *testing.B) {
	file := filepath.Join(b.TempDir(), "program")
	data := bytes.Repeat([]byte("x"), 16<<20)
	if err := os.WriteFile(file, data, 0600); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := binarySHA256(file); err != nil {
			b.Fatal(err)
		}
	}
}
